// Package setupcode encodes everything the second server needs to join a tunnel into a single short,
// copy-pasteable code. After the Iran server is installed it prints a code; the foreign server consumes
// it in one step (ctunnel join <code>) and is configured instantly — no manual field entry.
package setupcode

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/SpeedwiT/c-tunnel/internal/broker"
	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// prefix tags the long (offline) code so a user can recognise it and so we can version the format.
const prefix = "CTUN1-"

// shortPrefix tags the short, broker-backed pairing code.
const shortPrefix = "C-Tunnel-"

// shortAlphabet is Crockford base32 (no I, L, O, U) without padding — easy to read and type aloud.
var shortAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// Setup is the compact payload carried by a setup code. Field names are short to keep the code small.
type Setup struct {
	V         int               `json:"v"`            // format version
	Mode      string            `json:"m"`            // reverse | direct
	Transport string            `json:"t"`            // transport name
	PSK       string            `json:"k"`            // shared secret (base64)
	SNI       string            `json:"s,omitempty"`  // TLS server-name hint
	Peer      string            `json:"p"`            // address the receiver relates to (see ExitConfig/RelayDialInfo)
	Broker    string            `json:"b,omitempty"`  // rendezvous broker for the crdv transport
	Members   []string          `json:"ms,omitempty"` // cbond member transports
	TunSubnet string            `json:"n,omitempty"`  // direct mode /30
	Forwards  []config.Forward  `json:"f,omitempty"`  // informational for the operator; relay drives routing
	Extra     map[string]string `json:"x,omitempty"`  // forward-compat room
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
		Broker:    cfg.Broker,
		Members:   cfg.Members,
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
		Broker:    s.Broker,
		Members:   s.Members,
		TunSubnet: s.TunSubnet,
		Forwards:  s.Forwards,
	}
	if c.Transport == "cbond" {
		c.Role = config.RoleExit
		c.Peer = s.Peer
		return c
	}
	if c.Transport == "crdv" {
		// Both sides dial the broker; no listen/peer needed.
		c.Role = config.RoleExit
		return c
	}
	if c.Mode == config.ModeDirect {
		// Direct mode: Iran dials out, the foreign server listens. The foreign node stays the exit
		// role; RunDirect uses the role only to decide who dials.
		c.Role = config.RoleExit
		c.Listen = listenPart(s.Peer)
	} else {
		// Foreign dials the Iran relay.
		c.Role = config.RoleExit
		c.Peer = s.Peer
	}
	return c
}

// --- short, broker-backed pairing code: C-Tunnel-XXXXXXXXXXXXXXXX ---

const shortTokenBytes = 10 // 16 base32 chars; first 4 bytes key the broker store, all 10 derive the key

// MintShort stores the (encrypted) setup on the broker and returns a short pairing code. The broker
// only ever holds ciphertext — the key is derived from the full token, which lives only in the code.
func MintShort(brokerAddr string, s Setup) (string, error) {
	token := make([]byte, shortTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	plain, _ := json.Marshal(s)
	sealed, err := seal(shortKey(token), plain)
	if err != nil {
		return "", err
	}
	id := shortAlphabet.EncodeToString(token[:4])
	if err := broker.PutBlob(brokerAddr, id, sealed); err != nil {
		return "", err
	}
	return shortPrefix + shortAlphabet.EncodeToString(token), nil
}

// ResolveShort fetches and decrypts a setup from the broker given a short pairing code.
func ResolveShort(brokerAddr, code string) (Setup, error) {
	var s Setup
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, shortPrefix) {
		return s, fmt.Errorf("not a short C-Tunnel code")
	}
	token, err := shortAlphabet.DecodeString(strings.TrimPrefix(code, shortPrefix))
	if err != nil || len(token) != shortTokenBytes {
		return s, fmt.Errorf("short code is malformed")
	}
	id := shortAlphabet.EncodeToString(token[:4])
	sealed, err := broker.GetBlob(brokerAddr, id)
	if err != nil {
		return s, err
	}
	plain, err := open(shortKey(token), sealed)
	if err != nil {
		return s, fmt.Errorf("short code did not match the stored setup")
	}
	if err := json.Unmarshal(plain, &s); err != nil {
		return s, fmt.Errorf("stored setup is invalid: %w", err)
	}
	if s.V != 1 || s.PSK == "" || s.Transport == "" {
		return s, fmt.Errorf("stored setup is incomplete")
	}
	return s, nil
}

// IsShort reports whether a code is the short, broker-backed form.
func IsShort(code string) bool { return strings.HasPrefix(strings.TrimSpace(code), shortPrefix) }

func shortKey(token []byte) []byte {
	h := sha256.Sum256(append([]byte("c-tunnel-short"), token...))
	return h[:]
}

func seal(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func open(key, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
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
