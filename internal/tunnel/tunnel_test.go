package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// echoServer accepts connections and echoes bytes back; returns its address.
func echoServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	return l.Addr().String()
}

func TestTunnelForwardsThroughCTLS(t *testing.T) {
	target := echoServer(t)
	transportPort := freePort(t)
	forwardPort := freePort(t)
	psk := config.GenPSK()

	relay := &config.Config{
		Role: config.RoleRelay, Transport: "ctls", PSK: psk,
		Listen:   fmt.Sprintf("127.0.0.1:%d", transportPort),
		Forwards: []config.Forward{{Name: "svc", Listen: forwardPort, Target: target}},
	}
	exit := &config.Config{
		Role: config.RoleExit, Transport: "ctls", PSK: psk,
		Peer: fmt.Sprintf("127.0.0.1:%d", transportPort),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunRelay(ctx, relay)
	time.Sleep(150 * time.Millisecond)
	go RunExit(ctx, exit)

	// retry until the tunnel is live and the echo round-trips
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := roundTrip(forwardPort, []byte("hello c-tunnel")); err != nil {
			lastErr = err
			time.Sleep(150 * time.Millisecond)
			continue
		}
		return // success
	}
	t.Fatalf("echo through tunnel never succeeded: %v", lastErr)
}

func TestWrongPSKRejected(t *testing.T) {
	target := echoServer(t)
	transportPort := freePort(t)
	forwardPort := freePort(t)

	relay := &config.Config{
		Role: config.RoleRelay, Transport: "ctls", PSK: config.GenPSK(),
		Listen:   fmt.Sprintf("127.0.0.1:%d", transportPort),
		Forwards: []config.Forward{{Name: "svc", Listen: forwardPort, Target: target}},
	}
	exit := &config.Config{
		Role: config.RoleExit, Transport: "ctls", PSK: config.GenPSK(), // different PSK
		Peer: fmt.Sprintf("127.0.0.1:%d", transportPort),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunRelay(ctx, relay)
	time.Sleep(150 * time.Millisecond)
	go RunExit(ctx, exit)

	// the tunnel must never establish, so the echo must keep failing
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := roundTrip(forwardPort, []byte("x")); err == nil {
			t.Fatal("tunnel established with the wrong PSK")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func roundTrip(port int, msg []byte) error {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write(msg); err != nil {
		return err
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if !bytes.Equal(buf, msg) {
		return fmt.Errorf("echo mismatch: got %q", buf)
	}
	return nil
}
