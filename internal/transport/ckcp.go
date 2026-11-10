package transport

import (
	"net"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	kcp "github.com/xtaci/kcp-go/v5"
)

func init() {
	// C-KCP: reliable UDP tuned for normal-to-lossy links, moderate FEC.
	register(ckcp{name: "ckcp", desc: "reliable UDP (KCP) with FEC; smooths packet loss", data: 10, parity: 3, aggressive: false})
	// C-Storm: aggressive FEC + turbo for severe loss / heavy disruption.
	register(ckcp{name: "cstorm", desc: "reliable UDP (KCP) with aggressive FEC for severe loss/disruption", data: 10, parity: 5, aggressive: true})
}

// ckcp carries the tunnel over KCP, a reliable-UDP protocol with forward error correction, so lost
// packets are reconstructed without waiting for retransmission — ideal for Iran's lossy links. The
// core layer encrypts, so KCP runs without its own cipher.
type ckcp struct {
	name, desc   string
	data, parity int
	aggressive   bool
}

func (c ckcp) Name() string        { return c.name }
func (ckcp) Category() string      { return CatUDP }
func (c ckcp) Description() string { return c.desc }

func (c ckcp) block() kcp.BlockCrypt {
	b, _ := kcp.NewNoneBlockCrypt(nil)
	return b
}

func (c ckcp) tune(s *kcp.UDPSession) {
	s.SetStreamMode(true)
	s.SetWriteDelay(false)
	s.SetACKNoDelay(true)
	if c.aggressive {
		s.SetNoDelay(1, 10, 2, 1) // turbo
		s.SetWindowSize(2048, 2048)
	} else {
		s.SetNoDelay(1, 20, 2, 1)
		s.SetWindowSize(1024, 1024)
	}
	s.SetMtu(1350)
}

func (c ckcp) Listen(cfg *config.Config) (net.Listener, error) {
	ln, err := kcp.ListenWithOptions(cfg.Listen, c.block(), c.data, c.parity)
	if err != nil {
		return nil, err
	}
	_ = ln.SetReadBuffer(4 << 20)
	_ = ln.SetWriteBuffer(4 << 20)
	return &kcpListener{ln: ln, t: c}, nil
}

func (c ckcp) Dial(cfg *config.Config) (net.Conn, error) {
	s, err := kcp.DialWithOptions(cfg.Peer, c.block(), c.data, c.parity)
	if err != nil {
		return nil, err
	}
	c.tune(s)
	return s, nil
}

type kcpListener struct {
	ln *kcp.Listener
	t  ckcp
}

func (l *kcpListener) Accept() (net.Conn, error) {
	s, err := l.ln.AcceptKCP()
	if err != nil {
		return nil, err
	}
	l.t.tune(s)
	return s, nil
}

func (l *kcpListener) Close() error   { return l.ln.Close() }
func (l *kcpListener) Addr() net.Addr { return l.ln.Addr() }
