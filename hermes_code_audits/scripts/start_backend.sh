#!/usr/bin/env bash
# Start inference backend for a profile; wait until API is healthy.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="$AUDIT_ROOT/logs"
PID_FILE="$LOG_DIR/backend.pid"
LOG_FILE="$LOG_DIR/backend.log"

# shellcheck source=lib/audit_common.sh
source "$SCRIPT_DIR/lib/audit_common.sh"

usage() {
  echo "Usage: $(basename "$0") --profile <name> [--foreground] [--stop]"
}

PROFILE=""
FOREGROUND=0
STOP_ONLY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="${2:?}"; shift 2 ;;
    --foreground) FOREGROUND=1; shift ;;
    --stop) STOP_ONLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$PROFILE" ]]; then
  echo "ERROR: --profile required" >&2
  usage >&2
  exit 1
fi

export AUDIT_ROOT
load_audit_profile "$PROFILE"
WAIT_SEC="${WAIT_SEC:-300}"
mkdir -p "$LOG_DIR"

# Export for atomic-serve script
[[ -n "${MAIN_GGUF:-}" ]] && export MAIN_GGUF
[[ -n "${MAIN_GGUF_CHECK:-}" && -z "${MAIN_GGUF:-}" ]] && export MAIN_GGUF="$MAIN_GGUF_CHECK"
export CTX="${CTX:-32768}" NGL="${NGL:-14}" LLAMA_ROOT="${LLAMA_ROOT:-/home/ivan/git/atomic-llama-cpp-turboquant}"
export KV_CACHE="${KV_CACHE:-f16}" LLAMA_FIT="${LLAMA_FIT:-on}" FIT_TARGET="${FIT_TARGET:-1536}"
export FIT_CTX_MIN="${FIT_CTX_MIN:-4096}" FA="${FA:-on}" UBATCH="${UBATCH:-512}"
[[ -n "${NGL_DRAFT:-}" ]] && export NGL_DRAFT
[[ -n "${DRAFT_MAX:-}" ]] && export DRAFT_MAX
[[ -n "${DRAFT_MIN:-}" ]] && export DRAFT_MIN
[[ -n "${VERIFY_NEXTN_GGUF:-}" ]] && export VERIFY_NEXTN_GGUF
[[ -n "${LLAMA_UPSTREAM_ROOT:-}" ]] && export LLAMA_UPSTREAM_ROOT
[[ -n "${MODEL_DIR:-}" ]] && export MODEL_DIR
[[ -n "${SPEC_DRAFT_N_MAX:-}" ]] && export SPEC_DRAFT_N_MAX
[[ -n "${CACHE_RAM:-}" ]] && export CACHE_RAM
[[ -n "${CTX_CHECKPOINTS:-}" ]] && export CTX_CHECKPOINTS
[[ -n "${BACKEND_PORT:-}" ]] && export PORT="${PORT:-$BACKEND_PORT}"

stop_port() {
  local port="${1:-}"
  [[ -z "$port" ]] && return 0
  if command -v fuser >/dev/null 2>&1; then
    fuser -k "${port}/tcp" 2>/dev/null || true
    sleep 1
  fi
}

if [[ -n "${BACKEND_STOP_PATTERN:-}" ]]; then
  pkill -f "$BACKEND_STOP_PATTERN" 2>/dev/null || true
  sleep 1
fi
[[ -n "${BACKEND_PORT:-}" ]] && stop_port "$BACKEND_PORT"

if [[ "$STOP_ONLY" -eq 1 ]]; then
  rm -f "$PID_FILE"
  echo "Stopped backend for profile $PROFILE (port ${BACKEND_PORT:-n/a})"
  exit 0
fi

if [[ -z "${BACKEND_START_CMD:-}" ]]; then
  echo "Profile $PROFILE uses external backend (e.g. Ollama). Ensure service is running."
  if check_api "$API_HEALTH_URL"; then
    echo "API healthy: $API_HEALTH_URL"
    exit 0
  fi
  echo "ERROR: API not reachable: $API_HEALTH_URL" >&2
  exit 1
fi

# GGUF check for llama profiles
if [[ -n "${MAIN_GGUF_CHECK:-}" && ! -f "${MAIN_GGUF_CHECK}" ]]; then
  if [[ -n "${MAIN_GGUF_ALT:-}" && -f "${MAIN_GGUF_ALT}" ]]; then
    export MAIN_GGUF="$MAIN_GGUF_ALT"
  else
    echo "ERROR: GGUF missing for $PROFILE" >&2
    echo "  expected: ${MAIN_GGUF_CHECK}" >&2
    [[ -n "${MAIN_GGUF_ALT:-}" ]] && echo "  or:       ${MAIN_GGUF_ALT}" >&2
    exit 1
  fi
