package transport

import (
	"encoding/base32"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/miekg/dns"
	kcp "github.com/xtaci/kcp-go/v5"
)

func init() { register(cdns{}) }

// cdns (C-DNS) carries the tunnel inside real DNS messages. During Iran's shutdowns the censor's
// whitelist has repeatedly still passed DNS (UDP/53) while blocking almost everything else, so this is
// the blackout/last-resort transport: upstream bytes ride in the query name, downstream bytes ride in
// TXT answers, and KCP on top turns that lossy request/response channel into a reliable stream. Pair
// it with direct mode so the Iran server is the DNS *client* querying the foreign DNS server (the
// direction that survives when only outbound DNS works). The core still encrypts everything.
type cdns struct{}

func (cdns) Name() string     { return "cdns" }
func (cdns) Category() string { return CatSpecial }
func (cdns) Description() string {
	return "tunnel inside DNS (for full blackout); KCP over query/answer, slow but resilient"
}

// dnsCarrierMTU keeps each KCP packet small enough to fit a single DNS query name (≤255 bytes after
// base32 + labels + domain).
const dnsCarrierMTU = 110

// dnsDomain is the zone the tunnel masquerades under; both ends agree on it.
const dnsDomain = "t.ctun."

var dnsEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

func dnsBlock() kcp.BlockCrypt { b, _ := kcp.NewNoneBlockCrypt(nil); return b }

func tuneDNS(s *kcp.UDPSession) {
	s.SetStreamMode(true)
	s.SetNoDelay(1, 20, 2, 1)
	s.SetWindowSize(256, 256)
	s.SetMtu(dnsCarrierMTU)
	s.SetACKNoDelay(true)
}

func (cdns) Listen(cfg *config.Config) (net.Listener, error) {
	uaddr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		return nil, err
	}
	sc := newDNSServerConn(udp)
	lis, err := kcp.ServeConn(dnsBlock(), 0, 0, sc)
	if err != nil {
		udp.Close()
		return nil, err
	}
	return &dnsListener{lis: lis}, nil
}

func (cdns) Dial(cfg *config.Config) (net.Conn, error) {
	raddr, err := net.ResolveUDPAddr("udp", cfg.Peer)
	if err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	cc := newDNSClientConn(udp, raddr)
	sess, err := kcp.NewConn2(raddr, dnsBlock(), 0, 0, cc)
	if err != nil {
		cc.Close()
		return nil, err
	}
	tuneDNS(sess)
	return sess, nil
}

type dnsListener struct{ lis *kcp.Listener }

func (l *dnsListener) Accept() (net.Conn, error) {
	s, err := l.lis.AcceptKCP()
	if err != nil {
		return nil, err
	}
	tuneDNS(s)
	return s, nil
}
func (l *dnsListener) Close() error   { return l.lis.Close() }
func (l *dnsListener) Addr() net.Addr { return l.lis.Addr() }

// --- name <-> payload encoding ---

// encodeQuery builds a DNS query name carrying payload (or a poll when empty).
func encodeQuery(payload []byte) string {
	if len(payload) == 0 {
		return "p." + dnsDomain
	}
	s := strings.ToLower(dnsEnc.EncodeToString(payload))
	var labels []string
	labels = append(labels, "d")
	for len(s) > 60 {
		labels = append(labels, s[:60])
		s = s[60:]
	}
	labels = append(labels, s)
	return strings.Join(labels, ".") + "." + dnsDomain
}

// decodeQuery extracts the payload from a query name; ok=false means it was a poll (no upstream data).
func decodeQuery(name string) (payload []byte, ok bool) {
	name = strings.TrimSuffix(strings.ToLower(name), "."+dnsDomain)
	name = strings.TrimSuffix(name, ".")
	parts := strings.Split(name, ".")
	if len(parts) == 0 || parts[0] != "d" {
		return nil, false
	}
	joined := strings.ToUpper(strings.Join(parts[1:], ""))
	b, err := dnsEnc.DecodeString(joined)
	if err != nil {
		return nil, false
	}
	return b, true
}

func encodeTXT(datagram []byte) string { return strings.ToLower(dnsEnc.EncodeToString(datagram)) }
func decodeTXT(s string) ([]byte, error) {
	return dnsEnc.DecodeString(strings.ToUpper(s))
}

// --- server-side packet conn (the DNS server) ---

type dnsServerConn struct {
	udp *net.UDPConn

	mu  sync.Mutex
	out map[string][][]byte // addr -> queued downstream datagrams
}

const maxDownstreamPerResp = 8

func newDNSServerConn(udp *net.UDPConn) *dnsServerConn {
	return &dnsServerConn{udp: udp, out: map[string][][]byte{}}
}

func (c *dnsServerConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := c.udp.ReadFrom(buf)
		if err != nil {
			return 0, nil, err
		}
		msg := new(dns.Msg)
		if msg.Unpack(buf[:n]) != nil || len(msg.Question) == 0 {
			continue
		}
		payload, isData := decodeQuery(msg.Question[0].Name)
		c.respond(msg, addr)
		if isData && len(payload) > 0 {
			nn := copy(p, payload)
			return nn, addr, nil
		}
		// poll or empty: nothing for KCP, keep reading
	}
}

