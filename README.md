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

## Two tunnel modes

Chosen in the install menu; both carry the forwarded ports and run under the same secure core.

- **Reverse (معکوس)** — the foreign server dials in to the Iran server. Iran needs **one open port**;
  the foreign server needs none. This is the default and what the examples below show.
- **Direct (مستقیم)** — the Iran server dials **out** to the foreign server, so Iran needs **no inbound
  port at all**. It brings up a full **L3 IP tunnel** (a TUN interface carrying whole IP packets, GRE
  encapsulation inside the Noise-authenticated core) so both hosts share a private subnet and anything
  — not just the forwarded ports — can be routed. Mutual auth, a replay window and automatic MTU
  discovery. *(The IP-tunnel data path lands next; mode selection, config and setup-code flow are in.)*

## Transports (roadmap)

Ten transports in three selectable categories. ✅ = implemented & tested, ⏳ = planned.
Pick per node; `ctunnel transports` lists them grouped.

**TCP — resilient where UDP is throttled/blocked on an Iran datacenter**
- ✅ **C-TLS** — the tunnel inside an ordinary TLS 1.3 session (looks like HTTPS).
- ✅ **C-WS** — WebSocket upgrade over TLS (looks like a normal WebSocket).
- ✅ **C-HTTP** — plain HTTP/1.1 mimic (no TLS); core still encrypts.
- ✅ **C-Frag** — ClientHello/SNI fragmentation to defeat SNI-based DPI.
- ⏳ **C-Spoof** — **forged source-address** transport: both servers must be *spoof-capable*
  (اسپوف‌خور) and their datacenters must permit egress spoofing. Distinct from C-Frag and unrelated
  to any CDN.

**UDP — fastest on good links**
- ✅ **C-QUIC** — HTTP/3-shaped QUIC; fastest, migrates across IP/port.
- ✅ **C-KCP** — reliable UDP with FEC, tuned for lossy links; smooths drops.
- ✅ **C-Storm** — reliable UDP with aggressive FEC for severe loss/disruption.

**Special — blackout / last resort**
- ⏳ **C-ICMP** — tunnels over ping when only ICMP passes.
- ⏳ **C-DNS** — tunnels over DNS/DoH, the last resort.

7 of 10 are implemented and pass the end-to-end test; the remaining three (C-Spoof, C-ICMP, C-DNS)
are next. No CDN transport — Cloudflare/Arvan are CDNs and aren't needed here.

## The novel bit — seamless multi-transport

Existing tunnels pin one transport and *reconnect* (dropping every session) when it's throttled or
blocked. C Tunnel's carrier layer is being built to run **several transports at once** (e.g. QUIC +
KCP + TLS) and **migrate the live session across them at the packet level** — if one is choked mid-
stream, traffic shifts to the others *without tearing the tunnel down*, and bandwidth can be bonded
across them. `Manage → Link Test` already measures every transport's reachability and ping so the
best set is chosen automatically. This live cross-transport migration is the feature no existing
tunnel ships.

## Install (one line, on each server — Iran first)

```bash
curl -fsSL https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/scripts/install.sh | sudo bash
```

It builds the binary, installs `ctunnel`, and opens the menu:

1. **نصب سرور ایران (Relay)** — pick mode (reverse/direct), transport category + transport, ports;
   it generates the PSK, writes the config, installs the systemd service, and prints a **setup code**.
2. **نصب سرور خارج (Exit)** — paste the setup code; the foreign server is configured and started in
   one step (`ctunnel join <code>`).
3. **مدیریت** — live status + ping, **Link Test** (rank every transport by ping), change transport
   without reinstalling (`ctunnel switch <name>`), restart.

## CLI

```bash
ctunnel                      # interactive menu
ctunnel setup-code           # (Iran) print the foreign-server join code
ctunnel join <code>          # (abroad) configure from a setup code
ctunnel status               # role, mode, forwards, live ping
ctunnel linktest-server      # (Iran) serve Link Test probes
ctunnel linktest -host <ip>  # (abroad) test every transport, ranked by ping
ctunnel switch <transport>   # change transport without reinstalling
ctunnel transports           # list transports grouped by category
```

## Quick manual run (two machines, same PSK)

```bash
ctunnel genpsk          # make a shared key, put it in both configs
ctunnel run -config /etc/ctunnel/config.json
```

Relay config (Iran):

```json
{
  "role": "relay", "mode": "reverse", "transport": "ctls", "listen": ":7000",
  "psk": "<shared key>",
  "forwards": [{ "name": "vless", "listen": 443, "target": "127.0.0.1:443" }]
}
```

Exit config (abroad):

```json
{ "role": "exit", "mode": "reverse", "transport": "ctls", "peer": "RELAY_IP:7000", "psk": "<shared key>" }
```

A user connecting to `RELAY_IP:443` is tunnelled to `127.0.0.1:443` on the exit.
