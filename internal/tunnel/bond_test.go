package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// TestBondedTunnelForward runs the bonded transport across two real member transports (ctls + ckcp)
// over loopback and verifies a user connection is forwarded through it end-to-end.
func TestBondedTunnelForward(t *testing.T) {
	target := echoServer(t)
	base := freePort(t) // members use base, base+1
	forwardPort := freePort(t)
	psk := config.GenPSK()

	relay := &config.Config{
		Role: config.RoleRelay, Transport: "cbond", Members: []string{"ctls", "ckcp"}, PSK: psk,
		Listen:   netAddr(base),
		Forwards: []config.Forward{{Name: "svc", Listen: forwardPort, Target: target}},
	}
	exit := &config.Config{
		Role: config.RoleExit, Transport: "cbond", Members: []string{"ctls", "ckcp"}, PSK: psk,
		Peer: netAddr(base),
	}
	if err := relay.Validate(); err != nil {
		t.Fatalf("relay cfg: %v", err)
	}
	if err := exit.Validate(); err != nil {
		t.Fatalf("exit cfg: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunBondRelay(ctx, relay)
	time.Sleep(300 * time.Millisecond)
	go RunBondExit(ctx, exit)

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := roundTrip(forwardPort, []byte("hello bonded c-tunnel")); err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return
	}
	t.Fatalf("bonded tunnel round-trip never succeeded: %v", lastErr)
}

func netAddr(port int) string { return "127.0.0.1:" + itoa(port) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
