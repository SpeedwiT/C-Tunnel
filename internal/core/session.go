package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

// Session is a multiplexed tunnel over one secure connection. The relay opens streams toward the
// exit, each carrying one forwarded user connection; the exit dials the requested target per stream.
type Session struct {
	mux *yamux.Session
}

func yamuxConfig() *yamux.Config {
	c := yamux.DefaultConfig()
	c.KeepAliveInterval = 10 * time.Second
	c.ConnectionWriteTimeout = 15 * time.Second
	c.MaxStreamWindowSize = 1 << 20
	c.LogOutput = io.Discard
	return c
}

// NewSession wraps a secure conn. The relay is the yamux server, the exit is the client.
func NewSession(conn net.Conn, relay bool) (*Session, error) {
	var mux *yamux.Session
	var err error
	if relay {
		mux, err = yamux.Server(conn, yamuxConfig())
	} else {
		mux, err = yamux.Client(conn, yamuxConfig())
	}
	if err != nil {
		return nil, err
	}
	return &Session{mux: mux}, nil
}

func (s *Session) Close() error   { return s.mux.Close() }
func (s *Session) IsClosed() bool { return s.mux.IsClosed() }

// Ping measures the round-trip time over the live session.
func (s *Session) Ping() (time.Duration, error) { return s.mux.Ping() }

// Open starts a stream to the exit for the given target (host:port) and pipes `local` through it.
func (s *Session) Open(target string, local net.Conn) error {
	stream, err := s.mux.Open()
	if err != nil {
		return err
	}
	if err := writeString(stream, target); err != nil {
		stream.Close()
		return err
	}
	pipe(stream, local)
	return nil
}

// Serve runs on the exit: accept streams, read the target, dial it and pipe.
func (s *Session) Serve(dial func(network, addr string) (net.Conn, error)) error {
	for {
		stream, err := s.mux.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer stream.Close()
			target, err := readString(stream)
			if err != nil {
				return
			}
			remote, err := dial("tcp", target)
			if err != nil {
				return
			}
			pipe(stream, remote)
		}()
	}
}

func writeString(w io.Writer, s string) error {
	if len(s) > 65535 {
		return fmt.Errorf("target too long")
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(s)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func readString(r io.Reader) (string, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", err
	}
	buf := make([]byte, binary.BigEndian.Uint16(hdr[:]))
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// pipe copies bytes both ways until either side closes, then tears both down.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
