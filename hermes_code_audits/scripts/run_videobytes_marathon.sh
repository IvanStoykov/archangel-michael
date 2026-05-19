#!/usr/bin/env bash
# VideoBytes function-by-function marathon — all package reports under results/vb_audit_*.md
#
# Prereqs (terminal 1):
#   ./start_llama.sh llama-qwen-35b-mtp
#   curl -s http://127.0.0.1:8001/v1/models | jq '.data[0].meta.n_ctx'   # expect 131072
#
# Run (terminal 2 / tmux):
#   ./scripts/run_videobytes_marathon.sh
#   ./scripts/run_videobytes_marathon.sh --dry-run
#   ./scripts/run_videobytes_marathon.sh --task 3
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PREAMBLE_FILE="$AUDIT_ROOT/prompts/vb_marathon_preamble.txt"

if [[ ! -f "$PREAMBLE_FILE" ]]; then
  echo "error: missing $PREAMBLE_FILE" >&2
  exit 1
fi

export AUDIT_PREAMBLE_AGENT
AUDIT_PREAMBLE_AGENT="$(cat "$PREAMBLE_FILE")"

export MANIFEST="$AUDIT_ROOT/tasks.manifest.videobytes-marathon.yaml"
export MIN_OUTPUT_LINES="${MIN_OUTPUT_LINES:-220}"
export TASK_TIMEOUT_SEC="${TASK_TIMEOUT_SEC:-14400}"
export MAX_RETRIES="${MAX_RETRIES:-1}"
export RETRY_SLEEP_SEC="${RETRY_SLEEP_SEC:-45}"
export HERMES_EXTRA_ARGS="${HERMES_EXTRA_ARGS:--Q --max-turns 300}"
export RESUME="${RESUME:-1}"

PROFILE="${PROFILE:-llama-qwen-35b-mtp}"

echo "VideoBytes marathon"
echo "  manifest: $MANIFEST"
echo "  profile:  $PROFILE"
echo "  tasks:    7 packages (function-by-function)"
echo "  min lines per report: $MIN_OUTPUT_LINES"
echo "  timeout per task: ${TASK_TIMEOUT_SEC}s"
echo ""

exec "$AUDIT_ROOT/run_audits.sh" \
  --profile "$PROFILE" \
  --manifest "$(basename "$MANIFEST")" \
  --continue-on-error \
  "$@"
