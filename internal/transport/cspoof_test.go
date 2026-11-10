package transport

import (
	"encoding/binary"
	"testing"
)

func TestBuildIPv4UDP(t *testing.T) {
	src := [4]byte{10, 9, 8, 7}
	dst := [4]byte{1, 2, 3, 4}
	payload := []byte("c-tunnel")
	pkt := buildIPv4UDP(src, dst, 4444, 7000, payload)

	if len(pkt) != 20+8+len(payload) {
		t.Fatalf("length = %d", len(pkt))
	}
	if pkt[0] != 0x45 || pkt[9] != 17 {
		t.Fatalf("bad IP header: ver/ihl=%#x proto=%d", pkt[0], pkt[9])
	}
	// Re-summing a correct IPv4 header (checksum field included) yields 0.
	if s := ipChecksum(pkt[:20]); s != 0 {
		t.Fatalf("IP checksum does not verify: %#x", s)
	}
	if binary.BigEndian.Uint16(pkt[2:4]) != uint16(len(pkt)) {
		t.Fatal("total length wrong")
	}
	if string(pkt[12:16]) != string(src[:]) || string(pkt[16:20]) != string(dst[:]) {
		t.Fatal("src/dst not placed correctly")
	}
	if binary.BigEndian.Uint16(pkt[20:22]) != 4444 || binary.BigEndian.Uint16(pkt[22:24]) != 7000 {
		t.Fatal("udp ports wrong")
	}
	if int(binary.BigEndian.Uint16(pkt[24:26])) != 8+len(payload) {
		t.Fatal("udp length wrong")
	}
	if string(pkt[28:]) != string(payload) {
		t.Fatal("payload wrong")
	}
}
