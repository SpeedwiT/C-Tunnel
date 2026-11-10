// Package linktest probes every transport between the two servers and measures which actually connect
// and how fast they are, so the operator can pick the best one from "Manage → Link Test" instead of
// guessing. The relay opens a short-lived listener per transport; the exit dials each, completes the
// real secure handshake, and times a live round-trip ping.
package linktest

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/core"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
)

// Result is one transport's probe outcome.
type Result struct {
	Transport string
	Category  string
	OK        bool
	Connect   time.Duration // time to establish the carrier
	Ping      time.Duration // round-trip over the live secure session
	Err       string
}

// Server runs on the relay (Iran) during a Link Test: it listens for every named transport on
// basePort+index and keeps each accepted session alive so the exit's pings are answered. Call stop()
// to tear everything down.
func Server(base *config.Config, names []string, basePort int) (stop func(), err error) {
	psk, err := config.DecodePSK(base.PSK)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	var lns []net.Listener
	closeAll := func() {
		cancel()
		for _, l := range lns {
			_ = l.Close()
		}
	}
	for i, name := range names {
		tr, e := transport.Get(name)
		if e != nil {
			continue
		}
		cfg := *base
		cfg.Transport = name
		cfg.Listen = fmt.Sprintf(":%d", basePort+i)
		ln, e := tr.Listen(&cfg)
		if e != nil {
			closeAll()
			return nil, fmt.Errorf("listen %s: %w", name, e)
		}
		lns = append(lns, ln)
		go serveProbe(ctx, ln, psk)
	}
	if len(lns) == 0 {
		cancel()
		return nil, fmt.Errorf("no transports to serve")
	}
	return closeAll, nil
}

// serveProbe accepts probe connections and keeps each secure session alive (so yamux answers pings)
// until the exit disconnects or the test ends. It never opens forwarding streams.
func serveProbe(ctx context.Context, ln net.Listener, psk []byte) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			sec, err := core.Secure(raw, psk, false)
			if err != nil {
				raw.Close()
				return
			}
			sess, err := core.NewSession(sec, true)
			if err != nil {
				sec.Close()
				return
			}
			defer sess.Close()
			go func() { <-ctx.Done(); sess.Close() }()
			// Serve blocks keeping the session responsive; the probe opens no streams, so the
			// dialer is never actually used.
			_ = sess.Serve(func(_, _ string) (net.Conn, error) {
				return nil, fmt.Errorf("probe: no forwarding")
			})
		}()
	}
}

// Probe runs on the exit (foreign) side: dial one transport at host:basePort+index, do the real secure
// handshake, and time a live ping. timeout bounds the whole attempt.
func Probe(base *config.Config, name, host string, port int, timeout time.Duration) Result {
	return ProbeAddr(base, name, net.JoinHostPort(host, strconv.Itoa(port)), timeout)
}

// ProbeAddr is Probe against an explicit peer address (used by `status` on the live tunnel port).
func ProbeAddr(base *config.Config, name, peerAddr string, timeout time.Duration) Result {
	tr, err := transport.Get(name)
	r := Result{Transport: name}
	if err != nil {
		r.Err = err.Error()
		return r
	}
	r.Category = tr.Category()
	psk, err := config.DecodePSK(base.PSK)
	if err != nil {
		r.Err = err.Error()
		return r
	}
	cfg := *base
	cfg.Transport = name
	cfg.Peer = peerAddr

	type outcome struct {
		res Result
	}
	ch := make(chan outcome, 1)
	go func() {
		start := time.Now()
		raw, err := tr.Dial(&cfg)
		if err != nil {
			ch <- outcome{Result{Transport: name, Category: r.Category, Err: err.Error()}}
			return
		}
		connect := time.Since(start)
		sec, err := core.Secure(raw, psk, true)
		if err != nil {
			raw.Close()
			ch <- outcome{Result{Transport: name, Category: r.Category, Connect: connect, Err: "handshake: " + err.Error()}}
			return
		}
		sess, err := core.NewSession(sec, false)
		if err != nil {
			sec.Close()
			ch <- outcome{Result{Transport: name, Category: r.Category, Connect: connect, Err: err.Error()}}
			return
		}
		defer sess.Close()
		ping, err := sess.Ping()
		if err != nil {
			ch <- outcome{Result{Transport: name, Category: r.Category, Connect: connect, Err: "ping: " + err.Error()}}
			return
		}
		ch <- outcome{Result{Transport: name, Category: r.Category, OK: true, Connect: connect, Ping: ping}}
	}()

	select {
	case o := <-ch:
		return o.res
	case <-time.After(timeout):
		return Result{Transport: name, Category: r.Category, Err: "timeout"}
	}
}

// ProbeAll probes every named transport in order and returns the results ranked: connected first,
// then by lowest ping.
func ProbeAll(base *config.Config, names []string, host string, basePort int, timeout time.Duration) []Result {
	out := make([]Result, 0, len(names))
	for i, name := range names {
		out = append(out, Probe(base, name, host, basePort+i, timeout))
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].OK != out[b].OK {
			return out[a].OK
		}
		return out[a].Ping < out[b].Ping
	})
	return out
}
