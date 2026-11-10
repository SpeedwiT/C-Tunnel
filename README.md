# C Tunnel

**A fast, anti-DPI, anti-filter multi-transport tunnel between an Iran relay and a foreign exit.**

C Tunnel carries traffic between a server in Iran (the *relay* — the clean, fast, domestic entry
users connect to) and a server abroad (the *exit*), over a pluggable, DPI-resistant transport. It is
built for Iran's conditions: heavy throttling, packet loss, SNI/IP blocking and full blackouts, with
automatic fail-over between transports.

- **GitHub:** @SpeedwiT
- Written in Go — a single static binary, instant install, no runtime dependencies.

## How it works

```
 users ─▶ Iran relay  ══[ C Tunnel transport ]══  foreign exit ─▶ real services
          (public IP,                                (dials the relay,
           forwards ports)                            reaches targets)
```

- The **relay** (Iran) is installed first, listens on the forwarded ports and waits for the exit.
- The **exit** (abroad) dials the relay over the chosen transport and reaches the real services.
- Every transport runs under the same **secure core**: an authenticated X25519 handshake keyed by a
  pre-shared key, then ChaCha20-Poly1305 for confidentiality and integrity, multiplexed so one
  carrier connection serves every forwarded port. The transport's only job is to look innocuous to
  DPI; it can never see or forge the traffic.

## Transports (roadmap)

Ten transports in three selectable categories. ✅ = implemented & tested, ⏳ = planned.
Pick per node; `ctunnel transports` lists them grouped.

**TCP — resilient where UDP is throttled/blocked on an Iran datacenter**
- ✅ **C-TLS** — the tunnel inside an ordinary TLS 1.3 session (looks like HTTPS).
- ✅ **C-WS** — WebSocket upgrade over TLS (looks like a normal WebSocket).
- ✅ **C-HTTP** — plain HTTP/1.1 mimic (no TLS); core still encrypts.
- ✅ **C-Spoof** — ClientHello/SNI fragmentation to defeat SNI-based DPI (no CDN).
- ⏳ **C-H2Mux** — HTTP/2-shaped, API-like multiplexed traffic.

**UDP — fastest on good links**
- ✅ **C-QUIC** — HTTP/3-shaped QUIC; fastest, migrates across IP/port.
- ✅ **C-KCP** — reliable UDP with FEC, tuned for lossy links; smooths drops.
- ✅ **C-Storm** — reliable UDP with aggressive FEC for severe loss/disruption.

**Special — blackout / last resort**
- ⏳ **C-ICMP** — tunnels over ping when only ICMP passes.
- ⏳ **C-DNS** — tunnels over DNS/DoH, the last resort.

7 of 10 are implemented and pass the end-to-end test; the three remaining are next.

(No CDN transport — not needed.) Plus: automatic fail-over between transports, a built-in transport **test/ping** screen, live
network/server status, a stylish terminal install menu, one-line install with a *setup code* so the
foreign server joins in one step, and systemd services.

## Build

```bash
go build -o ctunnel ./cmd/ctunnel
```

## Quick manual run (two machines, same PSK)

```bash
ctunnel genpsk          # make a shared key, put it in both configs
ctunnel run -config /etc/ctunnel/config.json
```

Relay config (Iran):

```json
{
  "role": "relay", "transport": "ctls", "listen": ":7000",
  "psk": "<shared key>",
  "forwards": [{ "name": "vless", "listen": 443, "target": "127.0.0.1:443" }]
}
```

Exit config (abroad):

```json
{ "role": "exit", "transport": "ctls", "peer": "RELAY_IP:7000", "psk": "<shared key>" }
```

A user connecting to `RELAY_IP:443` is tunnelled to `127.0.0.1:443` on the exit.

> The one-line installer and interactive menu (Iran/Abroad, setup code, transport test, status) are
> being added next.
