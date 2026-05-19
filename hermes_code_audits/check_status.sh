#!/usr/bin/env bash
# Quick status for overnight audit results.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="$SCRIPT_DIR/results"
LOG="$SCRIPT_DIR/logs/run.jsonl"
MANIFEST="$SCRIPT_DIR/tasks.manifest.yaml"

# shellcheck source=scripts/lib/audit_common.sh
source "$SCRIPT_DIR/scripts/lib/audit_common.sh"
# shellcheck source=scripts/lib/manifest.sh
source "$SCRIPT_DIR/scripts/lib/manifest.sh"

[[ -f "$SCRIPT_DIR/audit.conf.active" ]] && source "$SCRIPT_DIR/audit.conf.active"
[[ -f "$SCRIPT_DIR/audit.conf" ]] && source "$SCRIPT_DIR/audit.conf"
MIN_OUTPUT_LINES="${MIN_OUTPUT_LINES:-200}"

load_tasks_manifest "$MANIFEST" || exit 1

echo "Audit results ($(date))"
echo "  profile: ${AUDIT_PROFILE:-not set}"
echo "  min lines for OK: $MIN_OUTPUT_LINES"
echo ""

printf "%-32s %8s %s\n" "TASK" "LINES" "STATUS"
printf "%-32s %8s %s\n" "----" "-----" "------"

ok=0
bad=0
missing=0

for prompt_file in "${ORDER[@]}"; do
  base="${prompt_file%.txt}"
  tid="$(task_id_for_prompt "$prompt_file")"
  f="$RESULTS/${base}.md"
  partial_f="$RESULTS/${tid}.partial.md"
  if [[ ! -f "$f" ]]; then
    if [[ -f "$partial_f" ]]; then
      lines=$(count_lines "$partial_f")
      printf "%-32s %8s %s\n" "$tid" "$lines" "partial only (retry)"
      bad=$((bad + 1))
    else
      printf "%-32s %8s %s\n" "$tid" "-" "missing"
      missing=$((missing + 1))
    fi
  elif output_is_valid "$f" "$tid"; then
    lines=$(count_lines "$f")
    printf "%-32s %8s %s\n" "$tid" "$lines" "ok"
    ok=$((ok + 1))
  else
    lines=$(count_lines "$f")
    reason="${VALIDATION_REASON:-invalid}"
    printf "%-32s %8s %s\n" "$tid" "$lines" "invalid ($reason)"
    bad=$((bad + 1))
  fi
done

echo ""
echo "ok=$ok  invalid=$bad  missing=$missing  total=${#ORDER[@]}"

if [[ -f "$SCRIPT_DIR/results/_summary.txt" ]]; then
  echo ""
  echo "Last run summary:"
  cat "$SCRIPT_DIR/results/_summary.txt"
fi

if [[ -f "$LOG" ]]; then
  echo ""
  echo "Last 5 log events:"
  tail -5 "$LOG"
fi

if [[ -f "$RESULTS/.run_audits.lock" ]]; then
  if ! flock -n "$RESULTS/.run_audits.lock" true 2>/dev/null; then
    echo ""
    echo "NOTE: run_audits.sh may still be running (lock file held)."
  fi
fi
