#!/bin/sh

set -e

TMPDIR="$(mktemp -d)"
BIN="$TMPDIR/gradient-engineer-go"

curl -fsSL -o "$BIN" https://gradient.engineer/gradient-engineer-go
chmod +x "$BIN"
"$BIN"

rm -f "$BIN"
rmdir "$TMPDIR"
