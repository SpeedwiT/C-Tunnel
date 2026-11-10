// Package broker is C Tunnel's rendezvous relay. Both servers dial OUT to it — neither needs an open
// inbound port — so a tunnel can be formed even when the Iran IP is only reachable domestically or the
// foreign IP is filtered. The broker pairs two outbound connections that present the same rendezvous
// id and pipes bytes between them. It never sees plaintext: the tunnel is already end-to-end encrypted
// by the secure core, so the broker needs no key and no certificate.
//
// It also keeps a tiny, short-lived blob store so a long setup payload can be fetched with a short
// pairing code (see the setupcode package).
package broker

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// proto header, one line, space-separated:
//
//	CRDV1 L <id>\n        register as the listener (relay) side, wait for a dialer
//	CRDV1 D <id>\n        register as the dialer (exit) side, wait for a listener
//	CRDV1 PUT <code> <n>\n<n bytes>   store a blob under a pairing code (replies OK\n)
//	CRDV1 GET <code>\n               fetch a blob (replies OK <n>\n<bytes> or ERR\n)
const magic = "CRDV1"

// pairTimeout is how long one side waits at the broker for its partner before giving up (the tunnel
// retries, so this only needs to be long enough to span a normal reconnect).
const pairTimeout = 90 * time.Second

// blobTTL bounds how long a setup blob lives in the store.
const blobTTL = 30 * time.Minute

type waiter struct {
	conn net.Conn
	done chan struct{}
}

type blob struct {
	data []byte
	exp  time.Time
}

// Broker is a running rendezvous relay.
type Broker struct {
	ln net.Listener

	mu      sync.Mutex
	waiting map[string]*waiter
	blobs   map[string]blob
}

// Serve starts a broker on listen (e.g. ":9000") and blocks until the listener is closed.
func Serve(listen string) error {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	return (&Broker{
		ln:      ln,
		waiting: map[string]*waiter{},
		blobs:   map[string]blob{},
	}).serve()
}

// Addr is the address the broker listens on (useful when started on :0 in tests).
func (b *Broker) Addr() net.Addr { return b.ln.Addr() }

// Start starts a broker on listen and returns it running in the background (used by tests/menus).
func Start(listen string) (*Broker, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	b := &Broker{ln: ln, waiting: map[string]*waiter{}, blobs: map[string]blob{}}
	go b.serve()
	return b, nil
}

func (b *Broker) Close() error { return b.ln.Close() }

func (b *Broker) serve() error {
	go b.gcLoop()
	for {
		c, err := b.ln.Accept()
		if err != nil {
			return nil
		}
		go b.handle(c)
	}
}

func (b *Broker) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		b.mu.Lock()
		for k, v := range b.blobs {
			if now.After(v.exp) {
				delete(b.blobs, k)
			}
		}
		b.mu.Unlock()
	}
}

func (b *Broker) handle(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		c.Close()
		return
	}
	var cmd, verb, id string
	var n int
	// Parse "CRDV1 <verb> <id> [n]".
	fields := splitFields(line)
	if len(fields) < 3 || fields[0] != magic {
		fmt.Fprint(c, "ERR\n")
		c.Close()
		return
	}
	cmd, verb, id = fields[0], fields[1], fields[2]
	_ = cmd
	switch verb {
	case "L", "D":
		_ = c.SetReadDeadline(time.Time{})
		b.pair(id, c, br)
	case "PUT":
		if len(fields) == 4 {
			fmt.Sscanf(fields[3], "%d", &n)
		}
		b.put(id, n, c, br)
	case "GET":
		b.get(id, c)
	default:
		fmt.Fprint(c, "ERR\n")
		c.Close()
	}
}

// pair blocks this connection until a partner with the same id arrives, then pipes the two together.
// Each side is sent a "PAIR\n" line the instant it is matched, so the client can block until paired
// (and the relay keeps exactly one registration outstanding) before the tunnel bytes flow.
func (b *Broker) pair(id string, c net.Conn, br *bufio.Reader) {
	b.mu.Lock()
	if w := b.waiting[id]; w != nil {
		delete(b.waiting, id)
		b.mu.Unlock()
		// second to arrive: signal both, pipe, then release the first.
		fmt.Fprint(w.conn, "PAIR\n")
		fmt.Fprint(c, "PAIR\n")
		pipe(bufConn{c, br}, w.conn)
		close(w.done)
		return
	}
	w := &waiter{conn: bufConn{c, br}, done: make(chan struct{})}
	b.waiting[id] = w
	b.mu.Unlock()

	select {
	case <-w.done: // paired and piped by the partner's goroutine
	case <-time.After(pairTimeout):
		b.mu.Lock()
		if b.waiting[id] == w {
			delete(b.waiting, id)
		}
		b.mu.Unlock()
		c.Close()
	}
}

func (b *Broker) put(code string, n int, c net.Conn, br *bufio.Reader) {
	defer c.Close()
	if n <= 0 || n > 1<<16 {
		fmt.Fprint(c, "ERR\n")
		return
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(br, data); err != nil {
		fmt.Fprint(c, "ERR\n")
		return
	}
	b.mu.Lock()
	b.blobs[code] = blob{data: data, exp: time.Now().Add(blobTTL)}
	b.mu.Unlock()
	fmt.Fprint(c, "OK\n")
}

func (b *Broker) get(code string, c net.Conn) {
	defer c.Close()
	b.mu.Lock()
	v, ok := b.blobs[code]
	b.mu.Unlock()
	if !ok || time.Now().After(v.exp) {
		fmt.Fprint(c, "ERR\n")
		return
	}
	fmt.Fprintf(c, "OK %d\n", len(v.data))
	_, _ = c.Write(v.data)
}

// bufConn lets buffered bytes already read for the header travel with the conn into the pipe.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) { _, _ = io.Copy(dst, src); done <- struct{}{} }
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
