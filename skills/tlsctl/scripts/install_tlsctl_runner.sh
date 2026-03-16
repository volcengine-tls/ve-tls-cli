#!/usr/bin/env bash
set -euo pipefail

base_url="${TLSCTL_RUNNER_BASE_URL:-https://github.com/volcengine-tls/ve-tls-cli/releases/latest/download}"
install_dir="${TLSCTL_RUNNER_INSTALL_DIR:-$HOME/.local/bin}"

mkdir -p "$install_dir"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
esac

bin_name="tlsctl-runner"
pkg=""
if [[ "$os" == "darwin" || "$os" == "linux" ]]; then
  pkg="tlsctl-runner_${os}_${arch}.tar.gz"
elif [[ "$os" == "mingw64_nt"* || "$os" == "msys_nt"* || "$os" == "cygwin_nt"* ]]; then
  echo "Windows please use install_tlsctl_runner.ps1"
  exit 1
else
  echo "unsupported os: $os"
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

curl -fsSL "$base_url/$pkg" -o "$tmp_dir/$pkg"
if curl -fsSL "$base_url/$pkg.sha256" -o "$tmp_dir/$pkg.sha256"; then
  (cd "$tmp_dir" && sha256sum -c "$pkg.sha256")
fi

tar -xzf "$tmp_dir/$pkg" -C "$tmp_dir"
install -m 0755 "$tmp_dir/$bin_name" "$install_dir/$bin_name"

echo "installed: $install_dir/$bin_name"

