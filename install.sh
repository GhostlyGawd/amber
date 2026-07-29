#!/bin/sh
# Amber installer. Downloads the latest release binary for your platform.
#
#   curl -fsSL https://raw.githubusercontent.com/ghostlygawd/amber/main/install.sh | sh
#
# Honors: AMBER_INSTALL_DIR (default /usr/local/bin, or ~/.local/bin if not
# writable), AMBER_VERSION (default: latest).
set -eu

REPO="ghostlygawd/amber"
BINARY="amber"

info() { printf '%s\n' "amber-install: $*" >&2; }
die()  { printf '%s\n' "amber-install: error: $*" >&2; exit 1; }

# --- platform detection ---
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS '$os' (tier-1: macOS, Linux). Try: go install github.com/$REPO/cmd/$BINARY@latest" ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported arch '$arch'" ;;
esac

# --- resolve version ---
version="${AMBER_VERSION:-}"
if [ -z "$version" ]; then
  info "resolving latest release…"
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/') \
    || die "could not resolve latest version; set AMBER_VERSION=vX.Y.Z"
fi
[ -n "$version" ] || die "empty version"
num=${version#v}

# --- choose install dir ---
dir="${AMBER_INSTALL_DIR:-/usr/local/bin}"
if [ ! -d "$dir" ] || [ ! -w "$dir" ]; then
  dir="$HOME/.local/bin"
  mkdir -p "$dir"
  info "using $dir (add it to PATH if it isn't already)"
fi

# --- download + verify ---
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
archive="amber_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

info "downloading $archive"
curl -fSL "$base/$archive" -o "$tmp/$archive" || die "download failed"

if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  info "verifying checksum"
  ( cd "$tmp" && grep " $archive\$" checksums.txt | (sha256sum -c - 2>/dev/null || shasum -a 256 -c -) ) \
    || die "checksum verification failed"
else
  die "checksums.txt not found; refusing unverified release"
fi

tar -xzf "$tmp/$archive" -C "$tmp"
install -m 0755 "$tmp/$BINARY" "$dir/$BINARY" 2>/dev/null || {
  chmod 0755 "$tmp/$BINARY"; mv "$tmp/$BINARY" "$dir/$BINARY";
}

info "installed $BINARY $version to $dir/$BINARY"
info "next: $BINARY init"
