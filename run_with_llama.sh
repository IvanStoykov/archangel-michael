#!/usr/bin/env bash
# One-shot: start stable llama-server → smoke test → run audits.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROFILE="${AUDIT_PROFILE:-llama-qwen-27b-stable}"

echo "=== 1/3 Start llama-server ($PROFILE) ==="
"$SCRIPT_DIR/scripts/start_backend.sh" --profile "$PROFILE"

echo ""
echo "=== 2/3 Smoke test ==="
"$SCRIPT_DIR/scripts/smoke_test.sh" --profile "$PROFILE"

echo ""
echo "=== 3/3 Run audits ==="
exec "$SCRIPT_DIR/run_audits.sh" --profile "$PROFILE" "$@"
