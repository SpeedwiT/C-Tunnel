// Package transport holds C Tunnel's pluggable, DPI-resistant carriers. A Transport only needs to
// move bytes and look innocuous on the wire; the core layer adds encryption, auth and multiplexing.
package transport

import (
	"fmt"
	"net"
	"sort"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// Transport is one way to carry the tunnel between the relay (Iran) and the exit (abroad).
type Transport interface {
	Name() string
	// Listen opens the relay-side listener yielding raw carrier connections.
	Listen(cfg *config.Config) (net.Listener, error)
	// Dial opens the exit-side carrier connection to the relay.
	Dial(cfg *config.Config) (net.Conn, error)
}

var registry = map[string]Transport{}

func register(t Transport) { registry[t.Name()] = t }

// Get returns a transport by name.
func Get(name string) (Transport, error) {
	t, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown transport %q (have: %v)", name, Names())
	}
	return t, nil
}

// Names lists the registered transports, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
