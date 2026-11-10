// Package config is the on-disk configuration for a C Tunnel node.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GenPSK returns a fresh random 32-byte pre-shared key, base64-encoded.
func GenPSK() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodePSK decodes a base64 PSK into raw bytes.
func DecodePSK(psk string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(psk)
	if err != nil {
		return nil, fmt.Errorf("psk must be base64: %w", err)
	}
	if len(b) < 16 {
		return nil, fmt.Errorf("psk too short")
	}
	return b, nil
}

// Role of this node.
const (
	RoleRelay = "relay" // the Iran server: public entry, listens for the exit and for users
	RoleExit  = "exit"  // the foreign server: dials the relay and reaches the real services
)

// Mode is the tunnel topology.
const (
	// ModeReverse (تونل معکوس): the foreign server dials in to the Iran server, so the Iran
	// server needs one open port and the foreign server needs none. Port-forward oriented.
	ModeReverse = "reverse"
	// ModeDirect (تونل مستقیم): the Iran server dials out to the foreign server, so the Iran
	// server needs NO inbound port. A full L3 IP tunnel (TUN + GRE-in-Noise) joins both hosts
	// on a private subnet; port forwards ride on top of it.
	ModeDirect = "direct"
)

// Forward is one port the relay exposes, routed through the tunnel to a target on the exit side.
type Forward struct {
	Name   string `json:"name"`
	Listen int    `json:"listen"`          // port opened on the relay (what users connect to)
	Target string `json:"target"`          // host:port the exit connects to (e.g. 127.0.0.1:443)
	Proto  string `json:"proto,omitempty"` // tcp (default) | udp
}

// Config is the whole node configuration.
type Config struct {
	Role      string    `json:"role"`
	Mode      string    `json:"mode,omitempty"`   // reverse (default) | direct
	Transport string    `json:"transport"`        // ctls | cquic | cws | ... (see internal/transport)
	Listen    string    `json:"listen,omitempty"` // relay: address the tunnel listens on, e.g. :7000
	Peer      string    `json:"peer,omitempty"`   // exit: relay address to dial, e.g. 1.2.3.4:7000
	PSK       string    `json:"psk"`              // shared secret (base64, 32 bytes)
	Forwards  []Forward `json:"forwards"`
	SNI       string    `json:"sni,omitempty"` // transport hint: TLS server name to present/expect
	// TunSubnet is the private /30 for ModeDirect, e.g. "10.72.0.1/30" (relay) — the exit takes .2.
	TunSubnet string `json:"tun_subnet,omitempty"`
	Creator   string `json:"creator,omitempty"`
	GitHub    string `json:"github,omitempty"`
}

// DefaultPath is where the config lives on a server install.
func DefaultPath() string {
	if p := os.Getenv("CTUNNEL_CONFIG"); p != "" {
		return p
	}
	return "/etc/ctunnel/config.json"
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (c *Config) Validate() error {
	switch c.Role {
	case RoleRelay:
		if c.Listen == "" {
			return fmt.Errorf("relay needs a listen address")
		}
	case RoleExit:
		if c.Peer == "" {
			return fmt.Errorf("exit needs a peer (relay) address")
		}
	default:
		return fmt.Errorf("role must be %q or %q", RoleRelay, RoleExit)
	}
	if c.PSK == "" {
		return fmt.Errorf("psk is required")
	}
	if c.Transport == "" {
		c.Transport = "ctls"
	}
	if c.Mode == "" {
		c.Mode = ModeReverse
	}
	if c.Mode != ModeReverse && c.Mode != ModeDirect {
		return fmt.Errorf("mode must be %q or %q", ModeReverse, ModeDirect)
	}
	return nil
}
