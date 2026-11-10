package transport

import (
	"encoding/binary"
	"net"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	kcp "github.com/xtaci/kcp-go/v5"
)

func init() { register(cspoof{}) }

// cspoof (C-Spoof) sends its packets with a FORGED source IP address. Both servers must be
// "spoof-capable" — i.e. their datacenters must not egress-filter (BCP38) — because each side crafts
// raw IPv4/UDP packets whose source is a fake address, which can slip past filters that key on the
// real server IP. Incoming packets are delivered to a normal UDP socket (their forged source is
// ignored; replies go to the peer's configured real address). KCP rides on top for reliability and
// the secure core encrypts everything. Requires root (CAP_NET_RAW) and only runs on Linux.
type cspoof struct{}

func (cspoof) Name() string     { return "cspoof" }
func (cspoof) Category() string { return CatSpecial }
func (cspoof) Description() string {
	return "forged source address (both servers must be spoof-capable); needs root"
}

func spoofBlock() kcp.BlockCrypt { b, _ := kcp.NewNoneBlockCrypt(nil); return b }

func tuneSpoof(s *kcp.UDPSession) {
	s.SetStreamMode(true)
	s.SetNoDelay(1, 20, 2, 1)
	s.SetWindowSize(512, 512)
	s.SetMtu(1200)
	s.SetACKNoDelay(true)
}

func (cspoof) Listen(cfg *config.Config) (net.Listener, error) {
	pc, err := newSpoofConn(cfg)
	if err != nil {
		return nil, err
	}
	lis, err := kcp.ServeConn(spoofBlock(), 0, 0, pc)
	if err != nil {
		pc.Close()
		return nil, err
	}
	return &spoofListener{lis: lis}, nil
}

func (cspoof) Dial(cfg *config.Config) (net.Conn, error) {
	pc, err := newSpoofConn(cfg)
	if err != nil {
		return nil, err
	}
	sess, err := kcp.NewConn2(spoofPeerAddr, spoofBlock(), 0, 0, pc)
	if err != nil {
		pc.Close()
		return nil, err
	}
	tuneSpoof(sess)
	return sess, nil
}

type spoofListener struct{ lis *kcp.Listener }

func (l *spoofListener) Accept() (net.Conn, error) {
	s, err := l.lis.AcceptKCP()
	if err != nil {
		return nil, err
	}
	tuneSpoof(s)
	return s, nil
}
func (l *spoofListener) Close() error   { return l.lis.Close() }
func (l *spoofListener) Addr() net.Addr { return l.lis.Addr() }

// spoofPeerAddr is the single stable peer address presented to KCP (single-pair tunnel).
type spoofAddr struct{}

func (spoofAddr) Network() string { return "cspoof" }
func (spoofAddr) String() string  { return "cspoof://peer" }

var spoofPeerAddr net.Addr = spoofAddr{}

// buildIPv4UDP crafts a complete IPv4+UDP datagram with a forged source address. The UDP checksum is
// left 0 (optional for IPv4) and the IP checksum is computed; Linux accepts/overwrites as needed.
func buildIPv4UDP(src, dst [4]byte, sport, dport uint16, payload []byte) []byte {
	const ipLen = 20
	udpLen := 8 + len(payload)
	total := ipLen + udpLen
	pkt := make([]byte, total)

	// IPv4 header
	pkt[0] = 0x45 // version 4, IHL 5
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(total))
	binary.BigEndian.PutUint16(pkt[4:6], 0) // id (kernel may fill)
	binary.BigEndian.PutUint16(pkt[6:8], 0) // flags/frag
	pkt[8] = 64                             // TTL
	pkt[9] = 17                             // UDP
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum(pkt[:ipLen]))

	// UDP header
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	binary.BigEndian.PutUint16(pkt[22:24], dport)
	binary.BigEndian.PutUint16(pkt[24:26], uint16(udpLen))
	binary.BigEndian.PutUint16(pkt[26:28], 0) // UDP checksum 0 = none (IPv4)
	copy(pkt[28:], payload)
	return pkt
}

func ipChecksum(b []byte) uint16 {
	sum := 0
	for i := 0; i+1 < len(b); i += 2 {
		sum += int(b[i])<<8 | int(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += int(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
