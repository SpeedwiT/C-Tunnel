package transport

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func init() { register(cws{}) }

// cws (C-WS) carries the tunnel as a WebSocket upgrade over TLS: a normal HTTPS request that asks to
// Upgrade to websocket, a 101 response, then the raw tunnel bytes. To DPI it is a WebSocket session
// behind HTTPS — very common and unremarkable. (We tunnel raw bytes after the upgrade; the core layer
// already frames and encrypts them.)
type cws struct{}

func (cws) Name() string     { return "cws" }
func (cws) Category() string { return CatTCP }
func (cws) Description() string {
	return "WebSocket over TLS (looks like a normal WebSocket)"
}

const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const wsPath = "/ws"

func (cws) Listen(cfg *config.Config) (net.Listener, error) {
	cert, err := selfSigned(sniOrDefault(cfg.SNI))
	if err != nil {
		return nil, err
	}
	inner, err := tls.Listen("tcp", cfg.Listen, &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		return nil, err
	}
	return &wsListener{Listener: inner}, nil
}

type wsListener struct{ net.Listener }

func (l *wsListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		conn, err := wsServerHandshake(c)
		if err != nil {
			c.Close()
			continue // not a valid upgrade; drop and keep serving
		}
		return conn, nil
	}
}

func wsServerHandshake(c net.Conn) (net.Conn, error) {
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return nil, err
	}
	key := req.Header.Get("Sec-WebSocket-Key")
	if req.Header.Get("Upgrade") == "" || key == "" {
		return nil, fmt.Errorf("not a websocket upgrade")
	}
	accept := wsAccept(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := c.Write([]byte(resp)); err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	return wrapBuffered(c, br), nil
}

func (cws) Dial(cfg *config.Config) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 15 * time.Second}
	c, err := tls.DialWithDialer(d, "tcp", cfg.Peer, &tls.Config{
		ServerName: sniOrDefault(cfg.SNI), InsecureSkipVerify: true, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		return nil, err
	}
	var kb [16]byte
	_, _ = rand.Read(kb[:])
	key := base64.StdEncoding.EncodeToString(kb[:])
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", wsPath, sniOrDefault(cfg.SNI), key)
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
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		c.Close()
		return nil, fmt.Errorf("ws upgrade failed: %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != wsAccept(key) {
		c.Close()
		return nil, fmt.Errorf("ws accept mismatch")
	}
	_ = c.SetDeadline(time.Time{})
	return wrapBuffered(c, br), nil
}

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsMagic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
