#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

GOCACHE_DIR="${GOCACHE:-/tmp/go-build}"

echo "[capabilities-contract] updating lock + snapshot ..."
UPDATE_CAPABILITIES_CONTRACT=1 GOCACHE="$GOCACHE_DIR" go test ./internal/cli -run TestCapabilitiesContractSnapshot -count=1

echo "[capabilities-contract] updated files:"
echo "  - cospec/agentic-stage1/capabilities-contract-lock.json"
echo "  - cospec/agentic-stage1/capabilities-contract-snapshot.txt"
echo
echo "[next] append one entry to cospec/agentic-stage1/capabilities-contract-changelog.md before commit."
