#!/usr/bin/env bash
# Compare baseline vs NextN decode speed (restarts llama-server twice).
#
# Usage:
#   ./scripts/bench_decode.sh
#   NGL=14 CTX=8192 ./scripts/bench_decode.sh
#   ./scripts/bench_decode.sh --max-tokens 256 --prompt "Count from 1 to 200."
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_FILE="$AUDIT_ROOT/logs/backend.log"

# shellcheck source=lib/audit_common.sh
source "$SCRIPT_DIR/lib/audit_common.sh"

BASELINE_PROFILE=llama-qwen-27b-stable
NEXTN_PROFILE=llama-qwen-27b-stable-nextn
MAX_TOKENS=128
PROMPT='Count from 1 to 80, one number per line.'
SKIP_RESTART=0
WAIT_SEC=300

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Restarts the backend twice (baseline, then NextN), runs one chat completion each,
and prints eval tok/s from logs/backend.log.

Options:
  --max-tokens N     Completion length (default: $MAX_TOKENS)
  --prompt TEXT      User message (default: fixed counting prompt)
  --ctx N            Server context (exported as CTX for both runs)
  --ngl N            GPU layers (exported as NGL / NGL_DRAFT)
  --no-restart       Use already-running server (single run only; pass BASELINE or NEXTN)
  --wait-sec N       API wait timeout per start (default: $WAIT_SEC)
  -h, --help         This help

Environment overrides (apply to both runs):
  CTX, NGL, NGL_DRAFT, DRAFT_MAX, KV_CACHE, MAIN_GGUF

Examples:
  ./scripts/bench_decode.sh
  NGL=20 CTX=8192 ./scripts/bench_decode.sh
  ./start_llama.sh llama-qwen-27b-stable-nextn
  ./scripts/bench_decode.sh --no-restart   # not useful alone; use two manual starts
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --max-tokens) MAX_TOKENS="${2:?}"; shift 2 ;;
    --prompt) PROMPT="${2:?}"; shift 2 ;;
    --ctx) export CTX="${2:?}"; shift 2 ;;
    --ngl) export NGL="${2:?}"; export NGL_DRAFT="${NGL_DRAFT:-$NGL}"; shift 2 ;;
    --no-restart) SKIP_RESTART=1; shift ;;
    --wait-sec) WAIT_SEC="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

export AUDIT_ROOT
export WAIT_SEC
# GGUF verify needs numpy in gguf-py; skip for bench
export VERIFY_NEXTN_GGUF=0

log_mark() {
  if [[ -f "$LOG_FILE" ]]; then
    wc -l <"$LOG_FILE" | tr -d ' '
  else
    echo 0
  fi
}

parse_timing() {
  local label="$1"
  local start_line="$2"
  local eval_line prompt_line accept_line
  eval_line=$(tail -n +"$((start_line + 1))" "$LOG_FILE" 2>/dev/null | grep -E 'eval time =' | tail -1 || true)
  prompt_line=$(tail -n +"$((start_line + 1))" "$LOG_FILE" 2>/dev/null | grep -E 'prompt eval time =' | tail -1 || true)
  accept_line=$(tail -n +"$((start_line + 1))" "$LOG_FILE" 2>/dev/null | grep -E 'draft acceptance rate' | tail -1 || true)

  if [[ -z "$eval_line" ]]; then
    echo "  $label: ERROR — no eval timing in log (see $LOG_FILE)"
    return 1
  fi

  local eval_tps prompt_tps eval_tokens
  eval_tps=$(sed -n 's/.*, *\([0-9.][0-9.]*\) tokens per second.*/\1/p' <<<"$eval_line")
  prompt_tps=$(sed -n 's/.*, *\([0-9.][0-9.]*\) tokens per second.*/\1/p' <<<"$prompt_line")
  eval_tokens=$(sed -n 's/.*eval time =[^/]*\/ *\([0-9][0-9]*\) tokens.*/\1/p' <<<"$eval_line")

  printf "  %-10s eval: %6s tok/s (%s tokens)" "$label" "${eval_tps:-?}" "${eval_tokens:-?}"
  [[ -n "$prompt_tps" ]] && printf "  |  prompt: %s tok/s" "$prompt_tps"
  echo
  if [[ -n "$accept_line" ]]; then
    echo "             ${accept_line#slot print_timing: }"
  fi
}

