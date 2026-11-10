# Manual / offline install — راهنمای نصب دستی

For a server (usually the **Iran** side) that cannot reach GitHub well enough to build from source, use
a **prebuilt static binary**. No Go, no git, no build — just download one file and run one script.

برای سروری (معمولاً **ایران**) که دسترسی‌اش به GitHub برای ساختن از سورس خوب نیست، از **باینری آمادهٔ
استاتیک** استفاده کن. بدون Go، بدون git، بدون کامپایل — فقط یک فایل دانلود کن و یک اسکریپت اجرا کن.

---

## 1. Pick your architecture — معماری سرور را پیدا کن

```bash
uname -m
```

- `x86_64` → download **`ctunnel-linux-amd64`**
- `aarch64` / `arm64` → download **`ctunnel-linux-arm64`**

## 2. Download the binary — باینری را دانلود کن

From the repo's [`dist/`](../dist) folder (open the file on GitHub and click **Download raw**), or:

```bash
# amd64 (most servers)
curl -fLO https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/dist/ctunnel-linux-amd64
# or arm64
curl -fLO https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/dist/ctunnel-linux-arm64
```

If `curl` itself is blocked, download the file on any device and copy it to the server with `scp`:

```bash
scp ctunnel-linux-amd64 root@SERVER_IP:/root/
```

Optional — verify the file is intact (checksums are in [`dist/SHA256SUMS`](../dist/SHA256SUMS)):

```bash
sha256sum ctunnel-linux-amd64
```

## 3. Install — نصب کن

Download the installer too, then run it (it finds the binary next to it or takes its path):

```bash
curl -fLO https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/scripts/manual-install.sh
sudo bash manual-install.sh ./ctunnel-linux-amd64
```

Or, with nothing but the binary, do it by hand:

```bash
sudo install -m 0755 ctunnel-linux-amd64 /usr/local/bin/ctunnel
sudo ctunnel          # opens the menu
```

That's it. `ctunnel` opens the English install menu. Install the **Iran (Relay)** server first, copy the
**setup code** it prints, and run it on the **Foreign (Exit)** server.

همین. دستور `ctunnel` منوی نصب (انگلیسی) را باز می‌کند. اول سرور **ایران (Relay)** را نصب کن، **کد
ستاپی** که چاپ می‌شود را کپی کن و روی سرور **خارج (Exit)** اجرا کن.

## 4. Updating — به‌روزرسانی

Download the newer binary and re-run step 3, then restart the service:

```bash
sudo systemctl restart ctunnel
```

## Notes — نکته‌ها

- Direct (TUN) mode needs `iproute2` (the `ip` command) and root. Install it with
  `apt-get install -y iproute2` if it is missing.
- The `cspoof` transport needs root / `CAP_NET_RAW`; the systemd service already requests it.
- The binaries are static (`CGO_ENABLED=0`), so they run on any modern Linux with no dependencies.
