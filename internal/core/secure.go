// Package core is the C Tunnel secure channel, framing and multiplexing that run on top of any
// transport. The transport only has to deliver bytes (and hide them from DPI); confidentiality,
// integrity and peer authentication are guaranteed here, uniformly, for every transport.
package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	maxFrame       = 16 * 1024
	handshakeMagic = "CTUN1"
)

// Secure performs an authenticated X25519 handshake keyed by the pre-shared key and returns an
// encrypted, authenticated net.Conn. `initiator` is the side that dials (the exit).
func Secure(conn net.Conn, psk []byte, initiator bool) (net.Conn, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return nil, err
	}
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, err
	}

	label := func(who string) []byte { return append([]byte(handshakeMagic+who), pub...) }
	write := func(who string) error {
		mac := mac(psk, label(who))
		if _, err := conn.Write(append(pub, mac...)); err != nil {
			return err
		}
		return nil
	}
	read := func(who string) ([]byte, error) {
		buf := make([]byte, 32+32)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		peerPub, peerMac := buf[:32], buf[32:]
		want := mac(psk, append([]byte(handshakeMagic+who), peerPub...))
		if !hmac.Equal(peerMac, want) {
			return nil, errors.New("ctunnel: peer authentication failed (wrong PSK?)")
		}
		return peerPub, nil
	}

	var peerPub []byte
	if initiator {
		if err := write("I"); err != nil {
			return nil, err
		}
		if peerPub, err = read("R"); err != nil {
			return nil, err
		}
	} else {
		if peerPub, err = read("I"); err != nil {
			return nil, err
		}
		if err := write("R"); err != nil {
			return nil, err
		}
	}

	shared, err := curve25519.X25519(priv[:], peerPub)
	if err != nil {
		return nil, err
	}
	// two directional keys so initiator and responder never share a key/nonce space
	kInit := deriveKey(shared, psk, "c2s")
	kResp := deriveKey(shared, psk, "s2c")
	var sendKey, recvKey []byte
	if initiator {
		sendKey, recvKey = kInit, kResp
	} else {
		sendKey, recvKey = kResp, kInit
	}

	sAEAD, err := chacha20poly1305.New(sendKey)
	if err != nil {
		return nil, err
	}
	rAEAD, err := chacha20poly1305.New(recvKey)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &secureConn{Conn: conn, send: sAEAD, recv: rAEAD}, nil
}

func mac(psk, msg []byte) []byte {
	h := hmac.New(sha256.New, psk)
	h.Write(msg)
	return h.Sum(nil)
}

func deriveKey(shared, psk []byte, info string) []byte {
	r := hkdf.New(sha256.New, shared, psk, []byte("ctunnel-v1-"+info))
	key := make([]byte, chacha20poly1305.KeySize)
	_, _ = io.ReadFull(r, key)
	return key
}

// secureConn frames and AEAD-encrypts a stream. Each frame: [2-byte len][ciphertext]; the 12-byte
// nonce is a per-direction counter.
type secureConn struct {
	net.Conn
	send, recv interface {
		Seal(dst, nonce, plaintext, ad []byte) []byte
		Open(dst, nonce, ciphertext, ad []byte) ([]byte, error)
		NonceSize() int
	}

	wmu    sync.Mutex
	wNonce [12]byte

	rmu    sync.Mutex
	rNonce [12]byte
	rbuf   []byte
}

func (s *secureConn) Write(p []byte) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > maxFrame {
			n = maxFrame
		}
		ct := s.send.Seal(nil, s.wNonce[:], p[:n], nil)
		incNonce(&s.wNonce)
		var hdr [2]byte
		binary.BigEndian.PutUint16(hdr[:], uint16(len(ct)))
		if _, err := s.Conn.Write(hdr[:]); err != nil {
			return total, err
		}
		if _, err := s.Conn.Write(ct); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

func (s *secureConn) Read(p []byte) (int, error) {
	s.rmu.Lock()
	defer s.rmu.Unlock()
	if len(s.rbuf) == 0 {
		var hdr [2]byte
		if _, err := io.ReadFull(s.Conn, hdr[:]); err != nil {
			return 0, err
		}
		n := binary.BigEndian.Uint16(hdr[:])
		if int(n) > maxFrame+chacha20poly1305.Overhead {
			return 0, fmt.Errorf("ctunnel: frame too large (%d)", n)
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(s.Conn, ct); err != nil {
			return 0, err
		}
		pt, err := s.recv.Open(nil, s.rNonce[:], ct, nil)
		if err != nil {
			return 0, errors.New("ctunnel: decryption failed")
		}
		incNonce(&s.rNonce)
		s.rbuf = pt
	}
	n := copy(p, s.rbuf)
	s.rbuf = s.rbuf[n:]
	return n, nil
}

func incNonce(n *[12]byte) {
	for i := len(n) - 1; i >= 0; i-- {
		n[i]++
		if n[i] != 0 {
			return
		}
	}
}
