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
	"github.com/SpeedwiT/c-tunnel/internal/transport"
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

// every registered transport must carry the tunnel end-to-end.
func TestAllTransportsForward(t *testing.T) {
	for _, name := range transport.Names() {
		name := name
		if name == "crdv" {
			continue // rendezvous needs a broker; covered by TestRendezvousForward
		}
		if name == "cspoof" {
			continue // forged-source transport needs peer-on-both-sides + root; see cspoof tests
		}
		t.Run(name, func(t *testing.T) {
			target := echoServer(t)
			transportPort := freePort(t)
			forwardPort := freePort(t)
			psk := config.GenPSK()

			relay := &config.Config{
				Role: config.RoleRelay, Transport: name, PSK: psk,
				Listen:   fmt.Sprintf("127.0.0.1:%d", transportPort),
				Forwards: []config.Forward{{Name: "svc", Listen: forwardPort, Target: target}},
			}
			exit := &config.Config{
				Role: config.RoleExit, Transport: name, PSK: psk,
				Peer: fmt.Sprintf("127.0.0.1:%d", transportPort),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go RunRelay(ctx, relay)
			time.Sleep(200 * time.Millisecond)
			go RunExit(ctx, exit)

			deadline := time.Now().Add(10 * time.Second)
			var lastErr error
			for time.Now().Before(deadline) {
				if err := roundTrip(forwardPort, []byte("hello c-tunnel via "+name)); err != nil {
					lastErr = err
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return
			}
			t.Fatalf("[%s] echo through tunnel never succeeded: %v", name, lastErr)
		})
	}
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
