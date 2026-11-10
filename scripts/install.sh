#!/usr/bin/env bash
# C Tunnel one-line installer (fast path: prebuilt static binary).
#   curl -fsSL https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/scripts/install.sh | sudo bash
# Downloads a ready-made binary (~10 MB) and opens the menu — no Go, no git, no compiling.
set -euo pipefail

BASE="https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main"
BIN="/usr/local/bin/ctunnel"

cyan=$'\033[36m'; green=$'\033[32m'; yellow=$'\033[33m'; red=$'\033[31m'; rst=$'\033[0m'
say(){ echo "${cyan}>${rst} $*"; }
ok(){ echo "${green}✓${rst} $*"; }
die(){ echo "${red}✗${rst} $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "Please run as root (sudo)."

# Architecture.
case "$(uname -m)" in
  x86_64)        arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "Unsupported architecture: $(uname -m)" ;;
esac

# Downloader (curl or wget).
fetch(){ # fetch <url> <out>
  if command -v curl >/dev/null 2>&1; then curl -fL --retry 3 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then wget -q -O "$2" "$1"
  else die "need curl or wget"; fi
}

say "downloading ctunnel (${arch}) ..."
tmp="$(mktemp)"
if fetch "$BASE/dist/ctunnel-linux-$arch" "$tmp" && [ -s "$tmp" ]; then
  install -m 0755 "$tmp" "$BIN"; rm -f "$tmp"
  ok "installed: $BIN ($arch)"
else
  rm -f "$tmp"
  die "download failed. See the manual install guide: $BASE/docs/MANUAL_INSTALL.md"
fi

# iproute2 is only needed for direct (TUN) mode — best effort, never blocks the install.
command -v ip >/dev/null 2>&1 || (apt-get install -y iproute2 >/dev/null 2>&1 || true)

echo
ok "Setup complete. Opening the menu..."
# When installed via `curl ... | bash`, stdin is the pipe, so read the menu from the terminal.
if [ -t 0 ]; then
  "$BIN" menu || true
elif [ -e /dev/tty ]; then
  "$BIN" menu < /dev/tty || true
else
  "$BIN" version || true
  ok "Run ${cyan}ctunnel${rst} in your shell to open the menu."
fi
