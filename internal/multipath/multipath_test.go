package multipath

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

func block() kcp.BlockCrypt { b, _ := kcp.NewNoneBlockCrypt(nil); return b }

// TestSeamlessFailoverBulk streams a large payload over KCP running on two member links and kills one
// of them mid-transfer. All bytes must still arrive intact — proving packet-level failover without a
// session drop.
func TestSeamlessFailoverBulk(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()

	client := Server()
	client.Add(c1)
	client.Add(c2)
	server := Server()
	server.Add(s1)
	server.Add(s2)
	defer client.Close()
	defer server.Close()

	lis, err := kcp.ServeConn(block(), 0, 0, server)
	if err != nil {
		t.Fatal(err)
	}

	const total = 300 * 1024
	payload := make([]byte, total)
	_, _ = rand.Read(payload)

	// Receiver: accept the KCP session and read exactly total bytes.
	got := make(chan []byte, 1)
	errc := make(chan error, 1)
	go func() {
		sess, err := lis.AcceptKCP()
		if err != nil {
			errc <- err
			return
		}
		sess.SetReadDeadline(time.Now().Add(20 * time.Second))
		buf := make([]byte, total)
		if _, err := io.ReadFull(sess, buf); err != nil {
			errc <- err
			return
		}
		got <- buf
	}()

	// Sender.
	sess, err := kcp.NewConn2(peerAddr, block(), 0, 0, client)
	if err != nil {
		t.Fatal(err)
	}
	sess.SetWriteDeadline(time.Now().Add(20 * time.Second))

	go func() {
		const chunk = 4096
		for off := 0; off < total; off += chunk {
			end := off + chunk
			if end > total {
				end = total
			}
			if _, err := sess.Write(payload[off:end]); err != nil {
				return
			}
			if off == 40*chunk {
				// Kill one member link mid-stream; KCP must recover over the other.
				c1.Close()
				s1.Close()
			}
			time.Sleep(time.Millisecond)
		}
	}()

	select {
	case b := <-got:
		if !bytes.Equal(b, payload) {
			t.Fatal("payload corrupted across failover")
		}
	case err := <-errc:
		t.Fatalf("receiver error: %v", err)
	case <-time.After(25 * time.Second):
		t.Fatal("transfer did not complete after a member failed")
	}
}

// TestBothMembersCarry sanity-checks that datagrams really stripe across members (round-robin).
func TestBothMembersCarry(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	mp := Server()
	mp.Add(c1)
	mp.Add(c2)
	defer mp.Close()

	// Drain the server ends, counting frames seen on each.
	seen := make(chan int, 64)
	read := func(conn net.Conn, id int) {
		hdr := make([]byte, 2)
		for {
			if _, err := io.ReadFull(conn, hdr); err != nil {
				return
			}
			n := int(hdr[0])<<8 | int(hdr[1])
			if _, err := io.ReadFull(conn, make([]byte, n)); err != nil {
				return
			}
			seen <- id
		}
	}
	go read(s1, 1)
	go read(s2, 2)

	for i := 0; i < 8; i++ {
		mp.WriteTo([]byte("ping"), peerAddr)
	}
	counts := map[int]int{}
	deadline := time.After(3 * time.Second)
	for total := 0; total < 8; {
		select {
		case id := <-seen:
			counts[id]++
			total++
		case <-deadline:
			t.Fatalf("only saw %d frames: %v", total, counts)
		}
	}
	if counts[1] == 0 || counts[2] == 0 {
		t.Fatalf("expected both members to carry traffic, got %v", counts)
	}
}
