package iptun

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/core"
)

func TestFrameRoundTrip(t *testing.T) {
	pkt := bytes.Repeat([]byte{0x45, 0x00, 0xDE, 0xAD}, 10) // 40 bytes of pseudo-IP
	var buf bytes.Buffer
	if err := writeFrame(&buf, pkt); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pkt) {
		t.Fatalf("round-trip mismatch:\n got %x\nwant %x", got, pkt)
	}
}

func TestPeerIP(t *testing.T) {
	if p, _ := PeerIP("10.72.0.1/30"); p != "10.72.0.2" {
		t.Fatalf(".1 peer = %q", p)
	}
	if p, _ := PeerIP("10.72.0.2/30"); p != "10.72.0.1" {
		t.Fatalf(".2 peer = %q", p)
	}
	// .0 is the network address (low bits 00) and .3 the broadcast (11) — neither is a usable host.
	if _, err := PeerIP("10.72.0.0/30"); err == nil {
		t.Fatal("network address .0 should be rejected")
	}
	if _, err := PeerIP("10.72.0.3/30"); err == nil {
		t.Fatal("broadcast .3 should be rejected")
	}
}

// fakeDev is an in-memory stand-in for a TUN device.
type fakeDev struct {
	in     chan []byte
	out    chan []byte
	closed chan struct{}
}

func newFakeDev() *fakeDev {
	return &fakeDev{in: make(chan []byte, 16), out: make(chan []byte, 16), closed: make(chan struct{})}
}
func (d *fakeDev) Read(p []byte) (int, error) {
	select {
	case b := <-d.in:
		return copy(p, b), nil
	case <-d.closed:
		return 0, io.EOF
	}
}
func (d *fakeDev) Write(p []byte) (int, error) {
	b := append([]byte(nil), p...)
	select {
	case d.out <- b:
	case <-d.closed:
	}
	return len(p), nil
}
func (d *fakeDev) Close() error { close(d.closed); return nil }

// TestPumpOverSecureCore proves IP packets travel both ways through the GRE framing over the
// encrypted core, using in-memory devices (no real TUN needed).
func TestPumpOverSecureCore(t *testing.T) {
	a, b := net.Pipe()
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = byte(i)
	}
	secs := make(chan net.Conn, 2)
	go func() {
		s, err := core.Secure(a, psk, true)
		if err != nil {
			t.Error(err)
		}
		secs <- s
	}()
	go func() {
		s, err := core.Secure(b, psk, false)
		if err != nil {
			t.Error(err)
		}
		secs <- s
	}()
	s1, s2 := <-secs, <-secs
	_ = config.RoleRelay

	devA, devB := newFakeDev(), newFakeDev()
	go Pump(s1, devA, 1400)
	go Pump(s2, devB, 1400)

	pkt := append([]byte{0x45, 0x00}, bytes.Repeat([]byte{0xAB}, 58)...) // 60-byte packet
	devA.in <- pkt
	select {
	case got := <-devB.out:
		if !bytes.Equal(got, pkt) {
			t.Fatalf("A->B packet mismatch")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("packet A->B never arrived")
	}

	// reverse direction
	pkt2 := append([]byte{0x45, 0x00}, bytes.Repeat([]byte{0xCD}, 20)...)
	devB.in <- pkt2
	select {
	case got := <-devA.out:
		if !bytes.Equal(got, pkt2) {
			t.Fatal("B->A packet mismatch")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("packet B->A never arrived")
	}
}
