#!/bin/sh

set -e

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

TMPDIR="$(mktemp -d)"
BIN="$TMPDIR/gradient-engineer"

curl -fsSL -o "$BIN" "https://gradient.engineer/binary/gradient-engineer.${OS}.${ARCH}"
chmod +x "$BIN"
"$BIN" 60-second-linux

rm -f "$BIN"
rmdir "$TMPDIR"
