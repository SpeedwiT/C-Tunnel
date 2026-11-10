package transport

import (
	"crypto/sha256"
	"encoding/base32"
	"net"
	"strings"

	"github.com/SpeedwiT/c-tunnel/internal/broker"
	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func init() { register(crdv{}) }

// crdv (C-RDV) is the rendezvous transport: both servers dial OUT to a broker that pairs them by a
// shared id and pipes the (already-encrypted) tunnel between them. Because neither side needs an open
// inbound port, the tunnel forms even when the Iran IP is reachable only domestically or the foreign
// IP is filtered — whichever side is blocked, both still reach the broker. The id is derived from the
// PSK, so the two servers compute the same rendezvous without extra configuration.
type crdv struct{}

func (crdv) Name() string     { return "crdv" }
func (crdv) Category() string { return CatSpecial }
func (crdv) Description() string {
	return "rendezvous: both servers dial a broker (for when one IP is blocked)"
}

// RendezvousID turns a PSK into a stable, shareable rendezvous id.
func RendezvousID(psk []byte) string {
	sum := sha256.Sum256(append([]byte("c-tunnel-rdv"), psk...))
	id := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:10])
	return strings.ToLower(id)
}

func rendezvousFor(cfg *config.Config) (string, error) {
	psk, err := config.DecodePSK(cfg.PSK)
	if err != nil {
		return "", err
	}
	return RendezvousID(psk), nil
}

func (crdv) Listen(cfg *config.Config) (net.Listener, error) {
	id, err := rendezvousFor(cfg)
	if err != nil {
		return nil, err
	}
	return &rdvListener{broker: cfg.Broker, id: id}, nil
}

func (crdv) Dial(cfg *config.Config) (net.Conn, error) {
	id, err := rendezvousFor(cfg)
	if err != nil {
		return nil, err
	}
	return broker.DialPair(cfg.Broker, "D", id)
}

// rdvListener yields one carrier per completed pairing. Accept blocks until a dialer arrives, so the
// relay keeps exactly one registration outstanding at the broker at a time.
type rdvListener struct {
	broker string
	id     string
	addr   rdvAddr
}

func (l *rdvListener) Accept() (net.Conn, error) { return broker.DialPair(l.broker, "L", l.id) }
func (l *rdvListener) Close() error              { return nil }
func (l *rdvListener) Addr() net.Addr            { return l.addr }

type rdvAddr struct{}

func (rdvAddr) Network() string { return "crdv" }
func (rdvAddr) String() string  { return "crdv://broker" }
