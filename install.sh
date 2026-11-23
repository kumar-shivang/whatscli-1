#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Install the latest whatscli-extended release (Linux and macOS, amd64/arm64):
#   curl -fsSL https://raw.githubusercontent.com/kumar-shivang/whatscli-extended/master/install.sh | bash
#
# Environment:
#   WHATSCLI_VERSION  install this tag (for example v1.7.3) instead of the latest
#   INSTALL_DIR       target directory (default: /usr/local/bin if writable, else ~/.local/bin)
set -euo pipefail

REPO="kumar-shivang/whatscli-extended"

err() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "$1 is required"; }

need curl
need tar

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS $(uname -s); Windows users: download the zip from https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture $(uname -m)" ;;
esac

tag="${WHATSCLI_VERSION:-}"
if [ -z "$tag" ]; then
  # The /releases/latest redirect avoids the GitHub API rate limit.
  url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")"
  tag="${url##*/}"
fi
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]] || err "could not determine the latest release (got '$tag')"

asset="whatscli-${tag}-${os}-${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

if [ -n "${INSTALL_DIR:-}" ]; then
  dir="$INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Installing whatscli $tag ($os/$arch) to $dir"
curl -fsSL -o "$tmp/$asset" "$base/$asset" || err "download failed: $base/$asset (no build for $os/$arch in $tag?)"
curl -fsSL -o "$tmp/checksums.txt" "$base/whatscli-${tag}-checksums.txt" || err "could not download checksums"

want="$(awk -v a="$asset" '$2==a || $2=="*"a {print $1}' "$tmp/checksums.txt")"
[ -n "$want" ] || err "no checksum listed for $asset"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
else
  err "sha256sum or shasum is required"
fi
[ "$got" = "$want" ] || err "checksum mismatch (got $got, want $want)"

tar -xzf "$tmp/$asset" -C "$tmp" whatscli
install -m 0755 "$tmp/whatscli" "$dir/whatscli.new"
mv -f "$dir/whatscli.new" "$dir/whatscli"

echo "Installed $dir/whatscli"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Note: $dir is not on your PATH. Add it, for example: export PATH=\"$dir:\$PATH\"" ;;
esac
echo "Run: whatscli    (update later from inside the app with /update)"
