#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

stub_bin="$tmp/bin"
mkdir -p "$stub_bin"

cat >"$stub_bin/uname" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "-s" ]]; then
  printf '%s\n' "${TEST_HOST_OS:?}"
  exit 0
fi
exec /usr/bin/uname "$@"
EOF
chmod +x "$stub_bin/uname"

cat >"$stub_bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "env" && "${2:-}" == "GOOS" ]]; then
  printf '%s\n' "${TEST_TARGET_OS:?}"
  exit 0
fi
if [[ "${1:-}" != "build" ]]; then
  echo "unexpected go invocation: $*" >&2
  exit 2
fi
printf '<%s>\n' "$@" >"${TEST_GO_LOG:?}"
printf '<CGO_ENABLED=%s>\n' "${CGO_ENABLED:-unset}" >>"$TEST_GO_LOG"
printf '<MACOSX_DEPLOYMENT_TARGET=%s>\n' "${MACOSX_DEPLOYMENT_TARGET:-unset}" >>"$TEST_GO_LOG"
dest=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then
    dest="${2:?}"
    break
  fi
  shift
done
if [[ -z "$dest" ]]; then
  echo "missing go build -o destination" >&2
  exit 2
fi
cat >"$dest" <<'SCRIPT'
#!/usr/bin/env sh
echo "volclog test"
SCRIPT
chmod +x "$dest"
EOF
chmod +x "$stub_bin/go"

run_case() {
  local name="$1"
  local host_os="$2"
  local target_os="$3"
  local edition="$4"
  local want_external="$5"
  local want_human="$6"
  local case_root="$tmp/$name"
  local go_log="$case_root/go.log"
  mkdir -p "$case_root/bin"

  PATH="$stub_bin:$PATH" \
    TEST_HOST_OS="$host_os" \
    TEST_TARGET_OS="$target_os" \
    TEST_GO_LOG="$go_log" \
    DEST_DIR="$case_root/bin" \
    VOLCLOG_EDITION="$edition" \
    bash "$repo_root/scripts/install-local.sh" >/dev/null

  if [[ "$want_external" == "yes" ]]; then
    grep -Fxq '<-a>' "$go_log"
    grep -Fxq '<-ldflags=-linkmode=external>' "$go_log"
    grep -Fxq '<CGO_ENABLED=1>' "$go_log"
    grep -Fxq '<MACOSX_DEPLOYMENT_TARGET=11.0>' "$go_log"
  elif grep -Eq '<(-a|-ldflags=-linkmode=external)>' "$go_log"; then
    echo "$name unexpectedly enabled the Darwin rebuild path" >&2
    exit 1
  elif ! grep -Fxq '<MACOSX_DEPLOYMENT_TARGET=unset>' "$go_log"; then
    echo "$name unexpectedly set a macOS deployment target" >&2
    exit 1
  fi

  if [[ "$want_human" == "yes" ]]; then
    grep -Fxq '<-tags=human>' "$go_log"
    test -x "$case_root/bin/volclog-human"
  else
    if grep -Fq '<-tags=human>' "$go_log"; then
      echo "$name unexpectedly enabled the human build tag" >&2
      exit 1
    fi
    test -x "$case_root/bin/volclog"
  fi
}

run_case darwin-default Darwin darwin default yes no
run_case darwin-human Darwin darwin human yes yes
run_case darwin-linux-target Darwin linux default no no
run_case linux-default Linux linux default no no
