package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/broker"
	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// TestRendezvousForward proves the either-IP-blocked path: neither the relay nor the exit opens an
// inbound tunnel port — both dial OUT to the broker, which pairs and pipes them — and a user
// connection through the relay still reaches a target on the exit side.
func TestRendezvousForward(t *testing.T) {
	bk, err := broker.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bk.Close()
	brokerAddr := bk.Addr().String()

	target := echoServer(t)
	userPort := freePort(t)
	psk := config.GenPSK()

	relayCfg := &config.Config{
		Role: config.RoleRelay, Transport: "crdv", Broker: brokerAddr, PSK: psk,
		Forwards: []config.Forward{{Name: "echo", Listen: userPort, Target: target}},
	}
	exitCfg := &config.Config{
		Role: config.RoleExit, Transport: "crdv", Broker: brokerAddr, PSK: psk,
	}
	if err := relayCfg.Validate(); err != nil {
		t.Fatalf("relay cfg: %v", err)
	}
	if err := exitCfg.Validate(); err != nil {
		t.Fatalf("exit cfg: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunRelay(ctx, relayCfg)
	go RunExit(ctx, exitCfg)

	deadline := time.Now().Add(12 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := roundTrip(userPort, []byte("hello via rendezvous")); err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return
	}
	t.Fatalf("rendezvous round-trip never succeeded: %v", lastErr)
}
