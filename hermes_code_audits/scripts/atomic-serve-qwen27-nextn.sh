#!/usr/bin/env bash
# Qwen 27B + atomic NextN speculative decode (same GGUF as stable baseline).
#
# KV_CACHE: f16 (default) | q8_0 | turbo3 (lab, CTX<=16384)
# Tune: NGL=10 DRAFT_MAX=2 CTX=16384 ./start_llama.sh llama-qwen-27b-stable-nextn
set -euo pipefail

LLAMA_ROOT="${LLAMA_ROOT:-/home/ivan/git/atomic-llama-cpp-turboquant}"
SERVER="${LLAMA_SERVER:-${LLAMA_ROOT}/build/bin/llama-server}"
MAIN_GGUF="${MAIN_GGUF:-/home/ivan/models/Qwen3.6-27B-MTP/Qwen3.6-27B-Q4_K_M.gguf}"
# Optional; needs numpy in gguf-py env. Off by default for bench/audits.
VERIFY_NEXTN_GGUF="${VERIFY_NEXTN_GGUF:-0}"

CTX="${CTX:-32768}"
NGL="${NGL:-14}"
NGL_DRAFT="${NGL_DRAFT:-$NGL}"
DRAFT_MAX="${DRAFT_MAX:-2}"
DRAFT_MIN="${DRAFT_MIN:-0}"
HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-8080}"
FA="${FA:-on}"
KV_CACHE="${KV_CACHE:-f16}"
LLAMA_FIT="${LLAMA_FIT:-on}"
FIT_TARGET="${FIT_TARGET:-1536}"
FIT_CTX_MIN="${FIT_CTX_MIN:-4096}"
UBATCH="${UBATCH:-512}"
THREADS="${THREADS:-}"
NO_WARMUP="${NO_WARMUP:-0}"

case "$KV_CACHE" in
  f16)    CTK=f16;    CTV=f16 ;;
  q8_0)   CTK=q8_0;   CTV=q8_0 ;;
  turbo3) CTK=turbo3; CTV=turbo3 ;;
  *)
    echo "error: KV_CACHE must be f16, q8_0, or turbo3 (got: ${KV_CACHE})" >&2
    exit 1
    ;;
esac
CTKD="$CTK"
CTVD="$CTV"

if [[ "$KV_CACHE" == "turbo3" && "$CTX" -gt 16384 ]]; then
  echo "error: turbo3 KV with CTX>${CTX} is unsafe on 12GB — use CTX<=16384 or KV_CACHE=f16" >&2
  exit 1
fi

if [[ ! -x "$SERVER" ]]; then
  echo "error: build llama-server first:" >&2
  echo "  cd ${LLAMA_ROOT} && cmake --build build --target llama-server" >&2
  exit 1
fi
if [[ ! -f "$MAIN_GGUF" ]]; then
  echo "error: GGUF not found: ${MAIN_GGUF}" >&2
  exit 1
fi

if [[ "$VERIFY_NEXTN_GGUF" != "0" ]]; then
  if ! python3 "${LLAMA_ROOT}/scripts/verify-qwen36-nextn-gguf.py" "$MAIN_GGUF"; then
    echo "error: NextN GGUF verify failed (install numpy or set VERIFY_NEXTN_GGUF=0)" >&2
    exit 1
  fi
fi

ARGS=(
  -m "$MAIN_GGUF"
  -md "$MAIN_GGUF"
  -c "$CTX"
  -ngl "$NGL"
  -ngld "$NGL_DRAFT"
  -ctk "$CTK"
  -ctv "$CTV"
  -ctkd "$CTKD"
  -ctvd "$CTVD"
  -fa "$FA"
  --host "$HOST"
  --port "$PORT"
  --parallel 1
  -np 1
  --cont-batching
  --cache-prompt
  --metrics
  --slots
  -ub "$UBATCH"
  --spec-type nextn
  --draft-max "$DRAFT_MAX"
  --draft-min "$DRAFT_MIN"
)

if [[ -n "$THREADS" ]]; then
  ARGS+=(-t "$THREADS" -tb "$THREADS")
fi

if [[ "$LLAMA_FIT" == "on" ]]; then
  ARGS+=(-fit on -fitt "$FIT_TARGET" -fitc "$FIT_CTX_MIN")
else
  ARGS+=(-fit off)
fi

[[ "$NO_WARMUP" != "0" ]] && ARGS+=(--no-warmup)

echo "info: Qwen27 audit server (NextN)" >&2
echo "info: MAIN=${MAIN_GGUF}" >&2
echo "info: CTX=${CTX} NGL=${NGL} NGL_DRAFT=${NGL_DRAFT} DRAFT_MAX=${DRAFT_MAX} KV_CACHE=${KV_CACHE} FIT=${LLAMA_FIT}" >&2
echo "info: PORT=${HOST}:${PORT}" >&2

exec "$SERVER" "${ARGS[@]}" "$@"
