#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

usage() {
  cat <<'EOF'
Usage: scripts/update-operation-catalog.sh \
  --spec <swagger.json> \
  --api-doc-root <api-doc-directory> \
  --group-key-mapping <group-key-mapping.yaml> \
  --swagger-tag-mapping <swagger-tag-mapping.yaml> \
  --overrides-root <override-directory>

All source paths are required. This command only supports explicit
source-based regeneration of the canonical operation catalog.
EOF
}

SPEC=""
API_DOC_ROOT=""
GROUP_KEY_MAPPING=""
SWAGGER_TAG_MAPPING=""
OVERRIDES_ROOT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --spec)
      SPEC="${2:-}"
      shift 2
      ;;
    --api-doc-root)
      API_DOC_ROOT="${2:-}"
      shift 2
      ;;
    --group-key-mapping)
      GROUP_KEY_MAPPING="${2:-}"
      shift 2
      ;;
    --swagger-tag-mapping)
      SWAGGER_TAG_MAPPING="${2:-}"
      shift 2
      ;;
    --overrides-root)
      OVERRIDES_ROOT="${2:-}"
      shift 2
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

for required in SPEC API_DOC_ROOT GROUP_KEY_MAPPING SWAGGER_TAG_MAPPING OVERRIDES_ROOT; do
  if [[ -z "${!required}" ]]; then
    echo "missing required argument for ${required}" >&2
    usage >&2
    exit 2
  fi
done

cd "$ROOT_DIR"

GO_BIN="${GO:-go}"
"$GO_BIN" run -buildvcs=false ./internal/openapigen \
  --spec "$SPEC" \
  --api-doc-root "$API_DOC_ROOT" \
  --group-key-mapping "$GROUP_KEY_MAPPING" \
  --swagger-tag-mapping "$SWAGGER_TAG_MAPPING" \
  --tool-risk-overrides "$OVERRIDES_ROOT/risk.yaml" \
  --tool-recovery-overrides "$OVERRIDES_ROOT/recovery.yaml" \
  --tool-output-policy-overrides "$OVERRIDES_ROOT/output_policy.yaml" \
  --tool-usage-constraints-overrides "$OVERRIDES_ROOT/usage_constraints.yaml" \
  --internal-operation-overrides "$OVERRIDES_ROOT/internal_operations.json" \
  --supplemental-operation-overrides "$OVERRIDES_ROOT/supplemental_operations.json" \
  --out-operation-catalog internal/contract/generated_catalog.json \
  --out-operation-catalog-lock contracts/operation-catalog-v2-lock.json \
  --lock-root .
