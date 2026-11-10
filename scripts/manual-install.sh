#!/usr/bin/env bash
# C Tunnel MANUAL installer — for servers that cannot reach GitHub well enough to build from source.
# It installs a prebuilt static binary. Two ways to use it:
#
#   1) You already downloaded the binary next to this script (ctunnel-linux-amd64 / -arm64):
#        sudo bash manual-install.sh
#
#   2) Point it at a file you downloaded anywhere:
#        sudo bash manual-install.sh /path/to/ctunnel-linux-amd64
#
# It copies the binary to /usr/local/bin/ctunnel, makes it executable, installs iproute2 if possible,
# and opens the menu. No Go, no git, no internet needed once you have the binary file.
set -euo pipefail

BIN="/usr/local/bin/ctunnel"
cyan=$'\033[36m'; green=$'\033[32m'; yellow=$'\033[33m'; red=$'\033[31m'; rst=$'\033[0m'
ok(){ echo "${green}OK${rst} $*"; }
die(){ echo "${red}x${rst} $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "Please run as root (sudo)."

# Pick the architecture.
case "$(uname -m)" in
  x86_64)        arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "Unsupported architecture: $(uname -m)" ;;
esac

# Find the binary: an explicit argument, or a file next to this script / in the current directory.
src="${1:-}"
if [ -z "$src" ]; then
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  for cand in "$here/ctunnel-linux-$arch" "./ctunnel-linux-$arch" "$here/../dist/ctunnel-linux-$arch"; do
    [ -f "$cand" ] && { src="$cand"; break; }
  done
fi
[ -n "$src" ] && [ -f "$src" ] || die "Binary not found. Download ctunnel-linux-$arch and pass its path:
  sudo bash manual-install.sh /path/to/ctunnel-linux-$arch"

echo "${cyan}>>${rst} installing $src -> $BIN"
install -m 0755 "$src" "$BIN"
ok "installed: $BIN ($arch)"

# Best-effort: iproute2 is only needed for direct (TUN) mode.
if ! command -v ip >/dev/null 2>&1; then
  (apt-get update -y && apt-get install -y iproute2) >/dev/null 2>&1 || \
    echo "${yellow}note:${rst} 'ip' (iproute2) not installed — needed only for direct/TUN mode."
fi

echo
if [ -t 0 ]; then
  "$BIN" menu || true
else
  "$BIN" version || true
fi
echo
ok "Run ${cyan}ctunnel${rst} any time to open the menu."
