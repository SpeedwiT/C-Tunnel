package broker

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// DialPair connects to the broker and registers under id with the given role ("L" for the listener/
// relay side, "D" for the dialer/exit side). It blocks until a partner with the same id arrives (the
// broker then sends "PAIR\n"); the returned conn carries the tunnel, piped transparently, so the
// secure handshake runs end-to-end.
func DialPair(brokerAddr, role, id string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", brokerAddr, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c, "%s %s %s\n", magic, role, id); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n') // blocks until paired (or the broker times out and closes)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("broker pairing failed: %w", err)
	}
	if strings.TrimSpace(line) != "PAIR" {
		c.Close()
		return nil, fmt.Errorf("broker: %s", strings.TrimSpace(line))
	}
	return &readerConn{Conn: c, r: br}, nil
}

// readerConn lets any bytes buffered past the PAIR line travel with the conn.
type readerConn struct {
	net.Conn
	r *bufio.Reader
}

func (rc *readerConn) Read(p []byte) (int, error) { return rc.r.Read(p) }

// PutBlob stores data under a pairing code (used by the Iran side when minting a short setup code).
func PutBlob(brokerAddr, code string, data []byte) error {
	c, err := net.DialTimeout("tcp", brokerAddr, 15*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := fmt.Fprintf(c, "%s PUT %s %d\n", magic, code, len(data)); err != nil {
		return err
	}
	if _, err := c.Write(data); err != nil {
		return err
	}
	resp, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return err
	}
	if resp != "OK\n" {
		return fmt.Errorf("broker rejected the blob")
	}
	return nil
}

// GetBlob fetches a blob by pairing code (used by the foreign side when joining with a short code).
func GetBlob(brokerAddr, code string) ([]byte, error) {
	c, err := net.DialTimeout("tcp", brokerAddr, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := fmt.Fprintf(c, "%s GET %s\n", magic, code); err != nil {
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var n int
	if _, err := fmt.Sscanf(resp, "OK %d\n", &n); err != nil || n <= 0 {
		return nil, fmt.Errorf("setup code not found or expired")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(br, data); err != nil {
		return nil, err
	}
	return data, nil
}
