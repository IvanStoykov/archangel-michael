#!/usr/bin/env bash
# Sequential overnight code audits via Hermes. Safe for tmux/SSH detach.
#
# Usage:
#   ./run_audits.sh --profile ollama-e4b
#   ./run_audits.sh --profile llama-gemma-26b --dry-run
#   ./run_audits.sh --profile ollama-e4b --task 3
#   ./run_audits.sh --continue-on-error

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMPTS_DIR="$SCRIPT_DIR"
RESULTS_DIR="$PROMPTS_DIR/results"
LOG_DIR="$PROMPTS_DIR/logs"
RUN_LOG="$LOG_DIR/run.jsonl"
SUMMARY_FILE="$RESULTS_DIR/_summary.txt"
LOCK_FILE="$RESULTS_DIR/.run_audits.lock"
MANIFEST="${MANIFEST:-$SCRIPT_DIR/tasks.manifest.yaml}"

# shellcheck source=scripts/lib/audit_common.sh
source "$SCRIPT_DIR/scripts/lib/audit_common.sh"
# shellcheck source=scripts/lib/manifest.sh
source "$SCRIPT_DIR/scripts/lib/manifest.sh"

# Defaults (overridden by audit.conf.active or audit.conf)
HERMES_PROVIDER="${HERMES_PROVIDER:-custom}"
HERMES_MODEL="${HERMES_MODEL:-Qwen3.6-27B-Q4_K_M.gguf}"
API_HEALTH_URL="${API_HEALTH_URL:-http://127.0.0.1:8080/v1/models}"
AUDIT_PROFILE="${AUDIT_PROFILE:-llama-qwen-27b-stable}"
TASK_TIMEOUT_SEC="${TASK_TIMEOUT_SEC:-10800}"
MAX_RETRIES="${MAX_RETRIES:-2}"
RETRY_SLEEP_SEC="${RETRY_SLEEP_SEC:-30}"
RESUME="${RESUME:-1}"
MIN_OUTPUT_LINES="${MIN_OUTPUT_LINES:-200}"
HERMES_EXTRA_ARGS="${HERMES_EXTRA_ARGS:--Q --max-turns 200}"
SAVE_PARTIAL="${SAVE_PARTIAL:-1}"
AUDIT_MODE="${AUDIT_MODE:-agent}"

if [[ -z "${AUDIT_PREAMBLE_AGENT:-}" ]]; then
  AUDIT_PREAMBLE_AGENT='RUNTIME INSTRUCTIONS (override conflicting lines below):
- You are in the target repository workspace. Use read_file and search_files only (never terminal:command).
- Do NOT run builds, tests, or shell commands. Read-only static analysis.
- You MUST complete every section under DELIVERABLES with evidence from the code before finishing.
- Do not stop with a plan or "next steps"; deliver the full report in this session.

'
fi

AUDIT_PREAMBLE_BUNDLED='RUNTIME INSTRUCTIONS (override conflicting lines below):
- Source code excerpts are appended below under SOURCE FILES. Do NOT call any tools.
- Analyze only the bundled source. Cite file paths from the bundle.
- You MUST complete every section under DELIVERABLES before finishing.
- Do not stop with a plan or "next steps"; deliver the full report in this session.

'
AUDIT_PROFILE="${AUDIT_PROFILE:-}"

[[ -f "$SCRIPT_DIR/audit.conf.active" ]] && source "$SCRIPT_DIR/audit.conf.active"
[[ -f "$SCRIPT_DIR/audit.conf" ]] && source "$SCRIPT_DIR/audit.conf"

