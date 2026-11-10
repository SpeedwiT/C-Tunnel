// Package multipath is C Tunnel's seamless multi-transport engine — the feature no other tunnel ships.
// Instead of pinning one transport and reconnecting (which drops every session) when it is throttled,
// it runs SEVERAL member links at once and presents them as one packet channel. A reliability layer
// (KCP) rides on top, so packets are striped across the live members for aggregate bandwidth and, if a
// member is choked or dies mid-stream, KCP simply retransmits over the others — the session never
// drops. The secure core sits above KCP, so every member carries only opaque ciphertext.
//
// Layering (exit → relay):
//
//	yamux → core.Secure → KCP → multipath.Conn → member transports (TLS/WS/QUIC/KCP/…)
//
// The model is point-to-point (one exit ⇄ one relay): all member links belong to the single logical
// tunnel, so no per-session demux is needed and the PacketConn presents one stable peer address.
package multipath

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Dialer opens one member link (used by the client side, which can redial a dropped member).
type Dialer func() (net.Conn, error)

// Conn presents many member links as a single net.PacketConn.
type Conn struct {
	mu        sync.Mutex
	members   []*member
	rr        uint64
	inbox     chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	reconnect bool
	deadline  atomic.Value // time.Time
}

type member struct {
	conn net.Conn
	wmu  sync.Mutex
	dial Dialer
	dead atomic.Bool
}

func newConn() *Conn {
	return &Conn{inbox: make(chan []byte, 4096), closed: make(chan struct{})}
}

// Server builds a multipath conn fed by accepted member links (relay side).
func Server() *Conn { return newConn() }

// Add registers an accepted member link with a server-side Conn.
func (c *Conn) Add(link net.Conn) { c.addLink(link, nil) }

// Client builds a multipath conn by opening each dialer, keeping dropped members redialed (exit side).
func Client(dialers []Dialer) *Conn {
	c := newConn()
	c.reconnect = true
	for _, d := range dialers {
		d := d
		if conn, err := d(); err == nil {
			c.addLink(conn, d)
		} else {
			go c.redial(d)
		}
	}
	return c
}

// MemberCount reports how many members are currently alive (for status/metrics).
func (c *Conn) MemberCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.members {
		if !m.dead.Load() {
			n++
		}
	}
	return n
}

func (c *Conn) addLink(link net.Conn, dial Dialer) {
	m := &member{conn: link, dial: dial}
	c.mu.Lock()
	c.members = append(c.members, m)
	c.mu.Unlock()
	go c.readMember(m)
}

func (c *Conn) readMember(m *member) {
	br := bufio.NewReaderSize(m.conn, 1<<16)
	for {
		frame, err := readFrame(br)
		if err != nil {
			c.killMember(m)
			return
		}
		select {
		case c.inbox <- frame:
		case <-c.closed:
			return
		}
	}
}

func (c *Conn) killMember(m *member) {
	if m.dead.Swap(true) {
		return
	}
	_ = m.conn.Close()
	c.mu.Lock()
	for i, mm := range c.members {
		if mm == m {
			c.members = append(c.members[:i], c.members[i+1:]...)
			break
		}
	}
	c.mu.Unlock()
	if c.reconnect && m.dial != nil {
		select {
		case <-c.closed:
		default:
			go c.redial(m.dial)
		}
	}
}

func (c *Conn) redial(d Dialer) {
	backoff := time.Second
	for {
		select {
		case <-c.closed:
			return
		default:
		}
		if conn, err := d(); err == nil {
			c.addLink(conn, d)
			return
		}
		t := time.NewTimer(backoff)
		select {
		case <-c.closed:
			t.Stop()
			return
		case <-t.C:
		}
		if backoff *= 2; backoff > 20*time.Second {
			backoff = 20 * time.Second
		}
	}
}

// WriteTo sends one datagram, striped round-robin across the live members (bonding + failover). It is
// best-effort: if every member fails, the datagram is dropped and KCP above will retransmit it.
func (c *Conn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.mu.Lock()
	members := make([]*member, len(c.members))
	copy(members, c.members)
	c.mu.Unlock()
	if len(members) == 0 {
		return len(p), nil // nothing alive yet; let KCP retransmit
	}
	start := int(atomic.AddUint64(&c.rr, 1))
	for i := 0; i < len(members); i++ {
		m := members[(start+i)%len(members)]
		if m.dead.Load() {
			continue
		}
		if err := writeFrame(m, p); err == nil {
			return len(p), nil
		}
		c.killMember(m)
	}
	return len(p), nil
}

// ReadFrom returns the next datagram from any member; the peer address is always the same singleton.
func (c *Conn) ReadFrom(p []byte) (int, net.Addr, error) {
	var timeout <-chan time.Time
	if d, ok := c.deadline.Load().(time.Time); ok && !d.IsZero() {
		rem := time.Until(d)
		if rem <= 0 {
			return 0, nil, timeoutErr{}
		}
		t := time.NewTimer(rem)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case frame := <-c.inbox:
		return copy(p, frame), peerAddr, nil
	case <-timeout:
		return 0, nil, timeoutErr{}
	case <-c.closed:
		return 0, nil, fmt.Errorf("multipath: closed")
	}
}

func (c *Conn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	c.mu.Lock()
	for _, m := range c.members {
		_ = m.conn.Close()
	}
	c.members = nil
	c.mu.Unlock()
	return nil
}

func (c *Conn) LocalAddr() net.Addr                { return peerAddr }
func (c *Conn) SetDeadline(t time.Time) error      { c.deadline.Store(t); return nil }
func (c *Conn) SetReadDeadline(t time.Time) error  { c.deadline.Store(t); return nil }
func (c *Conn) SetWriteDeadline(t time.Time) error { return nil }

// --- framing: [2-byte length][datagram] on each member stream ---

func writeFrame(m *member, p []byte) error {
	if len(p) > 65535 {
		return fmt.Errorf("multipath: datagram too large")
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(p)))
	m.wmu.Lock()
	defer m.wmu.Unlock()
	if _, err := m.conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := m.conn.Write(p)
	return err
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	buf := make([]byte, binary.BigEndian.Uint16(hdr[:]))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type mpAddr struct{}

func (mpAddr) Network() string { return "multipath" }
func (mpAddr) String() string  { return "multipath://peer" }

var peerAddr net.Addr = mpAddr{}

// PeerAddr is the single stable address the multipath conn presents (pass it to kcp.NewConn2).
func PeerAddr() net.Addr { return peerAddr }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
