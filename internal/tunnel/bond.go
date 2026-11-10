package tunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/core"
	"github.com/SpeedwiT/c-tunnel/internal/multipath"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
	kcp "github.com/xtaci/kcp-go/v5"
)

// bondBlock is the (none) KCP crypt — the secure core encrypts above it.
func bondBlock() kcp.BlockCrypt { b, _ := kcp.NewNoneBlockCrypt(nil); return b }

func tuneBond(s *kcp.UDPSession) {
	s.SetStreamMode(true)
	s.SetNoDelay(1, 20, 2, 1)
	s.SetWindowSize(1024, 1024)
	s.SetMtu(1350)
	s.SetACKNoDelay(true)
}

// memberConfig clones cfg for member i: its own transport and the base port + i.
func memberConfig(cfg *config.Config, i int) (*config.Config, error) {
	mc := *cfg
	mc.Transport = cfg.Members[i]
	mc.Members = nil
	base := cfg.Listen
	host := ""
	if cfg.Role == config.RoleExit {
		base = cfg.Peer
		h, _, err := net.SplitHostPort(cfg.Peer)
		if err != nil {
			return nil, fmt.Errorf("cbond peer %q: %w", cfg.Peer, err)
		}
		host = h
	}
	_, portStr, err := net.SplitHostPort(base)
	if err != nil {
		return nil, fmt.Errorf("cbond base addr %q: %w", base, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf(":%d", port+i)
	if cfg.Role == config.RoleExit {
		addr = net.JoinHostPort(host, strconv.Itoa(port+i))
		mc.Peer = addr
	} else {
		mc.Listen = addr
	}
	return &mc, nil
}

// RunBondRelay runs the Iran side of the bonded transport: it listens for every member transport,
// feeds all accepted links into one multipath conn, and serves the tunnel (KCP → core → yamux) on top.
func RunBondRelay(ctx context.Context, cfg *config.Config) error {
	psk, err := decodePSK(cfg.PSK)
	if err != nil {
		return err
	}
	mp := multipath.Server()
	defer mp.Close()

	for i := range cfg.Members {
		mc, err := memberConfig(cfg, i)
		if err != nil {
			return err
		}
		tr, err := transport.Get(mc.Transport)
		if err != nil {
			return err
		}
		ln, err := tr.Listen(mc)
		if err != nil {
			return fmt.Errorf("bond listen %s: %w", mc.Transport, err)
		}
		defer ln.Close()
		go func(ln net.Listener, name string) {
			<-ctx.Done()
			ln.Close()
			_ = name
		}(ln, mc.Transport)
		go func(ln net.Listener) {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				mp.Add(c)
			}
		}(ln)
	}

	lis, err := kcp.ServeConn(bondBlock(), 0, 0, mp)
	if err != nil {
		return err
	}
	h := &hub{}
	for _, fwd := range cfg.Forwards {
		go serveForward(ctx, h, fwd)
	}
	log.Printf("C Tunnel bonded relay up: members=%s forwards=%d", strings.Join(cfg.Members, "+"), len(cfg.Forwards))
	for {
		kc, err := lis.AcceptKCP()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		tuneBond(kc)
		go func() {
			sec, err := core.Secure(kc, psk, false)
			if err != nil {
				kc.Close()
				return
			}
			sess, err := core.NewSession(sec, true)
			if err != nil {
				sec.Close()
				return
			}
			log.Printf("bonded exit connected (%d live members)", mp.MemberCount())
			h.set(sess)
		}()
	}
}

// RunBondExit runs the foreign side: it dials every member transport (redialing dropped ones), bonds
// them, and serves streams by dialing their targets.
func RunBondExit(ctx context.Context, cfg *config.Config) error {
	psk, err := decodePSK(cfg.PSK)
	if err != nil {
		return err
	}
	dialers := make([]multipath.Dialer, 0, len(cfg.Members))
	for i := range cfg.Members {
		mc, err := memberConfig(cfg, i)
		if err != nil {
			return err
		}
		tr, err := transport.Get(mc.Transport)
		if err != nil {
			return err
		}
		mcCopy := mc
		dialers = append(dialers, func() (net.Conn, error) { return tr.Dial(mcCopy) })
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	backoff := time.Second
	for ctx.Err() == nil {
		mp := multipath.Client(dialers)
		kc, err := kcp.NewConn2(multipath.PeerAddr(), bondBlock(), 0, 0, mp)
		if err != nil {
			mp.Close()
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		tuneBond(kc)
		sec, err := core.Secure(kc, psk, true)
		if err != nil {
			kc.Close()
			mp.Close()
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		sess, err := core.NewSession(sec, false)
		if err != nil {
			sec.Close()
			mp.Close()
			continue
		}
		log.Printf("C Tunnel bonded exit connected (members=%s)", strings.Join(cfg.Members, "+"))
		backoff = time.Second
		_ = sess.Serve(func(network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		})
		sec.Close()
		mp.Close()
		log.Printf("bonded tunnel dropped; reconnecting")
	}
	return nil
}