// respond flushes up to maxDownstreamPerResp queued datagrams for addr as TXT answers.
func (c *dnsServerConn) respond(q *dns.Msg, addr net.Addr) {
	resp := new(dns.Msg)
	resp.SetReply(q)
	resp.Authoritative = true
	c.mu.Lock()
	queue := c.out[addr.String()]
	var take [][]byte
	for len(queue) > 0 && len(take) < maxDownstreamPerResp {
		take = append(take, queue[0])
		queue = queue[1:]
	}
	c.out[addr.String()] = queue
	c.mu.Unlock()

	name := q.Question[0].Name
	for _, dg := range take {
		resp.Answer = append(resp.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 0},
			Txt: []string{encodeTXT(dg)},
		})
	}
	resp.SetEdns0(4096, false)
	packed, err := resp.Pack()
	if err != nil {
		return
	}
	_, _ = c.udp.WriteTo(packed, addr)
}

func (c *dnsServerConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	dg := make([]byte, len(p))
	copy(dg, p)
	c.mu.Lock()
	q := c.out[addr.String()]
	if len(q) > 2048 { // guard against unbounded growth if the client stops polling
		q = q[len(q)-2048:]
	}
	c.out[addr.String()] = append(q, dg)
	c.mu.Unlock()
	return len(p), nil
}

func (c *dnsServerConn) Close() error                       { return c.udp.Close() }
func (c *dnsServerConn) LocalAddr() net.Addr                { return c.udp.LocalAddr() }
func (c *dnsServerConn) SetDeadline(t time.Time) error      { return c.udp.SetDeadline(t) }
func (c *dnsServerConn) SetReadDeadline(t time.Time) error  { return c.udp.SetReadDeadline(t) }
func (c *dnsServerConn) SetWriteDeadline(t time.Time) error { return c.udp.SetWriteDeadline(t) }

// --- client-side packet conn (the DNS client) ---

type dnsClientConn struct {
	udp    *net.UDPConn
	server net.Addr

	inbox    chan []byte
	closed   chan struct{}
	deadline atomic.Value // time.Time
	closeOne sync.Once
}

func newDNSClientConn(udp *net.UDPConn, server net.Addr) *dnsClientConn {
	c := &dnsClientConn{
		udp:    udp,
		server: server,
		inbox:  make(chan []byte, 4096),
		closed: make(chan struct{}),
	}
	go c.readLoop()
	go c.pollLoop()
	return c
}

func (c *dnsClientConn) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := c.udp.Read(buf)
		if err != nil {
			return
		}
		msg := new(dns.Msg)
		if msg.Unpack(buf[:n]) != nil {
			continue
		}
		for _, rr := range msg.Answer {
			txt, ok := rr.(*dns.TXT)
			if !ok {
				continue
			}
			for _, s := range txt.Txt {
				if dg, err := decodeTXT(s); err == nil && len(dg) > 0 {
					select {
					case c.inbox <- dg:
					case <-c.closed:
						return
					}
				}
			}
		}
	}
}

// pollLoop keeps asking the server so it can push queued downstream data even when we have nothing to
// send. It idles faster when traffic is flowing is unnecessary — KCP's own packets act as polls too.
func (c *dnsClientConn) pollLoop() {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			c.sendQuery(nil)
		}
	}
}

func (c *dnsClientConn) sendQuery(payload []byte) {
	m := new(dns.Msg)
	m.SetQuestion(encodeQuery(payload), dns.TypeTXT)
	m.SetEdns0(4096, false)
	packed, err := m.Pack()
	if err != nil {
		return
	}
	_, _ = c.udp.Write(packed)
}

func (c *dnsClientConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.sendQuery(p)
	return len(p), nil
}

func (c *dnsClientConn) ReadFrom(p []byte) (int, net.Addr, error) {
	var timer *time.Timer
	var timeout <-chan time.Time
	if d, ok := c.deadline.Load().(time.Time); ok && !d.IsZero() {
		rem := time.Until(d)
		if rem <= 0 {
			return 0, nil, timeoutErr{}
		}
		timer = time.NewTimer(rem)
		timeout = timer.C
		defer timer.Stop()
	}
	select {
	case dg := <-c.inbox:
		n := copy(p, dg)
		return n, c.server, nil
	case <-timeout:
		return 0, nil, timeoutErr{}
	case <-c.closed:
		return 0, nil, fmt.Errorf("c-dns: closed")
	}
}

func (c *dnsClientConn) Close() error {
	c.closeOne.Do(func() { close(c.closed) })
	return c.udp.Close()
}
func (c *dnsClientConn) LocalAddr() net.Addr { return c.udp.LocalAddr() }
func (c *dnsClientConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	return nil
}
func (c *dnsClientConn) SetReadDeadline(t time.Time) error {
	c.deadline.Store(t)
	return nil
}
func (c *dnsClientConn) SetWriteDeadline(time.Time) error { return nil }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
