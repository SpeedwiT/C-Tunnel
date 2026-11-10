#!/usr/bin/env bash
# C Tunnel one-line installer.
#   curl -fsSL https://raw.githubusercontent.com/SpeedwiT/C-Tunnel/main/scripts/install.sh | bash
# Builds a single static binary, installs it to /usr/local/bin/ctunnel, then opens the menu.
set -euo pipefail

REPO="https://github.com/SpeedwiT/C-Tunnel.git"
BRANCH="${CTUNNEL_BRANCH:-main}"
BIN="/usr/local/bin/ctunnel"
GO_VER="1.26.2"

cyan=$'\033[36m'; green=$'\033[32m'; yellow=$'\033[33m'; red=$'\033[31m'; rst=$'\033[0m'
say()  { echo "${cyan}»${rst} $*"; }
ok()   { echo "${green}✓${rst} $*"; }
die()  { echo "${red}✗${rst} $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "لطفاً با root اجرا کن (sudo)."

# --- toolchain -------------------------------------------------------------
ensure_pkg() { command -v "$1" >/dev/null 2>&1 || { say "نصب $1"; apt-get update -y && apt-get install -y "$1"; }; }
ensure_pkg git
ensure_pkg curl
command -v ip >/dev/null 2>&1 || { say "نصب iproute2 (برای حالت مستقیم/TUN)"; apt-get install -y iproute2 || true; }

if ! command -v go >/dev/null 2>&1; then
  arch=$(uname -m); case "$arch" in x86_64) ga=amd64;; aarch64|arm64) ga=arm64;; *) die "معماری پشتیبانی‌نشده: $arch";; esac
  say "نصب Go ${GO_VER}"
  curl -fsSL "https://go.dev/dl/go${GO_VER}.linux-${ga}.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
  export PATH="/usr/local/go/bin:$PATH"
fi
export PATH="/usr/local/go/bin:$PATH"
ok "Go: $(go version | awk '{print $3}')"

# --- build -----------------------------------------------------------------
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
say "fetching source (${BRANCH})"
git clone --depth 1 -b "$BRANCH" "$REPO" "$work/src" >/dev/null 2>&1 || die "git clone failed"
cd "$work/src"
VER="1.0.0"
say "compiling"
CGO_ENABLED=0 GOFLAGS=-mod=mod go build -trimpath \
  -ldflags "-s -w -X main.version=${VER}" -o "$work/ctunnel" ./cmd/ctunnel
install -m 0755 "$work/ctunnel" "$BIN"
ok "installed: $BIN (v${VER})"

echo
ok "Setup complete. Opening the menu…"
# Open the interactive menu now. When installed via `curl ... | bash`, stdin is the pipe,
# so read the menu from the controlling terminal instead.
if [ -t 0 ]; then
  "$BIN" menu || true
elif [ -e /dev/tty ]; then
  "$BIN" menu < /dev/tty || true
else
  "$BIN" -h 2>/dev/null | head -10 || true
  ok "Run ${cyan}ctunnel${rst} in your shell to open the menu."
fi
