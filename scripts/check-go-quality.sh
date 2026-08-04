#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

if ! command -v staticcheck >/dev/null 2>&1; then
  echo "error: staticcheck is required but was not found in PATH" >&2
  echo "install it with: go install honnef.co/go/tools/cmd/staticcheck@2024.1.1" >&2
  exit 1
fi

go vet ./...
go vet -tags=human ./...

staticcheck ./...
staticcheck -tags=human ./...
