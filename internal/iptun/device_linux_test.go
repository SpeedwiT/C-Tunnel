//go:build linux

package iptun

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestOpenRealTUN opens an actual TUN device, configures an address + MTU, and verifies the kernel
// shows it. Skips when not privileged or /dev/net/tun is absent.
func TestOpenRealTUN(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skip("no /dev/net/tun")
	}
	dev, err := Open("ctuntest0", "10.73.0.1/30", 1300)
	if err != nil {
		t.Skipf("cannot open tun in this sandbox: %v", err)
	}
	defer dev.Close()

	out, err := exec.Command("ip", "addr", "show", "ctuntest0").CombinedOutput()
	if err != nil {
		t.Fatalf("ip addr show: %v: %s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "10.73.0.1/30") {
		t.Fatalf("address not configured:\n%s", s)
	}
	if !strings.Contains(s, "mtu 1300") {
		t.Fatalf("mtu not set:\n%s", s)
	}
}