DRY_RUN=0
ONLY_TASK=0
CONTINUE_ON_ERROR=0
PROFILE_ARG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --task) ONLY_TASK="${2:?}"; shift 2 ;;
    --profile) PROFILE_ARG="${2:?}"; shift 2 ;;
    --continue-on-error) CONTINUE_ON_ERROR=1; shift ;;
    --manifest)
      if [[ "$2" == */* ]]; then
        MANIFEST="$2"
      else
        MANIFEST="$SCRIPT_DIR/$2"
      fi
      shift 2
      ;;
    -h|--help)
      sed -n '2,15p' "$0"
      echo "  --profile NAME     Load profiles/NAME.conf and sync Hermes"
      echo "  --manifest FILE  tasks.manifest.yaml or tasks.manifest.videobytes-marathon.yaml"
      echo "  --continue-on-error  Do not stop after first failure"
      echo "  VideoBytes marathon: ./scripts/run_videobytes_marathon.sh"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -n "$PROFILE_ARG" ]]; then
  export AUDIT_ROOT="$SCRIPT_DIR"
  load_audit_profile "$PROFILE_ARG"
  AUDIT_PROFILE="$PROFILE_ARG"
  "$SCRIPT_DIR/scripts/apply_profile.sh" "$PROFILE_ARG"
  # shellcheck source=/dev/null
  source "$SCRIPT_DIR/audit.conf.active"
fi

load_tasks_manifest "$MANIFEST" || exit 1
TOTAL=${#ORDER[@]}

log_json() {
  local payload="$1"
  mkdir -p "$LOG_DIR"
  printf '%s\n' "$payload" >> "$RUN_LOG"
}

timestamp() { date -Iseconds; }

acquire_lock() {
  mkdir -p "$RESULTS_DIR"
  exec 200>"$LOCK_FILE"
  if ! flock -n 200; then
    echo "ERROR: Another run_audits.sh is already running (lock: $LOCK_FILE)" >&2
    echo "       If stale: rm -f $LOCK_FILE" >&2
    exit 1
  fi
}

run_one_task() {
  local prompt_file="$1"
  local task_id repo output tmp prompt_path
  local attempt=1 max_attempts exit_code=0 started ended lines reason
  local -a hermes_cmd

  task_id="$(task_id_for_prompt "$prompt_file")"
  repo="${TASK_REPOS[$prompt_file]}"
  output="${RESULTS_DIR}/${prompt_file%.txt}.md"
  tmp="${output}.tmp.$$"
  prompt_path="${PROMPTS_DIR}/${prompt_file}"

  if [[ ! -f "$prompt_path" ]]; then
    echo "ERROR: missing prompt $prompt_path" >&2
    return 1
  fi
  if [[ ! -d "$repo" ]]; then
    echo "ERROR: missing repo $repo" >&2
    return 1
  fi

  if [[ "$RESUME" == "1" ]] && output_is_valid "$output" "$task_id"; then
    lines="$(count_lines "$output")"
    echo "  SKIP (valid output exists, ${lines} lines)"
    log_json "{\"event\":\"skip\",\"task\":\"$task_id\",\"lines\":$lines,\"profile\":\"${AUDIT_PROFILE:-}\",\"ts\":\"$(timestamp)\"}"
    return 0
  fi

  if ! check_api "$API_HEALTH_URL"; then
    echo "  ERROR: inference API not reachable at $API_HEALTH_URL" >&2
    log_json "{\"event\":\"api_down\",\"task\":\"$task_id\",\"url\":\"$API_HEALTH_URL\",\"profile\":\"${AUDIT_PROFILE:-}\",\"ts\":\"$(timestamp)\"}"
    return 1
  fi

  local query preamble ctx_file
  if [[ "${AUDIT_MODE}" == "bundled" ]]; then
    preamble="$AUDIT_PREAMBLE_BUNDLED"
    ctx_file="${RESULTS_DIR}/.context/${task_id}.txt"
    echo "  bundling source context ..."
    "$SCRIPT_DIR/scripts/bundle_task_context.sh" "$task_id" "$repo" "$ctx_file"
    query="${preamble}$(cat "$prompt_path")

SOURCE FILES (read-only bundle — do not call tools):
$(cat "$ctx_file")"
  else
    preamble="$AUDIT_PREAMBLE_AGENT"
    query="${preamble}$(cat "$prompt_path")"
  fi

  hermes_cmd=(hermes chat -q "$query" --provider "$HERMES_PROVIDER" -m "$HERMES_MODEL")
  # shellcheck disable=SC2206
  [[ -n "$HERMES_EXTRA_ARGS" ]] && hermes_cmd+=($HERMES_EXTRA_ARGS)

  max_attempts=$((MAX_RETRIES + 1))

  while [[ "$attempt" -le "$max_attempts" ]]; do
    echo "  attempt $attempt/$max_attempts ..."
    started="$(timestamp)"
    rm -f "$tmp"
    t0=$(date +%s)

    (
      cd "$repo" || exit 1
      if [[ "$TASK_TIMEOUT_SEC" -gt 0 ]]; then
        timeout --signal=TERM "${TASK_TIMEOUT_SEC}s" "${hermes_cmd[@]}" >"$tmp" 2>&1
      else
        "${hermes_cmd[@]}" >"$tmp" 2>&1
      fi
    )
    exit_code=$?
    ended="$(timestamp)"
    duration_sec=$(( $(date +%s) - t0 ))
    lines="$(count_lines "$tmp")"

    if [[ "$exit_code" -eq 124 ]]; then
      echo "  TIMEOUT after ${TASK_TIMEOUT_SEC}s (${lines} lines captured)"
    elif [[ "$exit_code" -ne 0 ]]; then
      echo "  hermes exit code $exit_code (${lines} lines)"
    fi

    if output_is_valid "$tmp" "$task_id"; then
      mv -f "$tmp" "$output"
      echo "  OK → $output (${lines} lines)"
      log_json "{\"event\":\"ok\",\"task\":\"$task_id\",\"attempt\":$attempt,\"exit\":$exit_code,\"lines\":$lines,\"duration_sec\":$duration_sec,\"started\":\"$started\",\"ended\":\"$ended\",\"profile\":\"${AUDIT_PROFILE:-}\",\"provider\":\"$HERMES_PROVIDER\",\"model\":\"$HERMES_MODEL\"}"
      return 0
    fi

    reason="${VALIDATION_REASON:-unknown}"
    echo "  invalid output (${lines} lines, reason: $reason)"
    if [[ -f "$tmp" ]]; then
      echo "  --- last 8 lines of attempt ---"
      tail -8 "$tmp" | sed 's/^/    /'
      if [[ "$SAVE_PARTIAL" == "1" ]]; then
        partial="${RESULTS_DIR}/${task_id}.attempt${attempt}.md"
        mv -f "$tmp" "$partial"
        echo "  saved partial → $partial"
        # Keep longest attempt as .partial.md for review
        if [[ ! -f "${RESULTS_DIR}/${task_id}.partial.md" ]] || \
           [[ "$(count_lines "$partial")" -gt "$(count_lines "${RESULTS_DIR}/${task_id}.partial.md")" ]]; then
          cp -f "$partial" "${RESULTS_DIR}/${task_id}.partial.md"
        fi
      else
        rm -f "$tmp"
      fi
    fi

    log_json "{\"event\":\"retry\",\"task\":\"$task_id\",\"attempt\":$attempt,\"exit\":$exit_code,\"lines\":$lines,\"duration_sec\":$duration_sec,\"validation_reason\":\"$reason\",\"partial\":\"${SAVE_PARTIAL}\",\"started\":\"$started\",\"ended\":\"$ended\",\"profile\":\"${AUDIT_PROFILE:-}\"}"

    attempt=$((attempt + 1))
    if [[ "$attempt" -le "$max_attempts" ]]; then
      echo "  sleeping ${RETRY_SLEEP_SEC}s before retry ..."
      sleep "$RETRY_SLEEP_SEC"
    fi
  done

  echo "  FAILED after $max_attempts attempts"
  if [[ "$SAVE_PARTIAL" == "1" && -f "${RESULTS_DIR}/${task_id}.partial.md" ]]; then
    echo "  Best partial saved: ${RESULTS_DIR}/${task_id}.partial.md"
  fi
  log_json "{\"event\":\"fail\",\"task\":\"$task_id\",\"attempts\":$max_attempts,\"validation_reason\":\"$reason\",\"profile\":\"${AUDIT_PROFILE:-}\",\"ts\":\"$(timestamp)\"}"
  return 1
}

mkdir -p "$RESULTS_DIR" "$LOG_DIR"
acquire_lock

echo "Overnight audits — $(timestamp)"
[[ -n "$AUDIT_PROFILE" ]] && echo "  profile:  $AUDIT_PROFILE"
echo "  mode:     ${AUDIT_MODE:-bundled}"
echo "  provider: $HERMES_PROVIDER  model: $HERMES_MODEL"
echo "  API:      $API_HEALTH_URL"
echo "  results:  $RESULTS_DIR"
echo "  log:      $RUN_LOG"
echo ""

if [[ "$DRY_RUN" == "1" ]]; then
  echo "[dry-run] Would run $TOTAL tasks"
  idx=0
  for prompt_file in "${ORDER[@]}"; do
    idx=$((idx + 1))
    tid="$(task_id_for_prompt "$prompt_file")"
    out="${RESULTS_DIR}/${prompt_file%.txt}.md"
    if [[ "$RESUME" == "1" ]] && output_is_valid "$out" "$tid"; then
      st="skip (valid)"
    else
      st="run"
    fi
    echo "  [$idx/$TOTAL] $tid → $st"
  done
  exit 0
fi

if ! check_api "$API_HEALTH_URL"; then
  echo "ERROR: Inference API not up. Try:" >&2
  echo "  ./scripts/start_backend.sh --profile <name>" >&2
  echo "  ./scripts/smoke_test.sh --profile <name>" >&2
  exit 1
fi

ok=0
fail=0
skip=0
idx=0

for prompt_file in "${ORDER[@]}"; do
  idx=$((idx + 1))
  if [[ "$ONLY_TASK" -gt 0 && "$idx" -ne "$ONLY_TASK" ]]; then
    continue
  fi

  tid="$(task_id_for_prompt "$prompt_file")"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "[$idx/$TOTAL] $tid"
  echo "  repo: ${TASK_REPOS[$prompt_file]}"
  echo "  out:  ${RESULTS_DIR}/${prompt_file%.txt}.md"
  echo "  started: $(date)"

  if [[ "$RESUME" == "1" ]] && output_is_valid "${RESULTS_DIR}/${prompt_file%.txt}.md" "$tid"; then
    skip=$((skip + 1))
    echo "  SKIP (valid output exists)"
    continue
  fi

  if run_one_task "$prompt_file"; then
    ok=$((ok + 1))
  else
    fail=$((fail + 1))
    if [[ "$CONTINUE_ON_ERROR" -eq 0 ]]; then
      echo "  Stopping run after failure (use --continue-on-error to keep going)."
      break
    fi
    echo "  Continuing after failure (--continue-on-error)."
  fi
done

{
  echo "Run finished: $(timestamp)"
  echo "  ok=$ok  fail=$fail  skipped=$skip  total=$TOTAL"
  echo "  profile=${AUDIT_PROFILE:-default}"
  echo "  provider=$HERMES_PROVIDER model=$HERMES_MODEL"
} | tee "$SUMMARY_FILE"

echo ""
echo "Summary written to $SUMMARY_FILE"
echo "Structured log: $RUN_LOG"
echo "Results: $RESULTS_DIR"

if [[ "$fail" -gt 0 ]]; then
  exit 1
fi