elif [[ -n "${MAIN_GGUF_CHECK:-}" ]]; then
  export MAIN_GGUF="${MAIN_GGUF:-$MAIN_GGUF_CHECK}"
fi

echo "Starting backend ($PROFILE) ..."
echo "  log: $LOG_FILE"
if [[ -n "${LLAMA_UPSTREAM_ROOT:-}" ]]; then
  echo "  stack: upstream llama.cpp draft-mtp"
  echo "  MAIN_GGUF=${MAIN_GGUF:-n/a} CTX=${CTX} FIT_TARGET=${FIT_TARGET:-1536} SPEC_DRAFT_N_MAX=${SPEC_DRAFT_N_MAX:-2} PORT=${BACKEND_PORT:-8001}"
else
  echo "  MAIN_GGUF=${MAIN_GGUF:-n/a} CTX=${CTX} NGL=${NGL} KV=${KV_CACHE:-f16} FIT=${LLAMA_FIT:-on} PORT=${BACKEND_PORT:-8080}"
  [[ -n "${DRAFT_MAX:-}" ]] && echo "  NextN: NGL_DRAFT=${NGL_DRAFT:-$NGL} DRAFT_MAX=${DRAFT_MAX} DRAFT_MIN=${DRAFT_MIN:-0}"
fi

if [[ "$FOREGROUND" -eq 1 ]]; then
  # shellcheck disable=SC2086
  eval "$BACKEND_START_CMD" 2>&1 | tee -a "$LOG_FILE"
  exit "${PIPESTATUS[0]}"
fi

# shellcheck disable=SC2086
nohup bash -c "$BACKEND_START_CMD" >>"$LOG_FILE" 2>&1 &
echo $! >"$PID_FILE"
echo "  pid: $(cat "$PID_FILE")"

backend_pid_alive() {
  [[ -f "$PID_FILE" ]] || return 1
  local pid
  pid="$(cat "$PID_FILE" 2>/dev/null)" || return 1
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

elapsed=0
while [[ "$elapsed" -lt "$WAIT_SEC" ]]; do
  if check_api "$API_HEALTH_URL"; then
    echo "API ready after ${elapsed}s: $API_HEALTH_URL"
    exit 0
  fi
  if [[ -f "$PID_FILE" ]] && ! backend_pid_alive; then
    echo "ERROR: backend process exited before API was ready — see $LOG_FILE" >&2
    tail -40 "$LOG_FILE" >&2 || true
    if grep -q 'ModuleNotFoundError: No module named' "$LOG_FILE" 2>/dev/null; then
      echo "" >&2
      echo "Python dependency missing (often numpy for NextN verify)." >&2
      echo "  VERIFY_NEXTN_GGUF=0 ./start_llama.sh llama-qwen-27b-stable-nextn" >&2
      echo "  # or: pip install numpy" >&2
    fi
    exit 1
  fi
  sleep 5
  elapsed=$((elapsed + 5))
  echo "  waiting... ${elapsed}s"
done

echo "ERROR: API not healthy after ${WAIT_SEC}s — see $LOG_FILE" >&2
tail -30 "$LOG_FILE" >&2 || true
if grep -q 'ModuleNotFoundError: No module named' "$LOG_FILE" 2>/dev/null; then
  echo "" >&2
  echo "Hint: NextN preflight verify failed — retry with VERIFY_NEXTN_GGUF=0" >&2
elif grep -q 'cudaMalloc failed: out of memory' "$LOG_FILE" 2>/dev/null; then
  echo "" >&2
  echo "GPU OOM while loading model. Free VRAM (stop Ollama/other apps) or retry:" >&2
  if [[ -n "${LLAMA_UPSTREAM_ROOT:-}" ]]; then
    echo "  FIT_TARGET=2048 ./start_llama.sh llama-qwen-35b-mtp" >&2
    echo "  # or set MAIN_GGUF to Q3_K_XL in profiles/llama-qwen-35b-mtp.conf" >&2
  else
    echo "  NGL=10 ./start_llama.sh" >&2
    echo "  KV_CACHE=q8_0 ./scripts/start_backend.sh --profile llama-qwen-27b-q8" >&2
    echo "  ./scripts/llama-fit-qwen27.sh  # print fitted -ngl/-c if fit-params is built" >&2
  fi
fi
exit 1
