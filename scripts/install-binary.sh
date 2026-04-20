#!/usr/bin/env bash
set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
DEST_DIR="${DEST_DIR:-$PREFIX/bin}"

DOWNLOAD_URL="${VOLCLOG_DOWNLOAD_URL:-}"
BASE_URL="${VOLCLOG_BASE_URL:-}"
VERSION="${VOLCLOG_VERSION:-}"
EDITION="${VOLCLOG_EDITION:-}"

usage() {
  cat <<'EOF'
usage: install-binary.sh [--edition human]

Environment:
  VOLCLOG_DOWNLOAD_URL  direct archive URL
  VOLCLOG_BASE_URL      release base URL
  VOLCLOG_EDITION       human to install volclog-human
  BIN_NAME              override installed binary name
EOF
}

verify_archive_checksum() {
  local archive_path="$1"
  local sha_path="$2"
  local expected actual

  expected="$(awk '{print $1}' "$sha_path")"
  if [[ -z "$expected" ]]; then
    echo "invalid sha256 file: $sha_path" >&2
    exit 3
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive_path" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
  else
    echo "missing sha256 tool" >&2
    exit 3
  fi

  if [[ "$expected" != "$actual" ]]; then
    echo "sha256 mismatch" >&2
    exit 3
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --edition)
      if [[ $# -lt 2 ]]; then
        echo "missing --edition value" >&2
        exit 2
      fi
      EDITION="$2"
      shift 2
      ;;
    --edition=*)
      EDITION="${1#*=}"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$EDITION" ]]; then
  if [[ "${BIN_NAME:-}" = "volclog-human" ]]; then
    EDITION="human"
  else
    EDITION="default"
  fi
fi

case "$EDITION" in
  default|"") SRC_BIN_NAME="volclog" ;;
  human) SRC_BIN_NAME="volclog-human" ;;
  *)
    echo "invalid VOLCLOG_EDITION: $EDITION" >&2
    exit 2
    ;;
esac

BIN_NAME="${BIN_NAME:-$SRC_BIN_NAME}"

DEST="$DEST_DIR/$BIN_NAME"

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
    echo "  VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download bash scripts/install-binary.sh --edition human" >&2
    echo "  VOLCLOG_BASE_URL=https://github.com/volcengine-tls/ve-tls-cli/releases/download/<tag> bash scripts/install-binary.sh" >&2
    exit 2
  fi
  pkg="volclog_${os}_${arch}.tar.gz"
  if [[ "$SRC_BIN_NAME" = "volclog-human" ]]; then
    pkg="volclog-human_${os}_${arch}.tar.gz"
  fi
  DOWNLOAD_URL="${BASE_URL%/}/$pkg"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$DEST_DIR"

pkg_file="$(basename "${DOWNLOAD_URL%%\?*}")"
archive_path="$tmp/$pkg_file"

curl -fsSL "$DOWNLOAD_URL" -o "$archive_path"

sha_url="${DOWNLOAD_URL}.sha256"
sha_path="${archive_path}.sha256"
if curl -fsSL "$sha_url" -o "$sha_path" 2>/dev/null; then
  verify_archive_checksum "$archive_path" "$sha_path"
fi

tar -xzf "$archive_path" -C "$tmp"
if [[ ! -f "$tmp/$SRC_BIN_NAME" ]]; then
  echo "binary not found in package" >&2
  exit 4
fi

install -m 0755 "$tmp/$SRC_BIN_NAME" "$DEST"
echo "installed: $DEST"
"$DEST" --version || true
