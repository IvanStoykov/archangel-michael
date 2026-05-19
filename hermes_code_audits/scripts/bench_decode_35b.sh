#!/usr/bin/env bash
# Quick decode bench for llama-qwen-35b-mtp (port 8001, draft-mtp).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_FILE="$AUDIT_ROOT/logs/backend.log"
MAX_TOKENS=128
PROMPT='Count from 1 to 80, one number per line.'

# shellcheck source=lib/audit_common.sh
source "$SCRIPT_DIR/lib/audit_common.sh"
export AUDIT_ROOT
load_audit_profile llama-qwen-35b-mtp

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq required" >&2
  exit 1
fi

echo "35B draft-mtp bench — $(date -Iseconds)"
echo "  profile: llama-qwen-35b-mtp"
echo "  API:     $API_HEALTH_URL"

if ! check_api "$API_HEALTH_URL"; then
  echo "Starting backend ..."
  WAIT_SEC="${WAIT_SEC:-600}" "$SCRIPT_DIR/start_backend.sh" --profile llama-qwen-35b-mtp
fi

mark=0
[[ -f "$LOG_FILE" ]] && mark=$(wc -l <"$LOG_FILE" | tr -d ' ')

http_code=$(curl -sf -o /tmp/bench35b.json -w '%{http_code}' --max-time 600 \
  "${HERMES_BASE_URL}/chat/completions" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n \
    --arg m "$HERMES_MODEL" \
    --arg p "$PROMPT" \
    --argjson n "$MAX_TOKENS" \
    '{model:$m,messages:[{role:"user",content:$p}],max_tokens:$n,temperature:0,stream:false}')" \
  ) || http_code="000"

if [[ "$http_code" != "200" ]]; then
  echo "ERROR: HTTP $http_code" >&2
  cat /tmp/bench35b.json 2>/dev/null >&2 || true
  exit 1
fi

eval_line=$(tail -n +"$((mark + 1))" "$LOG_FILE" 2>/dev/null | grep -E 'eval time =' | tail -1 || true)
accept_line=$(tail -n +"$((mark + 1))" "$LOG_FILE" 2>/dev/null | grep -E 'draft acceptance' | tail -1 || true)

if [[ -n "$eval_line" ]]; then
  tps=$(sed -n 's/.*, *\([0-9.][0-9.]*\) tokens per second.*/\1/p' <<<"$eval_line")
  echo "  eval: ${tps:-?} tok/s"
  [[ -n "$accept_line" ]] && echo "  ${accept_line##* | }"
else
  echo "  (no timing in $LOG_FILE — check server stdout)"
fi

rm -f /tmp/bench35b.json
