// Package iptun implements C Tunnel's direct-mode data path: a full Layer-3 IP tunnel. A TUN interface
// on each host carries whole IP packets, which are GRE-encapsulated and sent over the secure core
// (mutually authenticated X25519+PSK, ChaCha20-Poly1305 with a per-direction counter nonce that also
// gives replay protection — the "Noise" layer). Both hosts share a private /30, so anything can be
// routed between them, and forwarded ports ride on top.
package iptun

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// Device is the minimal TUN interface the pump needs (satisfied by *water.Interface and test fakes).
type Device interface {
	io.ReadWriteCloser
}

// Frame layout on the wire: [2-byte total length][4-byte GRE header][IP packet].
// The length covers the GRE header + packet so the reader can find packet boundaries on the byte
// stream the secure core gives us.
const greHeaderLen = 4

// greHeaderV4 is a minimal GRE header: flags/version = 0, protocol type = 0x0800 (IPv4).
var greHeaderV4 = [greHeaderLen]byte{0x00, 0x00, 0x08, 0x00}

// maxPacket caps a single IP packet we will frame (guards against a desync reading a bogus length).
const maxPacket = 65535

// writeFrame GRE-encapsulates one IP packet and writes it to w.
func writeFrame(w io.Writer, pkt []byte) error {
	if len(pkt) == 0 || len(pkt) > maxPacket {
		return nil // drop empties / oversized silently; the kernel won't hand us these
	}
	total := greHeaderLen + len(pkt)
	hdr := make([]byte, 2+greHeaderLen)
	binary.BigEndian.PutUint16(hdr[:2], uint16(total))
	copy(hdr[2:], greHeaderV4[:])
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(pkt)
	return err
}

// readFrame reads one GRE-encapsulated IP packet from r and returns the inner packet.
func readFrame(r *bufio.Reader) ([]byte, error) {
	var lenb [2]byte
	if _, err := io.ReadFull(r, lenb[:]); err != nil {
		return nil, err
	}
	total := int(binary.BigEndian.Uint16(lenb[:]))
	if total < greHeaderLen || total > greHeaderLen+maxPacket {
		return nil, fmt.Errorf("iptun: bad frame length %d", total)
	}
	buf := make([]byte, total)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	// buf[:greHeaderLen] is the GRE header; the rest is the IP packet.
	return buf[greHeaderLen:], nil
}

// Pump runs the bidirectional data path between the TUN device and the secure carrier until either
// side errors (e.g. the carrier drops), then returns so the caller can reconnect.
func Pump(conn net.Conn, dev Device, mtu int) error {
	errc := make(chan error, 2)
	// device -> carrier
	go func() {
		buf := make([]byte, mtu+greHeaderLen+64)
		for {
			n, err := dev.Read(buf)
			if err != nil {
				errc <- err
				return
			}
			if err := writeFrame(conn, buf[:n]); err != nil {
				errc <- err
				return
			}
		}
	}()
	// carrier -> device
	go func() {
		br := bufio.NewReaderSize(conn, 1<<16)
		for {
			pkt, err := readFrame(br)
			if err != nil {
				errc <- err
				return
			}
			if _, err := dev.Write(pkt); err != nil {
				errc <- err
				return
			}
		}
	}()
	return <-errc
}
