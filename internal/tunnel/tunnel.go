// Package tunnel wires a transport + the secure core into a working relay (Iran) / exit (abroad).
package tunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/core"
	"github.com/SpeedwiT/c-tunnel/internal/notify"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
)

// RunRelay runs the Iran side: it waits for the exit to connect over the transport, then serves the
// forwarded ports to users, routing each connection through the live tunnel.
func RunRelay(ctx context.Context, cfg *config.Config) error {
	tr, err := transport.Get(cfg.Transport)
	if err != nil {
		return err
	}
	ln, err := tr.Listen(cfg)
	if err != nil {
		return fmt.Errorf("listen (%s): %w", cfg.Transport, err)
	}
	defer ln.Close()

	h := &hub{}
	psk, err := decodePSK(cfg.PSK)
	if err != nil {
		return err
	}

	// open the user-facing forward listeners
	for _, fwd := range cfg.Forwards {
		go serveForward(ctx, h, fwd)
	}

	log.Printf("C Tunnel relay up: transport=%s listen=%s forwards=%d", cfg.Transport, cfg.Listen, len(cfg.Forwards))
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("relay accept: %v", err)
			time.Sleep(time.Second)
			continue
		}
		go func() {
			sec, err := core.Secure(raw, psk, false)
			if err != nil {
				raw.Close()
				log.Printf("relay handshake: %v", err)
				return
			}
			sess, err := core.NewSession(sec, true)
			if err != nil {
				sec.Close()
				return
			}
			log.Printf("exit connected from %s", raw.RemoteAddr())
			h.set(sess)
		}()
	}
}

func serveForward(ctx context.Context, h *hub, fwd config.Forward) {
	addr := fmt.Sprintf(":%d", fwd.Listen)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("forward %q listen %s: %v", fwd.Name, addr, err)
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
		sess := h.get()
		if sess == nil || sess.IsClosed() {
			c.Close() // no live tunnel yet
			continue
		}
		go func() {
			if err := sess.Open(fwd.Target, c); err != nil {
				c.Close()
			}
		}()
	}
}

// RunExit runs the foreign side: dial the relay, serve streams by connecting to their targets, and
// reconnect forever with backoff so the tunnel is self-healing.
func RunExit(ctx context.Context, cfg *config.Config) error {
	tr, err := transport.Get(cfg.Transport)
	if err != nil {
		return err
	}
	psk, err := decodePSK(cfg.PSK)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	// Optional Telegram status/ping notifications.
	notifier := notify.FromConfig(cfg)
	var live struct {
		mu sync.Mutex
		s  *core.Session
	}
	peerName := cfg.Peer
	if cfg.Transport == "crdv" {
		peerName = "broker:" + cfg.Broker
	}
	if notifier != nil {
		interval := 0
		if cfg.Telegram != nil {
			interval = cfg.Telegram.Interval
		}
		go notifier.Heartbeat(ctx, time.Duration(interval)*time.Second, func() notify.StatusLine {
			live.mu.Lock()
			s := live.s
			live.mu.Unlock()
			sl := notify.StatusLine{Transport: cfg.Transport, Mode: cfg.Mode, Peer: peerName}
			if s != nil && !s.IsClosed() {
				sl.Up = true
				if p, err := s.Ping(); err == nil {
					sl.Ping = p
				}
			}
			return sl
		})
	}

	backoff := time.Second
	for ctx.Err() == nil {
		raw, err := tr.Dial(cfg)
		if err != nil {
			log.Printf("exit dial %s: %v", peerName, err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		sec, err := core.Secure(raw, psk, true)
		if err != nil {
			raw.Close()
			log.Printf("exit handshake: %v", err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		sess, err := core.NewSession(sec, false)
		if err != nil {
			sec.Close()
			continue
		}
		log.Printf("C Tunnel exit connected to relay %s (transport=%s)", peerName, cfg.Transport)
		backoff = time.Second
		live.mu.Lock()
		live.s = sess
		live.mu.Unlock()
		if notifier != nil {
			p, _ := sess.Ping()
			notifier.SendOnce(notify.Format(notify.StatusLine{Up: true, Transport: cfg.Transport, Mode: cfg.Mode, Peer: peerName, Ping: p}))
		}
		_ = sess.Serve(func(network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		})
		live.mu.Lock()
		live.s = nil
		live.mu.Unlock()
		log.Printf("tunnel dropped; reconnecting")
		if notifier != nil {
			notifier.SendOnce(notify.Format(notify.StatusLine{Up: false}))
		}
	}
	return nil
}

// hub holds the current live session shared between the accept loop and the forward listeners.
type hub struct {
	mu   sync.RWMutex
	sess *core.Session
}

func (h *hub) set(s *core.Session) {
	h.mu.Lock()
	old := h.sess
	h.sess = s
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (h *hub) get() *core.Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sess
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 20*time.Second {
		return 20 * time.Second
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
