package linktest

import (
	"net"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func freeBasePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return p
}

func TestProbeAllOverLoopback(t *testing.T) {
	base := &config.Config{
		Role: config.RoleRelay,
		Mode: config.ModeReverse,
		PSK:  config.GenPSK(),
		SNI:  "www.cloudflare.com",
	}
	names := []string{"ctls", "cws", "chttp", "cfrag", "cquic"}
	basePort := freeBasePort(t)

	stop, err := Server(base, names, basePort)
	if err != nil {
		t.Fatalf("link-test server: %v", err)
	}
	defer stop()
	time.Sleep(150 * time.Millisecond) // let listeners come up

	results := ProbeAll(base, names, "127.0.0.1", basePort, 5*time.Second)
	if len(results) != len(names) {
		t.Fatalf("want %d results, got %d", len(names), len(results))
	}
	ok := 0
	for _, r := range results {
		if r.OK {
			ok++
			if r.Ping <= 0 {
				t.Errorf("%s connected but ping not measured", r.Transport)
			}
		} else {
			t.Logf("%s did not connect: %s", r.Transport, r.Err)
		}
	}
	if ok == 0 {
		t.Fatal("expected at least one transport to connect over loopback")
	}
	// Ranking: every OK result must sort before every failed one.
	seenFail := false
	for _, r := range results {
		if !r.OK {
			seenFail = true
		} else if seenFail {
			t.Fatal("ranking broken: a connected transport sorted after a failed one")
		}
	}
}

func TestProbeWrongPSKFails(t *testing.T) {
	server := &config.Config{Role: config.RoleRelay, PSK: config.GenPSK()}
	names := []string{"ctls"}
	basePort := freeBasePort(t)
	stop, err := Server(server, names, basePort)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	time.Sleep(100 * time.Millisecond)

	client := &config.Config{Role: config.RoleExit, PSK: config.GenPSK()} // different PSK
	r := Probe(client, "ctls", "127.0.0.1", basePort, 4*time.Second)
	if r.OK {
		t.Fatal("probe must fail when the PSK does not match")
	}
}
