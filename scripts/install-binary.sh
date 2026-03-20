#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="${BIN_NAME:-volclog}"
PREFIX="${PREFIX:-$HOME/.local}"
DEST_DIR="${DEST_DIR:-$PREFIX/bin}"
DEST="$DEST_DIR/$BIN_NAME"

DOWNLOAD_URL="${VOLCLOG_DOWNLOAD_URL:-}"
BASE_URL="${VOLCLOG_BASE_URL:-}"
VERSION="${VOLCLOG_VERSION:-}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
esac

if [[ -z "$DOWNLOAD_URL" ]]; then
  if [[ -z "$BASE_URL" ]]; then
    echo "missing VOLCLOG_DOWNLOAD_URL or VOLCLOG_BASE_URL" >&2
    echo "examples:" >&2
    echo "  VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh" >&2
    echo "  VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/download/<tag> bash scripts/install-binary.sh" >&2
    exit 2
  fi
  pkg="volclog_${os}_${arch}.tar.gz"
  DOWNLOAD_URL="${BASE_URL%/}/$pkg"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$DEST_DIR"

curl -fsSL "$DOWNLOAD_URL" -o "$tmp/pkg.tgz"

sha_url="${DOWNLOAD_URL}.sha256"
if curl -fsSL "$sha_url" -o "$tmp/pkg.tgz.sha256" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c pkg.tgz.sha256)
  elif command -v shasum >/dev/null 2>&1; then
    expected="$(awk '{print $1}' "$tmp/pkg.tgz.sha256")"
    actual="$(shasum -a 256 "$tmp/pkg.tgz" | awk '{print $1}')"
    if [[ "$expected" != "$actual" ]]; then
      echo "sha256 mismatch" >&2
      exit 3
    fi
  fi
fi

tar -xzf "$tmp/pkg.tgz" -C "$tmp"
if [[ ! -f "$tmp/$BIN_NAME" ]]; then
  echo "binary not found in package" >&2
  exit 4
fi

install -m 0755 "$tmp/$BIN_NAME" "$DEST"
echo "installed: $DEST"
"$DEST" --version || true
