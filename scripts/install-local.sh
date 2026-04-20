#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EDITION="${VOLCLOG_EDITION:-default}"
BIN_NAME="${BIN_NAME:-}"
PREFIX="${PREFIX:-$HOME/.local}"
DEST_DIR="${DEST_DIR:-$PREFIX/bin}"

build_tags=()
case "$EDITION" in
  default|"")
    SRC_BIN_NAME="volclog"
    ;;
  human)
    SRC_BIN_NAME="volclog-human"
    build_tags=(-tags=human)
    ;;
  *)
    echo "invalid VOLCLOG_EDITION: $EDITION" >&2
    exit 2
    ;;
esac

BIN_NAME="${BIN_NAME:-$SRC_BIN_NAME}"
DEST="$DEST_DIR/$BIN_NAME"

if ! command -v go >/dev/null 2>&1; then
  echo "go not found. use one of:" >&2
  echo "  bash scripts/install-binary.sh" >&2
  echo "  docker build -t volclog:local . && docker run --rm volclog:local --help" >&2
  exit 2
fi

mkdir -p "$DEST_DIR"

cd "$ROOT"
go build "${build_tags[@]}" -o "$DEST" ./cmd/volclog

echo "installed: $DEST"
echo "version:"
"$DEST" --version || true
