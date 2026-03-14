#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_NAME="${BIN_NAME:-tlsctl}"
PREFIX="${PREFIX:-$HOME/.local}"
DEST_DIR="${DEST_DIR:-$PREFIX/bin}"
DEST="$DEST_DIR/$BIN_NAME"

if ! command -v go >/dev/null 2>&1; then
  echo "go not found. use one of:" >&2
  echo "  bash scripts/install-binary.sh" >&2
  echo "  docker build -t tlsctl:local . && docker run --rm tlsctl:local --help" >&2
  exit 2
fi

mkdir -p "$DEST_DIR"

cd "$ROOT"
go build -o "$DEST" ./cmd/tlsctl

echo "installed: $DEST"
echo "version:"
"$DEST" --version || true
