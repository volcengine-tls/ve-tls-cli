#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
echo '{"version":1,"profiles":{}}' > "$TMP_DIR/config.json"

cat > "$TMP_DIR/req.json" <<'EOF'
{"TopicId":"t","Query":"*","StartTime":1710374400000,"EndTime":1710378000000}
EOF
cat > "$TMP_DIR/project_req.json" <<'EOF'
{"ProjectName":"demo","Region":"cn-beijing"}
EOF
cat > "$TMP_DIR/ctx.json" <<'EOF'
{"region":"cn-beijing","execution":{"dry_run":true}}
EOF
cat > "$TMP_DIR/page_all_ctx.json" <<'EOF'
{"region":"cn-beijing","execution":{"dry_run":true,"page":{"all":true}}}
EOF
cat > "$TMP_DIR/topics_req.json" <<'EOF'
{"query":{"PageSize":2}}
EOF

roundtrip_check() {
  local action="$1"
  local context_file="$2"
  local input_file="$3"
  local desc_json="$TMP_DIR/${action//./_}_describe.json"
  local exec_json="$TMP_DIR/${action//./_}_exec.json"
  local desc_required_path="$TMP_DIR/${action//./_}_desc_required.json"
  local exec_body_keys_path="$TMP_DIR/${action//./_}_exec_body_keys.json"

  GOCACHE=/tmp/go-build go run ./cmd/volclog --output json tool describe "$action" > "$desc_json"
  VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
    GOCACHE=/tmp/go-build go run ./cmd/volclog --output json tool exec "$action" --context "file://$context_file" --input "file://$input_file" > "$exec_json"

  jq '.input.body.required // []' "$desc_json" > "$desc_required_path"
  jq '.data.request_preview.body | keys // []' "$exec_json" > "$exec_body_keys_path"

  if ! jq -e -n --slurpfile desc_required "$desc_required_path" --slurpfile exec_keys "$exec_body_keys_path" '$desc_required[0] - $exec_keys[0] | length == 0' >/dev/null; then
    echo "Contract drift detected for $action: required body keys from tool describe do not align with tool exec request_preview.body."
    echo "describe required: $(cat "$desc_required_path")"
    echo "exec body keys:   $(cat "$exec_body_keys_path")"
    exit 1
  fi

  if ! jq -e '.data.request_preview.body | type=="object" and (type=="object")' "$exec_json" >/dev/null; then
    echo "Contract drift detected for $action: tool exec request_preview.body is missing or not an object."
    exit 1
  fi
}

echo "[1/5] Targeted unit tests"
GOCACHE=/tmp/go-build go test ./internal/cli -run 'TestFilterCapabilitiesByGroupAndAction|TestFilterCapabilitiesActionAmbiguous|TestConfigure_ProfileAliasAddUse|TestAPIDryRunDoesNotSendRequest|TestAPIOutputModeFileReturnsEnvelope|TestAPIDryRunWithTraceAddsTracePathToEnvelope|TestRequestTemplateOutputMode|TestCompletionZshIncludesGroupsFlagsAndSubcommands'

echo "[2/5] Progressive disclosure L0-L3"
GOCACHE=/tmp/go-build go run ./cmd/volclog tool list >/dev/null
GOCACHE=/tmp/go-build go run ./cmd/volclog tool list log >/dev/null
GOCACHE=/tmp/go-build go run ./cmd/volclog tool describe log.search-logs >/dev/null
VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
  GOCACHE=/tmp/go-build go run ./cmd/volclog tool exec log.search-logs --context "file://$TMP_DIR/ctx.json" --input "file://$TMP_DIR/req.json" >/dev/null

echo "[3/5] Describe/exec contract roundtrip"
roundtrip_check "log.search-logs" "$TMP_DIR/ctx.json" "$TMP_DIR/req.json"
roundtrip_check "project.create-project" "$TMP_DIR/ctx.json" "$TMP_DIR/project_req.json"

echo "[4/5] supports_all contract + dry-run page.all"
TOPICS_DESC="$TMP_DIR/topic_describe.json"
GOCACHE=/tmp/go-build go run ./cmd/volclog --output json tool describe topic.describe-topics > "$TOPICS_DESC"
if ! jq -e '.execution.supports_all == true' "$TOPICS_DESC" >/dev/null; then
  echo "Expected topic.describe-topics to advertise execution.supports_all=true"
  exit 1
fi
VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
  GOCACHE=/tmp/go-build go run ./cmd/volclog --output json tool exec topic.describe-topics --context "file://$TMP_DIR/page_all_ctx.json" --input "file://$TMP_DIR/topics_req.json" >/dev/null

echo "[5/5] Trace + envelope(file) in tool dry-run mode"
cat > "$TMP_DIR/file_ctx.json" <<EOF
{"region":"cn-beijing","execution":{"dry_run":true,"output":{"mode":"file","dir":"$TMP_DIR/out"}}}
EOF
VOLCENGINE_ACCESS_KEY_ID=ak VOLCENGINE_ACCESS_KEY_SECRET=sk VOLCENGINE_REGION=cn-beijing VOLCENGINE_ENDPOINT=https://tls-cn-beijing.volces.com VOLCLOG_CONFIG="$TMP_DIR/config.json" \
  GOCACHE=/tmp/go-build go run ./cmd/volclog --trace-dir "$TMP_DIR/traces" tool exec log.search-logs --context "file://$TMP_DIR/file_ctx.json" --input "file://$TMP_DIR/req.json" >/dev/null

echo "[5/5] Notes"
echo "L4 real request is not executed by this script."
echo "To verify L4 online:"
echo "  volclog tool exec log.search-logs --context file://./ctx.json --input file://./req.json"

echo "Stage1 acceptance checks passed (offline subset)."
