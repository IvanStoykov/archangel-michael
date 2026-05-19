#!/usr/bin/env bash
# Shared validation and profile helpers for overnight audits.

audit_script_dir() {
  local d
  d="$(cd "$(dirname "${BASH_SOURCE[1]}")/../.." && pwd)"
  echo "$d"
}

load_audit_profile() {
  local profile="${1:?profile name}"
  local root="${AUDIT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
  local prof_file="${root}/profiles/${profile}.conf"

  if [[ ! -f "$prof_file" ]]; then
    echo "ERROR: unknown profile '$profile' (missing $prof_file)" >&2
    return 1
  fi

  # shellcheck source=/dev/null
  source "$prof_file"
  export AUDIT_PROFILE="$profile"
  export HERMES_PROVIDER HERMES_MODEL API_HEALTH_URL HERMES_CONTEXT_LENGTH
  export HERMES_BASE_URL BACKEND_PORT BACKEND_START_CMD BACKEND_STOP_PATTERN
  export HERMES_EXTRA_ARGS MIN_OUTPUT_LINES TASK_TIMEOUT_SEC MAX_RETRIES RETRY_SLEEP_SEC RESUME
  export MAIN_GGUF_CHECK MAIN_GGUF LLAMA_MODE LLAMA_ROOT CTX NGL WAIT_SEC AUDIT_MODE
  export KV_CACHE LLAMA_FIT FIT_TARGET FIT_CTX_MIN FA UBATCH THREADS NO_WARMUP
  export NGL_DRAFT DRAFT_MAX DRAFT_MIN VERIFY_NEXTN_GGUF
  export LLAMA_UPSTREAM_ROOT MODEL_DIR HF_REPO SERVER_CTX SPEC_DRAFT_N_MAX
  return 0
}

count_lines() {
  local f="$1"
  if [[ -f "$f" ]]; then
    wc -l < "$f" | tr -d ' '
  else
    echo 0
  fi
}

# Sets VALIDATION_REASON on failure.
output_is_valid() {
  local f="$1"
  local task_id="${2:-}"
  local lines sections found min_sections=2 hcount

  VALIDATION_REASON=""

  [[ -f "$f" ]] || { VALIDATION_REASON="missing_file"; return 1; }
  [[ -s "$f" ]] || { VALIDATION_REASON="empty_file"; return 1; }

  lines="$(count_lines "$f")"
  if [[ "$lines" -lt "${MIN_OUTPUT_LINES:-200}" ]]; then
    VALIDATION_REASON="too_few_lines:${lines}"
    return 1
  fi

  if grep -qE 'API call failed|Connection error|context_length_insufficient|requires at least [0-9]+ tokens of context|below the minimum 64,?000 required by Hermes|invalid tool call|Model generated invalid tool' "$f"; then
    VALIDATION_REASON="api_error_in_output"
    return 1
  fi

  if grep -qE 'Worker exited|kanban_block|kanban_complete|hermes exit' "$f"; then
    VALIDATION_REASON="agent_crash_banner"
    return 1
  fi

  # Prompt echo: Query: at start without report headings
  if head -30 "$f" | grep -qE '^Query:'; then
    if ! grep -qE '^## |^\*\*[A-Za-z]' "$f"; then
      VALIDATION_REASON="prompt_echo_query_only"
      return 1
    fi
  fi

  if head -40 "$f" | grep -q 'DELIVERABLES:' && ! grep -qE '^## |^\*\*[A-Za-z].*\*\*:' "$f"; then
    VALIDATION_REASON="prompt_echo_deliverables_only"
    return 1
  fi

  # Per-task section markers (at least 2 of expected)
  if [[ -n "$task_id" && -n "${TASK_SECTIONS[$task_id]:-}" ]]; then
    sections="${TASK_SECTIONS[$task_id]}"
    found=0
    IFS='|' read -ra _secs <<< "$sections"
    for s in "${_secs[@]}"; do
      if grep -qi "$s" "$f"; then
        found=$((found + 1))
      fi
    done
    if [[ "$found" -lt "$min_sections" ]]; then
      VALIDATION_REASON="missing_sections:${found}/${min_sections}"
      return 1
    fi
  else
    # Generic fallback: at least 4 markdown headings
    hcount=$(grep -cE '^## ' "$f" 2>/dev/null || echo 0)
    if [[ "$hcount" -lt 4 ]]; then
      VALIDATION_REASON="too_few_headings:${hcount}"
      return 1
    fi
  fi

  return 0
}

check_api() {
  local url="${1:-$API_HEALTH_URL}"
  local base="${url%/v1/models}"
  base="${base%/models}"
  if curl -sf --max-time 15 "$url" >/dev/null 2>&1; then
    return 0
  fi
  if curl -sf --max-time 15 "${base}/v1/models" >/dev/null 2>&1; then
    return 0
  fi
  if curl -sf --max-time 15 "${base}/health" >/dev/null 2>&1; then
    return 0
  fi
  return 1
}
