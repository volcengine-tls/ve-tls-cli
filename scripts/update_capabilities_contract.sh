#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

GOCACHE_DIR="${GOCACHE:-/tmp/go-build}"

echo "[capabilities-contract] updating lock + snapshot ..."
UPDATE_CAPABILITIES_CONTRACT=1 GOCACHE="$GOCACHE_DIR" go test ./internal/cli -run TestCapabilitiesContractSnapshot -count=1

echo "[capabilities-contract] updated files:"
echo "  - contracts/agentic-stage1/tool-contract-lock.json"
echo "  - contracts/agentic-stage1/tool-contract-snapshot.txt"
