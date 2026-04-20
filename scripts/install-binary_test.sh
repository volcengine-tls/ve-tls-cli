#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
esac

fixture_dir="$tmp/fixture"
mkdir -p "$fixture_dir"

cat >"$tmp/volclog" <<'EOF'
#!/usr/bin/env sh
echo "volclog vtest"
EOF
chmod +x "$tmp/volclog"

cat >"$tmp/volclog-human" <<'EOF'
#!/usr/bin/env sh
echo "volclog-human vtest"
EOF
chmod +x "$tmp/volclog-human"

pkg_default="volclog_${os}_${arch}.tar.gz"
pkg_human="volclog-human_${os}_${arch}.tar.gz"

tar -czf "$fixture_dir/$pkg_default" -C "$tmp" volclog
tar -czf "$fixture_dir/$pkg_human" -C "$tmp" volclog-human

python3 - "$fixture_dir/$pkg_default" "$fixture_dir/$pkg_default.sha256" "$fixture_dir/$pkg_human" "$fixture_dir/$pkg_human.sha256" <<'PY'
import hashlib
import pathlib
import sys

for index in range(1, len(sys.argv), 2):
    archive = pathlib.Path(sys.argv[index])
    sha_file = pathlib.Path(sys.argv[index + 1])
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    sha_file.write_text(f"{digest}  dist/{archive.name}\n", encoding="utf-8")
PY

PREFIX="$tmp/prefix" \
VOLCLOG_DOWNLOAD_URL="file://$fixture_dir/$pkg_default" \
bash "$repo_root/scripts/install-binary.sh" >/dev/null

test -x "$tmp/prefix/bin/volclog"

PREFIX="$tmp/prefix-human" \
VOLCLOG_DOWNLOAD_URL="file://$fixture_dir/$pkg_human" \
bash "$repo_root/scripts/install-binary.sh" --edition human >/dev/null

test -x "$tmp/prefix-human/bin/volclog-human"
