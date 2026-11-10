package tunnel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/core"
	"github.com/SpeedwiT/c-tunnel/internal/iptun"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
)

// RunDirect runs the direct-mode (full IP) tunnel. The Iran server (relay) dials OUT — so it needs no
// inbound port — and the foreign server (exit) listens. A TUN device on each host joins both on a
// private /30; forwarded ports are plain TCP proxies over that shared subnet.
func RunDirect(ctx context.Context, cfg *config.Config) error {
	tr, err := transport.Get(cfg.Transport)
	if err != nil {
		return err
	}
	psk, err := decodePSK(cfg.PSK)
	if err != nil {
		return err
	}
	localCIDR, err := localTunCIDR(cfg)
	if err != nil {
		return err
	}
	peerIP, err := iptun.PeerIP(localCIDR)
	if err != nil {
		return err
	}
	mtu := autoMTU(cfg.Transport)

	dev, err := iptun.Open("ctun0", localCIDR, mtu)
	if err != nil {
		return err
	}
	defer dev.Close()
	log.Printf("C Tunnel direct: tun ctun0 %s (peer %s) mtu=%d transport=%s role=%s",
		localCIDR, peerIP, mtu, cfg.Transport, cfg.Role)

	// Forward ports over the shared subnet.
	for _, fwd := range cfg.Forwards {
		if cfg.Role == config.RoleRelay {
			// Iran: users hit :Listen, we proxy across the tunnel to the exit's TUN ip.
			go tcpProxy(ctx, fmt.Sprintf(":%d", fwd.Listen), fmt.Sprintf("%s:%d", peerIP, fwd.Listen))
		} else {
			// Foreign: traffic arrives at our TUN ip:Listen; proxy it to the real target.
			local := strings.SplitN(localCIDR, "/", 2)[0]
			go tcpProxy(ctx, fmt.Sprintf("%s:%d", local, fwd.Listen), fwd.Target)
		}
	}

	if cfg.Role == config.RoleRelay {
		return directDial(ctx, cfg, tr, psk, dev, mtu)
	}
	return directListen(ctx, cfg, tr, psk, dev, mtu)
}

// directDial is the Iran side: dial out, secure, pump, and reconnect forever.
func directDial(ctx context.Context, cfg *config.Config, tr transport.Transport, psk []byte, dev iptun.Device, mtu int) error {
	backoff := time.Second
	for ctx.Err() == nil {
		raw, err := tr.Dial(cfg)
		if err != nil {
			log.Printf("direct dial %s: %v", cfg.Peer, err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		sec, err := core.Secure(raw, psk, true)
		if err != nil {
			raw.Close()
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		log.Printf("direct tunnel up (dialed %s)", cfg.Peer)
		backoff = time.Second
		_ = iptun.Pump(sec, dev, mtu)
		sec.Close()
		log.Printf("direct tunnel dropped; reconnecting")
	}
	return nil
}

// directListen is the foreign side: accept, secure, pump; a new peer replaces the old pump.
func directListen(ctx context.Context, cfg *config.Config, tr transport.Transport, psk []byte, dev iptun.Device, mtu int) error {
	ln, err := tr.Listen(cfg)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		sec, err := core.Secure(raw, psk, false)
		if err != nil {
			raw.Close()
			continue
		}
		log.Printf("direct tunnel up (peer connected from %s)", raw.RemoteAddr())
		_ = iptun.Pump(sec, dev, mtu)
		sec.Close()
		log.Printf("direct tunnel dropped; waiting for peer")
	}
}

// tcpProxy accepts on listenAddr and pipes each connection to dialAddr.
func tcpProxy(ctx context.Context, listenAddr, dialAddr string) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Printf("forward listen %s: %v", listenAddr, err)
		return
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go func() {
			remote, err := net.DialTimeout("tcp", dialAddr, 10*time.Second)
			if err != nil {
				c.Close()
				return
			}
			pipeConn(c, remote)
		}()
	}
}

func pipeConn(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}

// localTunCIDR returns this node's TUN address. The relay uses TunSubnet as given (.1); the exit takes
// the peer address (.2) with the same prefix.
func localTunCIDR(cfg *config.Config) (string, error) {
	base := cfg.TunSubnet
	if base == "" {
		base = "10.72.0.1/30"
	}
	if cfg.Role == config.RoleRelay {
		return base, nil
	}
	peer, err := iptun.PeerIP(base)
	if err != nil {
		return "", err
	}
	prefix := "30"
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		prefix = base[i+1:]
	}
	return peer + "/" + prefix, nil
}

// autoMTU picks a safe TUN MTU for the carrier: lower for datagram transports that add their own
// overhead, higher for stream transports.
func autoMTU(tr string) int {
	switch tr {
	case "cquic", "ckcp", "cstorm", "cdns", "crdv":
		return 1200
	default:
		return 1380
	}
}
