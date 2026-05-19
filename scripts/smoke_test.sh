#!/usr/bin/env bash
# Preflight: API health + short Hermes chat.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck source=lib/audit_common.sh
source "$SCRIPT_DIR/lib/audit_common.sh"

PROFILE=""
START_BACKEND=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="${2:?}"; shift 2 ;;
    --start-backend) START_BACKEND=1; shift ;;
    -h|--help)
      echo "Usage: $(basename "$0") --profile <name> [--start-backend]"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$PROFILE" ]] || { echo "ERROR: --profile required" >&2; exit 1; }

export AUDIT_ROOT
"$SCRIPT_DIR/apply_profile.sh" "$PROFILE"
# shellcheck source=/dev/null
source "$AUDIT_ROOT/audit.conf.active"

if [[ "$START_BACKEND" -eq 1 ]]; then
  "$SCRIPT_DIR/start_backend.sh" --profile "$PROFILE"
fi

echo "Checking API: $API_HEALTH_URL"
if ! check_api "$API_HEALTH_URL"; then
  echo "ERROR: API not reachable" >&2
  exit 1
fi
echo "API OK"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Hermes smoke chat ..."
if ! hermes chat -q "Reply with exactly: OK" --provider "$HERMES_PROVIDER" -m "$HERMES_MODEL" -Q >"$tmp" 2>&1; then
  echo "ERROR: hermes chat failed" >&2
  tail -20 "$tmp" >&2
  exit 1
fi

if grep -qE 'API call failed|Connection error|context_length_insufficient|requires at least [0-9]+ tokens' "$tmp"; then
  echo "ERROR: smoke output contains API/context errors:" >&2
  cat "$tmp" >&2
  exit 1
fi

if ! grep -qi 'OK' "$tmp"; then
  echo "WARNING: expected 'OK' in reply; output:" >&2
  tail -15 "$tmp" >&2
fi

echo "Smoke test passed for profile: $PROFILE"
