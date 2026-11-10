//go:build linux

package iptun

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

// Open creates (or opens) a TUN interface named ifName, assigns cidr (e.g. "10.72.0.1/30"), sets the
// MTU and brings it up. Requires CAP_NET_ADMIN (root).
func Open(ifName, cidr string, mtu int) (Device, error) {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = ifName
	ifce, err := water.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("open tun %q (need root/CAP_NET_ADMIN): %w", ifName, err)
	}
	name := ifce.Name()
	if err := configure(name, cidr, mtu); err != nil {
		ifce.Close()
		return nil, err
	}
	return ifce, nil
}

func configure(name, cidr string, mtu int) error {
	steps := [][]string{
		{"addr", "add", cidr, "dev", name},
		{"link", "set", "dev", name, "mtu", fmt.Sprint(mtu)},
		{"link", "set", "dev", name, "up"},
	}
	for _, args := range steps {
		if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			// "addr add" fails harmlessly if the address is already present on a reused device.
			if args[0] == "addr" && strings.Contains(string(out), "File exists") {
				continue
			}
			return fmt.Errorf("ip %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// PeerIP returns the other end's address for a /30 (if we are .1 the peer is .2, and vice-versa).
// cidr is this host's address with prefix, e.g. "10.72.0.1/30".
func PeerIP(cidr string) (string, error) {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("iptun: need an IPv4 /30")
	}
	peer := make(net.IP, 4)
	copy(peer, v4)
	switch v4[3] & 0x3 {
	case 1:
		peer[3] = (v4[3] &^ 0x3) | 2
	case 2:
		peer[3] = (v4[3] &^ 0x3) | 1
	default:
		return "", fmt.Errorf("iptun: host part of %s must be .1 or .2 in a /30", cidr)
	}
	return peer.String(), nil
}
