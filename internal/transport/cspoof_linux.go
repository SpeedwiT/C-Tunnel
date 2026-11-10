//go:build linux

package transport

import (
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// spoofConn is a net.PacketConn whose outbound packets carry a forged source address (sent via a raw
// socket), while inbound packets are read from a normal UDP socket bound to the same port.
type spoofConn struct {
	recv  *net.UDPConn
	peer  *net.UDPAddr
	port  uint16
	rawFd int // -1 when not spoofing (falls back to the UDP socket)
	src   [4]byte
	dst   [4]byte
	open  atomic.Bool
}

func newSpoofConn(cfg *config.Config) (*spoofConn, error) {
	peer, err := net.ResolveUDPAddr("udp", cfg.Peer)
	if err != nil {
		return nil, fmt.Errorf("cspoof peer %q: %w", cfg.Peer, err)
	}
	dst4 := peer.IP.To4()
	if dst4 == nil {
		return nil, fmt.Errorf("cspoof needs an IPv4 peer")
	}
	// Bind our local port (cfg.Listen if set, else the peer's port — distinct hosts normally share it).
	localPort := peer.Port
	if cfg.Listen != "" {
		if lp, err := portOfAddr(cfg.Listen); err == nil {
			localPort = int(lp)
		}
	}
	recv, err := net.ListenUDP("udp", &net.UDPAddr{Port: localPort})
	if err != nil {
		return nil, fmt.Errorf("cspoof bind :%d: %w", localPort, err)
	}
	c := &spoofConn{recv: recv, peer: peer, port: uint16(peer.Port), rawFd: -1}
	copy(c.dst[:], dst4)
	c.open.Store(true)

	if cfg.SpoofSrc != "" {
		src := net.ParseIP(cfg.SpoofSrc).To4()
		if src == nil {
			recv.Close()
			return nil, fmt.Errorf("cspoof spoof_src %q is not an IPv4 address", cfg.SpoofSrc)
		}
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
		if err != nil {
			recv.Close()
			return nil, fmt.Errorf("cspoof raw socket (need root/CAP_NET_RAW): %w", err)
		}
		if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
			syscall.Close(fd)
			recv.Close()
			return nil, err
		}
		c.rawFd = fd
		copy(c.src[:], src)
	}
	return c, nil
}

func (c *spoofConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	if c.rawFd < 0 {
		return c.recv.WriteToUDP(p, c.peer) // non-spoof fallback
	}
	pkt := buildIPv4UDP(c.src, c.dst, c.port, c.port, p)
	sa := &syscall.SockaddrInet4{Addr: c.dst} // port ignored for IPPROTO_RAW
	if err := syscall.Sendto(c.rawFd, pkt, 0, sa); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *spoofConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, _, err := c.recv.ReadFromUDP(p)
	if err != nil {
		return 0, nil, err
	}
	return n, spoofPeerAddr, nil // forged source is ignored; one logical peer
}

func (c *spoofConn) Close() error {
	if !c.open.Swap(false) {
		return nil
	}
	if c.rawFd >= 0 {
		syscall.Close(c.rawFd)
	}
	return c.recv.Close()
}

func (c *spoofConn) LocalAddr() net.Addr                { return c.recv.LocalAddr() }
func (c *spoofConn) SetDeadline(t time.Time) error      { return c.recv.SetDeadline(t) }
func (c *spoofConn) SetReadDeadline(t time.Time) error  { return c.recv.SetReadDeadline(t) }
func (c *spoofConn) SetWriteDeadline(t time.Time) error { return c.recv.SetWriteDeadline(t) }

// portOfAddr is a small helper mirrored by tests.
func portOfAddr(addr string) (uint16, error) {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(p)
	return uint16(n), err
}
