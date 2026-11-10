package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func init() { register(ctls{}) }

// ctls (C-TLS) carries the tunnel inside an ordinary TLS 1.3 session on a TCP port, so to a DPI it
// looks like plain HTTPS. Trust is not from the certificate (the core PSK handshake authenticates
// the peer) — the TLS layer is camouflage, so the exit skips verification.
type ctls struct{}

func (ctls) Name() string { return "ctls" }

func (ctls) Listen(cfg *config.Config) (net.Listener, error) {
	cert, err := selfSigned(sniOrDefault(cfg.SNI))
	if err != nil {
		return nil, err
	}
	tc := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}
	return tls.Listen("tcp", cfg.Listen, tc)
}

func (ctls) Dial(cfg *config.Config) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 15 * time.Second}
	tc := &tls.Config{
		ServerName:         sniOrDefault(cfg.SNI),
		InsecureSkipVerify: true, // peer identity is proven by the core PSK handshake
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2", "http/1.1"},
	}
	return tls.DialWithDialer(d, "tcp", cfg.Peer, tc)
}

func sniOrDefault(sni string) string {
	if sni == "" {
		return "www.cloudflare.com"
	}
	return sni
}

// selfSigned makes an ephemeral certificate for the given name (camouflage only).
func selfSigned(host string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(825 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
