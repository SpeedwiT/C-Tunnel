// Package setupcode encodes everything the second server needs to join a tunnel into a single short,
// copy-pasteable code. After the Iran server is installed it prints a code; the foreign server consumes
// it in one step (ctunnel join <code>) and is configured instantly — no manual field entry.
package setupcode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// prefix tags the code so a user can recognise it and so we can version the format.
const prefix = "CTUN1-"

// Setup is the compact payload carried by a setup code. Field names are short to keep the code small.
type Setup struct {
	V         int               `json:"v"`           // format version
	Mode      string            `json:"m"`           // reverse | direct
	Transport string            `json:"t"`           // transport name
	PSK       string            `json:"k"`           // shared secret (base64)
	SNI       string            `json:"s,omitempty"` // TLS server-name hint
	Peer      string            `json:"p"`           // address the receiver relates to (see ExitConfig/RelayDialInfo)
	TunSubnet string            `json:"n,omitempty"` // direct mode /30
	Forwards  []config.Forward  `json:"f,omitempty"` // informational for the operator; relay drives routing
	Extra     map[string]string `json:"x,omitempty"` // forward-compat room
}

// FromRelay builds the setup code payload the FOREIGN (exit) server needs, given the Iran (relay)
// config and the public address the foreign server will relate to.
//
//   - reverse mode: publicAddr is the Iran server's own public host:port (the exit dials it).
//   - direct mode:  publicAddr is the FOREIGN server's listen host:port (Iran dials out to it);
//     the operator supplies it during Iran setup because Iran must know where to call.
func FromRelay(cfg *config.Config, publicAddr string) Setup {
	return Setup{
		V:         1,
		Mode:      cfg.Mode,
		Transport: cfg.Transport,
		PSK:       cfg.PSK,
		SNI:       cfg.SNI,
		Peer:      publicAddr,
		TunSubnet: cfg.TunSubnet,
		Forwards:  cfg.Forwards,
	}
}

// Encode renders a Setup as a single shareable code: CTUN1-<base64url(json)>-<crc>.
// The crc lets the receiver detect a mistyped/truncated code before trying to connect.
func Encode(s Setup) string {
	raw, _ := json.Marshal(s)
	body := base64.RawURLEncoding.EncodeToString(raw)
	sum := crc32.ChecksumIEEE([]byte(body))
	return fmt.Sprintf("%s%s-%08x", prefix, body, sum)
}

// Decode parses a setup code back into a Setup, verifying the prefix and checksum.
func Decode(code string) (Setup, error) {
	var s Setup
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, prefix) {
		return s, fmt.Errorf("not a C Tunnel setup code")
	}
	rest := strings.TrimPrefix(code, prefix)
	dash := strings.LastIndexByte(rest, '-')
	if dash < 0 {
		return s, fmt.Errorf("setup code is malformed (missing checksum)")
	}
	body, want := rest[:dash], rest[dash+1:]
	got := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(body)))
	if got != want {
		return s, fmt.Errorf("setup code checksum mismatch — it was copied incompletely")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return s, fmt.Errorf("setup code is not valid base64: %w", err)
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("setup code payload is invalid: %w", err)
	}
	if s.V != 1 {
		return s, fmt.Errorf("unsupported setup code version %d (upgrade C Tunnel)", s.V)
	}
	if s.PSK == "" || s.Transport == "" || s.Peer == "" {
		return s, fmt.Errorf("setup code is missing required fields")
	}
	return s, nil
}

// ExitConfig turns a decoded Setup into the foreign (exit) server's config.
//
//   - reverse mode: the exit dials Peer (the Iran server's public address).
//   - direct mode:  the exit LISTENS on Peer's port (Iran dials out to it), so it takes the relay role
//     on the wire while staying the "foreign" node logically; the full-IP layer is symmetric.
func (s Setup) ExitConfig() *config.Config {
	c := &config.Config{
		Mode:      orDefault(s.Mode, config.ModeReverse),
		Transport: s.Transport,
		PSK:       s.PSK,
		SNI:       s.SNI,
		TunSubnet: s.TunSubnet,
		Forwards:  s.Forwards,
	}
	if c.Mode == config.ModeDirect {
		// Iran dials out; the foreign server is the one that listens.
		c.Role = config.RoleRelay
		c.Listen = listenPart(s.Peer)
	} else {
		// Foreign dials the Iran relay.
		c.Role = config.RoleExit
		c.Peer = s.Peer
	}
	return c
}

func listenPart(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return ":" + addr[i+1:]
	}
	return addr
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
