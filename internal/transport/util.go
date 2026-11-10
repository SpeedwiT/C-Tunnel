package transport

import (
	"bufio"
	"net"
)

// prefixConn is a net.Conn whose reads first drain bytes already buffered by a bufio.Reader (used by
// the handshake-based transports, which read HTTP headers with a bufio.Reader before handing off).
type prefixConn struct {
	net.Conn
	r *bufio.Reader
}

func wrapBuffered(c net.Conn, r *bufio.Reader) net.Conn {
	if r.Buffered() == 0 {
		return c
	}
	return &prefixConn{Conn: c, r: r}
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if p.r != nil && p.r.Buffered() > 0 {
		return p.r.Read(b)
	}
	return p.Conn.Read(b)
}
