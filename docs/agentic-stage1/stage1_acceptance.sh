#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

cat > "$TMP_DIR/req.json" <<'EOF'
{"TopicId":"t","Query":"*","StartTime":1710374400000,"EndTime":1710378000000}
EOF

echo "[1/4] Targeted unit tests"
GOCACHE=/tmp/go-build go test ./internal/cli -run 'TestFilterCapabilitiesByGroupAndAction|TestFilterCapabilitiesActionAmbiguous|TestConfigure_ProfileAliasAddUse|TestAPIDryRunDoesNotSendRequest|TestAPIOutputModeFileReturnsEnvelope|TestAPIDryRunWithTraceAddsTracePathToEnvelope|TestRequestTemplateOutputMode|TestCompletionZshIncludesGroupsFlagsAndSubcommands'

echo "[2/4] Progressive disclosure L0-L3"
GOCACHE=/tmp/go-build go run ./cmd/volclog capabilities >/dev/null
GOCACHE=/tmp/go-build go run ./cmd/volclog capabilities --group log >/dev/null
GOCACHE=/tmp/go-build go run ./cmd/volclog api log search-logs -h >/dev/null
VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
  GOCACHE=/tmp/go-build go run ./cmd/volclog --dry-run api log search-logs --request "file://$TMP_DIR/req.json" >/dev/null

echo "[3/4] Trace + envelope(file) in dry-run mode"
VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
  GOCACHE=/tmp/go-build go run ./cmd/volclog --trace-dir "$TMP_DIR/traces" --output-mode file --output-file "$TMP_DIR/out.json" --dry-run api log search-logs --request "file://$TMP_DIR/req.json" >/dev/null

echo "[4/4] Notes"
echo "L4 real request is not executed by this script."
echo "To verify L4 online:"
echo "  volclog api log search-logs --request file://./req.json"

echo "Stage1 acceptance checks passed (offline subset)."
