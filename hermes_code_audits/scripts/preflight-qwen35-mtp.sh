#!/usr/bin/env bash
# One-time checks for llama-qwen-35b-mtp (upstream draft-mtp + havenoammo GGUF).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DO_DOWNLOAD=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --download) DO_DOWNLOAD=1; shift ;;
    -h|--help)
      echo "Usage: $(basename "$0") [--download]"
      echo "  Verifies upstream llama-server, havenoammo GGUF, then prints next steps."
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

export AUDIT_ROOT
# shellcheck source=lib/audit_common.sh
source "$SCRIPT_DIR/lib/audit_common.sh"
load_audit_profile llama-qwen-35b-mtp

SERVER="${LLAMA_UPSTREAM_ROOT}/build/bin/llama-server"
ok=0
fail=0

check() {
  if "$@"; then
    echo "  OK: $*"
    ok=$((ok + 1))
  else
    echo "  FAIL: $*"
    fail=$((fail + 1))
  fi
}

echo "Preflight: llama-qwen-35b-mtp"
echo "  HF:  ${HF_REPO}"
echo "  GGUF: ${MAIN_GGUF}"
echo "  API:  ${API_HEALTH_URL}"
echo ""

if [[ -x "$SERVER" ]]; then
  # Capture help first: grep -q + pipefail can false-fail with SIGPIPE (141).
  _help="$(timeout 8 "$SERVER" --help 2>&1 || true)"
  if [[ "$_help" == *draft-mtp* ]]; then
    echo "  OK: upstream llama-server has draft-mtp"
    ok=$((ok + 1))
  else
    echo "  FAIL: $SERVER missing draft-mtp — ./scripts/setup-upstream-llama-mtp.sh"
    fail=$((fail + 1))
  fi
else
  echo "  FAIL: missing $SERVER — ./scripts/setup-upstream-llama-mtp.sh"
  fail=$((fail + 1))
fi

if [[ -f "$MAIN_GGUF" ]]; then
  echo "  OK: GGUF present ($(du -h "$MAIN_GGUF" | cut -f1))"
  ok=$((ok + 1))
elif [[ $DO_DOWNLOAD -eq 1 ]]; then
  "$SCRIPT_DIR/download-qwen35-mtp-gguf.sh"
  [[ -f "$MAIN_GGUF" ]] && { echo "  OK: GGUF downloaded"; ok=$((ok + 1)); } || { echo "  FAIL: download"; fail=$((fail + 1)); }
else
  echo "  FAIL: missing $MAIN_GGUF"
  echo "        run: ./scripts/download-qwen35-mtp-gguf.sh"
  echo "        or:  $(basename "$0") --download"
  fail=$((fail + 1))
fi

echo ""
if [[ "$fail" -eq 0 ]]; then
  echo "Ready. Start server:"
  echo "  ./start_llama.sh llama-qwen-35b-mtp"
  echo "Run audits:"
  if [[ "${PROFILE_NAME:-}" == *unsloth* ]]; then
    echo "  ./run_audits.sh --profile llama-qwen-35b-mtp-unsloth --task 1"
  else
    echo "  ./run_audits.sh --profile llama-qwen-35b-mtp --task 1"
  fi
  echo "  Guide: docs/guides/qwen36-unsloth-mtp.md"
  exit 0
fi
echo "$fail check(s) failed."
exit 1
