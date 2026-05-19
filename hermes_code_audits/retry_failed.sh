#!/usr/bin/env bash
# Re-run only tasks whose .md output is missing or invalid.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="$SCRIPT_DIR/results"
MANIFEST="$SCRIPT_DIR/tasks.manifest.yaml"

# shellcheck source=scripts/lib/audit_common.sh
source "$SCRIPT_DIR/scripts/lib/audit_common.sh"
# shellcheck source=scripts/lib/manifest.sh
source "$SCRIPT_DIR/scripts/lib/manifest.sh"

[[ -f "$SCRIPT_DIR/audit.conf.active" ]] && source "$SCRIPT_DIR/audit.conf.active"
[[ -f "$SCRIPT_DIR/audit.conf" ]] && source "$SCRIPT_DIR/audit.conf"

load_tasks_manifest "$MANIFEST" || exit 1

removed=0
for prompt_file in "${ORDER[@]}"; do
  tid="$(task_id_for_prompt "$prompt_file")"
  f="$RESULTS/${tid}.md"
  [[ -f "$f" ]] || continue
  if ! output_is_valid "$f" "$tid"; then
    echo "Removing invalid: $f (${VALIDATION_REASON:-})"
    rm -f "$f"
    removed=$((removed + 1))
  fi
done

echo "Removed $removed invalid result(s). Starting run_audits.sh ..."
exec "$SCRIPT_DIR/run_audits.sh" "$@"
