package transport

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func init() { register(cfrag{}) }

// cfrag (C-Frag) is C-TLS whose ClientHello is written in tiny TCP segments, so the SNI is split
// across packets. DPI that extracts the SNI from a single packet can't see it, defeating SNI-based
// blocking — without any CDN. The relay side is an ordinary TLS listener.
type cfrag struct{}

func (cfrag) Name() string     { return "cfrag" }
func (cfrag) Category() string { return CatTCP }
func (cfrag) Description() string {
	return "ClientHello/SNI fragmentation to defeat SNI-based DPI"
}

func (cfrag) Listen(cfg *config.Config) (net.Listener, error) {
	cert, err := selfSigned(sniOrDefault(cfg.SNI))
	if err != nil {
		return nil, err
	}
	return tls.Listen("tcp", cfg.Listen, &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	})
}

func (cfrag) Dial(cfg *config.Config) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 15 * time.Second}
	raw, err := d.Dial("tcp", cfg.Peer)
	if err != nil {
		return nil, err
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true) // send each fragment as its own packet
	}
	frag := &fragConn{Conn: raw, remaining: 4096, chunk: 6}
	tlsConn := tls.Client(frag, &tls.Config{
		ServerName: sniOrDefault(cfg.SNI), InsecureSkipVerify: true, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		raw.Close()
		return nil, err
	}
	frag.done() // stop fragmenting after the handshake
	return tlsConn, nil
}

// fragConn splits the first `remaining` bytes of output into tiny chunks (the TLS ClientHello), then
// passes bytes through unchanged.
type fragConn struct {
	net.Conn
	remaining int
	chunk     int
	off       bool
}

func (f *fragConn) done() { f.off = true }

func (f *fragConn) Write(p []byte) (int, error) {
	if f.off || f.remaining <= 0 {
		return f.Conn.Write(p)
	}
	total := 0
	for len(p) > 0 && f.remaining > 0 {
		n := f.chunk
		if n > len(p) {
			n = len(p)
		}
		w, err := f.Conn.Write(p[:n])
		total += w
		f.remaining -= w
		if err != nil {
			return total, err
		}
		time.Sleep(time.Millisecond) // force separate segments
		p = p[n:]
	}
	if len(p) > 0 {
		w, err := f.Conn.Write(p)
		return total + w, err
	}
	return total, nil
}
