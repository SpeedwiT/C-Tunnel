package setupcode

import (
	"strings"
	"testing"

	"github.com/SpeedwiT/c-tunnel/internal/broker"
	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func relayCfg() *config.Config {
	return &config.Config{
		Role:      config.RoleRelay,
		Mode:      config.ModeReverse,
		Transport: "cquic",
		Listen:    ":7000",
		PSK:       config.GenPSK(),
		SNI:       "www.cloudflare.com",
		Forwards: []config.Forward{
			{Name: "web", Listen: 8443, Target: "127.0.0.1:443", Proto: "tcp"},
		},
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cfg := relayCfg()
	code := Encode(FromRelay(cfg, "1.2.3.4:7000"))
	if !strings.HasPrefix(code, prefix) {
		t.Fatalf("code missing prefix: %q", code)
	}
	got, err := Decode(code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PSK != cfg.PSK || got.Transport != "cquic" || got.Peer != "1.2.3.4:7000" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Mode != config.ModeReverse || len(got.Forwards) != 1 {
		t.Fatalf("mode/forwards lost: %+v", got)
	}
}

func TestReverseExitConfigDials(t *testing.T) {
	cfg := relayCfg()
	s, err := Decode(Encode(FromRelay(cfg, "1.2.3.4:7000")))
	if err != nil {
		t.Fatal(err)
	}
	ec := s.ExitConfig()
	if ec.Role != config.RoleExit {
		t.Fatalf("reverse exit must dial, got role %q", ec.Role)
	}
	if ec.Peer != "1.2.3.4:7000" {
		t.Fatalf("exit peer = %q", ec.Peer)
	}
	if err := ec.Validate(); err != nil {
		t.Fatalf("exit config invalid: %v", err)
	}
}

func TestDirectExitConfigListens(t *testing.T) {
	cfg := relayCfg()
	cfg.Mode = config.ModeDirect
	// In direct mode the foreign server listens; Peer is the foreign listen address.
	s, err := Decode(Encode(FromRelay(cfg, "0.0.0.0:7000")))
	if err != nil {
		t.Fatal(err)
	}
	ec := s.ExitConfig()
	if ec.Role != config.RoleRelay {
		t.Fatalf("direct foreign side must listen, got role %q", ec.Role)
	}
	if ec.Listen != ":7000" {
		t.Fatalf("listen = %q", ec.Listen)
	}
	if err := ec.Validate(); err != nil {
		t.Fatalf("direct config invalid: %v", err)
	}
}

func TestShortCodeViaBroker(t *testing.T) {
	bk, err := broker.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bk.Close()
	addr := bk.Addr().String()

	cfg := relayCfg()
	cfg.Transport = "crdv"
	cfg.Broker = addr
	code, err := MintShort(addr, FromRelay(cfg, "0.0.0.0:0"))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !IsShort(code) || !strings.HasPrefix(code, shortPrefix) {
		t.Fatalf("bad short code: %q", code)
	}
	got, err := ResolveShort(addr, code)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.PSK != cfg.PSK || got.Broker != addr || got.Transport != "crdv" {
		t.Fatalf("short round-trip mismatch: %+v", got)
	}
	ec := got.ExitConfig()
	if ec.Role != config.RoleExit || ec.Broker != addr {
		t.Fatalf("crdv exit config wrong: %+v", ec)
	}
	if err := ec.Validate(); err != nil {
		t.Fatalf("crdv exit invalid: %v", err)
	}
	// A wrong code (right length, wrong bytes) must not resolve.
	if _, err := ResolveShort(addr, shortPrefix+"00000000000000000"); err == nil {
		t.Fatal("expected wrong short code to fail")
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	code := Encode(FromRelay(relayCfg(), "1.2.3.4:7000"))
	// Flip a character in the body to break the checksum.
	bad := code[:len(prefix)+5] + "X" + code[len(prefix)+6:]
	if _, err := Decode(bad); err == nil {
		t.Fatal("expected checksum mismatch to be rejected")
	}
	if _, err := Decode("hello world"); err == nil {
		t.Fatal("expected non-code to be rejected")
	}
}
