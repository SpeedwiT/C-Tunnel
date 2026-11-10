package transport

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/quic-go/quic-go"
)

func init() { register(cquic{}) }

// cquic (C-QUIC) carries the tunnel over QUIC with an HTTP/3 ALPN, so on the wire it looks like an
// ordinary HTTP/3 session. It is the fastest transport, multiplexes natively, and migrates across
// IP/port changes, so brief address changes don't drop the tunnel. UDP.
type cquic struct{}

func (cquic) Name() string     { return "cquic" }
func (cquic) Category() string { return CatUDP }
func (cquic) Description() string {
	return "QUIC/HTTP3; fastest, migrates across IP/port"
}

func quicConfig() *quic.Config {
	return &quic.Config{
		KeepAlivePeriod: 15 * time.Second,
		MaxIdleTimeout:  40 * time.Second,
	}
}

func (cquic) Listen(cfg *config.Config) (net.Listener, error) {
	cert, err := selfSigned(sniOrDefault(cfg.SNI))
	if err != nil {
		return nil, err
	}
	tc := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"}}
	ln, err := quic.ListenAddr(cfg.Listen, tc, quicConfig())
	if err != nil {
		return nil, err
	}
	return &quicListener{ln: ln}, nil
}

func (cquic) Dial(cfg *config.Config) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tc := &tls.Config{ServerName: sniOrDefault(cfg.SNI), InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"}}
	conn, err := quic.DialAddr(ctx, cfg.Peer, tc, quicConfig())
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "")
		return nil, err
	}
	return &quicConn{Stream: stream, conn: conn}, nil
}

type quicListener struct{ ln *quic.Listener }

func (l *quicListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.ln.Accept(context.Background())
		if err != nil {
			return nil, err
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			_ = conn.CloseWithError(0, "")
			continue
		}
		return &quicConn{Stream: stream, conn: conn}, nil
	}
}

func (l *quicListener) Close() error   { return l.ln.Close() }
func (l *quicListener) Addr() net.Addr { return l.ln.Addr() }

// quicConn adapts a QUIC stream+connection to net.Conn.
type quicConn struct {
	*quic.Stream
	conn *quic.Conn
}

func (q *quicConn) LocalAddr() net.Addr  { return q.conn.LocalAddr() }
func (q *quicConn) RemoteAddr() net.Addr { return q.conn.RemoteAddr() }
func (q *quicConn) Close() error {
	_ = q.Stream.Close()
	return q.conn.CloseWithError(0, "")
}
