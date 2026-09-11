#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EDITION="${VOLCLOG_EDITION:-default}"
BIN_NAME="${BIN_NAME:-}"
PREFIX="${PREFIX:-$HOME/.local}"
DEST_DIR="${DEST_DIR:-$PREFIX/bin}"

build_args=()
case "$EDITION" in
  default|"")
    SRC_BIN_NAME="volclog"
    ;;
  human)
    SRC_BIN_NAME="volclog-human"
    build_args=(-tags=human)
    ;;
  *)
    echo "invalid VOLCLOG_EDITION: $EDITION" >&2
    exit 2
    ;;
esac

BIN_NAME="${BIN_NAME:-$SRC_BIN_NAME}"
DEST="$DEST_DIR/$BIN_NAME"
build_args+=( -o "$DEST" ./cmd/volclog )

if ! command -v go >/dev/null 2>&1; then
  echo "go not found. use one of:" >&2
  echo "  bash scripts/install-binary.sh" >&2
  echo "  docker build -t volclog:local . && docker run --rm volclog:local --help" >&2
  exit 2
fi

mkdir -p "$DEST_DIR"

cd "$ROOT"
if [[ "$(uname -s)" == "Darwin" && "$(go env GOOS)" == "darwin" ]]; then
  # Go 1.22's internal linker can emit a Mach-O binary without LC_UUID on
  # newer macOS versions. dyld rejects that binary before main starts.
  export CGO_ENABLED=1
  export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-11.0}"
  # Go 1.22 does not key every cgo cache entry by deployment target. Rebuild
  # all packages so an earlier build for a newer macOS cannot leak in.
  build_args=(-a -ldflags=-linkmode=external "${build_args[@]}")
fi
go build "${build_args[@]}"

echo "installed: $DEST"
echo "version:"
"$DEST" --version || true
