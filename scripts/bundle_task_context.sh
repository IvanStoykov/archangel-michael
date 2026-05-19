#!/usr/bin/env bash
# Gather read-only source excerpts for a task (no LLM tools required).
set -euo pipefail

TASK_ID="${1:?task id}"
REPO="${2:?repo path}"
OUT="${3:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MAX_BYTES="${BUNDLE_MAX_BYTES:-180000}"

declare -A TASK_PATTERNS=(
  [01_videobytes_fmp4]="fmp4|mp4|moov|mdat|moof|ftyp|ConvertFmp4|progressive"
  [01b_videobytes_buffer_rtsp]="buffer|rtsp|keyframe|segment|ingest"
  [02_factoryman_scheduler]="drift|scheduler|quota|state_machine|shift_assignment"
  [02b_factoryman_drift_shifts]="drift|shift|floater|AlertWriter|workorder"
  [03_ophanim_lpr_ingest]="lpr|ingest|batch|loiter|aggregator"
  [03b_ophanim_retention_auth]="retention|shotpoint|videobytes|auth|token"
  [04a_liminal_phase2_architecture]="websocket|audio|transcri|vad|whisper"
  [04b_liminal_security_flutter]="websocket|flutter|provider|security|cors"
)

pattern="${TASK_PATTERNS[$TASK_ID]:-}"
if [[ -z "$pattern" ]]; then
  echo "ERROR: unknown task id: $TASK_ID" >&2
  exit 1
fi

if [[ -z "$OUT" ]]; then
  OUT="${AUDIT_ROOT}/results/.context/${TASK_ID}.txt"
fi
mkdir -p "$(dirname "$OUT")"

{
  echo "=== BUNDLED SOURCE CONTEXT for ${TASK_ID} ==="
  echo "repo: ${REPO}"
  echo "pattern: ${pattern}"
  echo ""

  cd "$REPO" || exit 1

  mapfile -t files < <(
    rg -l -i -e "$pattern" --glob '*.go' --glob '*.dart' --glob '*.yaml' . 2>/dev/null | head -40
  )

  if [[ ${#files[@]} -eq 0 ]]; then
    mapfile -t files < <(find . -name '*.go' -type f 2>/dev/null | head -25)
  fi

  total=0
  for f in "${files[@]}"; do
    [[ -f "$f" ]] || continue
    size=$(wc -c <"$f" | tr -d ' ')
    if [[ $((total + size)) -gt $MAX_BYTES ]]; then
      echo ""
      echo "=== [truncated: ${MAX_BYTES} byte budget reached] ==="
      break
    fi
    echo ""
    echo "########## FILE: ${f} ##########"
    head -c "$((MAX_BYTES - total))" "$f"
    total=$((total + size))
  done
} >"$OUT"

bytes=$(wc -c <"$OUT" | tr -d ' ')
lines=$(wc -l <"$OUT" | tr -d ' ')
echo "Bundled ${bytes} bytes, ${lines} lines → $OUT"
