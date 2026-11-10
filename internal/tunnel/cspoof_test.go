//go:build linux

package tunnel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// TestSpoofTunnelForward runs the real forged-source transport end-to-end: both sides send raw IPv4/UDP
// packets with a fake source IP, receive on a normal UDP socket, and carry the tunnel over KCP+core.
// Needs root/CAP_NET_RAW; skips otherwise.
func TestSpoofTunnelForward(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("cspoof needs root/CAP_NET_RAW")
	}
	target := echoServer(t)
	portR := freePort(t)
	portE := freePort(t)
	forwardPort := freePort(t)
	psk := config.GenPSK()

	relay := &config.Config{
		Role: config.RoleRelay, Transport: "cspoof", PSK: psk,
		Listen: netAddr(portR), Peer: netAddr(portE), SpoofSrc: "10.9.8.7",
		Forwards: []config.Forward{{Name: "svc", Listen: forwardPort, Target: target}},
	}
	exit := &config.Config{
		Role: config.RoleExit, Transport: "cspoof", PSK: psk,
		Listen: netAddr(portE), Peer: netAddr(portR), SpoofSrc: "10.9.8.8",
	}
	if err := relay.Validate(); err != nil {
		t.Fatalf("relay cfg: %v", err)
	}
	if err := exit.Validate(); err != nil {
		t.Fatalf("exit cfg: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunRelay(ctx, relay)
	time.Sleep(300 * time.Millisecond)
	go RunExit(ctx, exit)

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := roundTrip(forwardPort, []byte("hello spoofed c-tunnel")); err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return
	}
	t.Fatalf("spoofed tunnel round-trip never succeeded: %v", lastErr)
}