run_completion() {
  local model="$1"
  local tmp
  tmp="$(mktemp)"
  local http_code
  http_code=$(curl -sf -o "$tmp" -w '%{http_code}' --max-time 600 \
    "http://127.0.0.1:${BACKEND_PORT:-8080}/v1/chat/completions" \
    -H 'Content-Type: application/json' \
    -d "$(jq -n \
      --arg m "$model" \
      --arg p "$PROMPT" \
      --argjson n "$MAX_TOKENS" \
      '{model:$m,messages:[{role:"user",content:$p}],max_tokens:$n,temperature:0,stream:false}')" \
    ) || http_code="000"
  if [[ "$http_code" != "200" ]]; then
    echo "ERROR: API returned HTTP $http_code" >&2
    tail -20 "$tmp" >&2 || true
    rm -f "$tmp"
    return 1
  fi
  rm -f "$tmp"
}

bench_profile() {
  local profile="$1"
  local label="$2"

  echo ""
  echo "━━━ $label ($profile) ━━━"
  export AUDIT_ROOT WAIT_SEC
  load_audit_profile "$profile"
  export MAIN_GGUF="${MAIN_GGUF:-$MAIN_GGUF_CHECK}"
  export CTX NGL KV_CACHE LLAMA_FIT FIT_TARGET FIT_CTX_MIN
  [[ -n "${NGL_DRAFT:-}" ]] && export NGL_DRAFT
  [[ -n "${DRAFT_MAX:-}" ]] && export DRAFT_MAX
  [[ -n "${DRAFT_MIN:-}" ]] && export DRAFT_MIN

  echo "  CTX=${CTX:-?} NGL=${NGL:-?} KV=${KV_CACHE:-f16} DRAFT_MAX=${DRAFT_MAX:-n/a}"

  local mark
  mark="$(log_mark)"

  if [[ "$SKIP_RESTART" -eq 0 ]]; then
    "$SCRIPT_DIR/start_backend.sh" --profile "$profile" --stop >/dev/null
    "$SCRIPT_DIR/start_backend.sh" --profile "$profile"
  else
    if ! check_api "${API_HEALTH_URL:-http://127.0.0.1:8080/v1/models}"; then
      echo "ERROR: API not up; start server first or drop --no-restart" >&2
      return 1
    fi
  fi

  # Warm slot (short) so timing reflects decode not cold load
  run_completion "$HERMES_MODEL" >/dev/null 2>&1 || true
  sleep 2
  mark="$(log_mark)"

  echo "  Running completion (max_tokens=$MAX_TOKENS) ..."
  if ! run_completion "$HERMES_MODEL"; then
    return 1
  fi
  sleep 1
  parse_timing "$label" "$mark"
}

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq required for bench_decode.sh" >&2
  exit 1
fi

mkdir -p "$AUDIT_ROOT/logs"

echo "Decode benchmark — $(date -Iseconds)"
echo "  prompt: ${PROMPT:0:60}..."
echo "  max_tokens: $MAX_TOKENS"
if command -v nvidia-smi >/dev/null 2>&1; then
  echo "  GPU: $(nvidia-smi --query-gpu=name,memory.used,memory.total --format=csv,noheader 2>/dev/null | head -1)"
fi

if [[ "$SKIP_RESTART" -eq 1 ]]; then
  bench_profile "$NEXTN_PROFILE" "nextn"
  exit 0
fi

bench_profile "$BASELINE_PROFILE" "baseline"
bench_profile "$NEXTN_PROFILE" "nextn"

echo ""
echo "Done. If NextN is slower, try higher NGL or lower CTX; OOM → llama-qwen-27b-q8 profile."
echo "For upstream draft-mtp A/B, build ggml-org/llama.cpp and compare separately."
