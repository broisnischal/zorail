#!/usr/bin/env bash
#
# Zorail CLI installer — downloads the latest `zorail` binary for your platform
# from GitHub Releases and installs it onto your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/broisnischal/zorail/main/scripts/install-cli.sh | bash
#
# Env overrides:
#   ZORAIL_VERSION   pin a release tag (e.g. v0.3.0); default: latest
#   ZORAIL_BIN_DIR   install directory; default: /usr/local/bin (sudo if needed)
#                    or ~/.local/bin when /usr/local/bin isn't writable
#
# Windows: download the .zip from the Releases page instead (see the README).
set -euo pipefail

OWNER="broisnischal"
REPO="zorail"
BIN="zorail"

err() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m▸\033[0m %s\n' "$*"; }
ok() { printf '\033[32m✓\033[0m %s\n' "$*"; }

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar >/dev/null 2>&1 || err "tar is required"

# ---- detect platform ---------------------------------------------------------
case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) err "unsupported OS $(uname -s) — download from https://github.com/$OWNER/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) err "unsupported architecture $(uname -m)" ;;
esac

# ---- resolve version (follow the /releases/latest redirect) ------------------
ver="${ZORAIL_VERSION:-}"
if [ -z "$ver" ]; then
  ver="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$OWNER/$REPO/releases/latest" | sed -E 's#.*/tag/##')"
  [ -n "$ver" ] || err "could not determine the latest version; set ZORAIL_VERSION (e.g. v0.3.0)"
fi

asset="${BIN}_${ver}_${os}_${arch}.tar.gz"
url="https://github.com/$OWNER/$REPO/releases/download/$ver/$asset"

# ---- download + extract ------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
info "downloading $asset"
curl -fSL "$url" -o "$tmp/$asset" || err "download failed: $url"
tar -xzf "$tmp/$asset" -C "$tmp" || err "could not extract $asset"
[ -f "$tmp/$BIN" ] || err "archive did not contain $BIN"

# ---- install onto PATH -------------------------------------------------------
SUDO=""
dir="${ZORAIL_BIN_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir="/usr/local/bin"
  elif command -v sudo >/dev/null 2>&1; then
    dir="/usr/local/bin"; SUDO="sudo"
  else
    dir="$HOME/.local/bin"
  fi
fi
$SUDO mkdir -p "$dir"
$SUDO install -m 0755 "$tmp/$BIN" "$dir/$BIN"

ok "installed $BIN $ver to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) printf '\033[33m!\033[0m %s is not on your PATH — add it (e.g. export PATH="%s:$PATH")\n' "$dir" "$dir" ;;
esac
printf '\nGet started:\n  %s setup    # connect a domain\n  %s up       # run server + tunnel\n  %s help\n' "$BIN" "$BIN" "$BIN"
