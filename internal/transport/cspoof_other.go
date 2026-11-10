//go:build !linux

package transport

import (
	"fmt"
	"net"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// newSpoofConn is unavailable off Linux (it needs raw sockets); the stub keeps the registry consistent.
func newSpoofConn(_ *config.Config) (net.PacketConn, error) {
	return nil, fmt.Errorf("cspoof is only supported on Linux")
}
