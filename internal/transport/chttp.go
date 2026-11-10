package transport

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func init() { register(chttp{}) }

// chttp (C-HTTP) carries the tunnel as plain HTTP/1.1 on a TCP port (no TLS): an innocuous GET and a
// 200 OK, then the raw (already-encrypted) tunnel bytes. Useful where TLS itself draws attention or
// on ports where cleartext HTTP is expected. Confidentiality is still guaranteed by the core layer.
type chttp struct{}

func (chttp) Name() string     { return "chttp" }
func (chttp) Category() string { return CatTCP }
func (chttp) Description() string {
	return "شبیه HTTP ساده (بدون TLS)؛ رمزنگاری در لایهٔ هسته"
}

const httpPath = "/stream"

func (chttp) Listen(cfg *config.Config) (net.Listener, error) {
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, err
	}
	return &httpListener{Listener: ln}, nil
}

type httpListener struct{ net.Listener }

func (l *httpListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			c.Close()
			continue
		}
		_ = req.Body.Close()
		resp := "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\n" +
			"Cache-Control: no-cache\r\nConnection: keep-alive\r\n\r\n"
		if _, err := c.Write([]byte(resp)); err != nil {
			c.Close()
			continue
		}
		_ = c.SetDeadline(time.Time{})
		return wrapBuffered(c, br), nil
	}
}

func (chttp) Dial(cfg *config.Config) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 15 * time.Second}
	c, err := d.Dial("tcp", cfg.Peer)
	if err != nil {
		return nil, err
	}
	host := cfg.SNI
	if host == "" {
		host = cfg.Peer
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0\r\n"+
		"Accept: */*\r\nConnection: keep-alive\r\n\r\n", httpPath, host)
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := c.Write([]byte(req)); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("http handshake failed: %s", resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	return wrapBuffered(c, br), nil
}
