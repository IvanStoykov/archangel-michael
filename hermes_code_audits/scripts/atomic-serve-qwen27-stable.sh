#!/usr/bin/env bash
# Stable llama-server for overnight audits on RTX 4070 12 GB.
#
# KV_CACHE: f16 (default) | q8_0 (~50% less KV VRAM) | turbo3 (lab only, max CTX 16384)
# Tune: NGL=10 CTX=16384 KV_CACHE=q8_0 ./start_llama.sh
#
# Built-in --fit on (default) auto-adjusts if VRAM is tight; set LLAMA_FIT=off to use fixed NGL/CTX.
set -euo pipefail

LLAMA_ROOT="${LLAMA_ROOT:-/home/ivan/git/atomic-llama-cpp-turboquant}"
SERVER="${LLAMA_SERVER:-${LLAMA_ROOT}/build/bin/llama-server}"
MAIN_GGUF="${MAIN_GGUF:-/home/ivan/models/Qwen3.6-27B-MTP/Qwen3.6-27B-Q4_K_M.gguf}"

CTX="${CTX:-32768}"
NGL="${NGL:-14}"
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

# turbo3 + large ctx has crashed CUDA on RTX 4070 (see turboquant FA dispatch issues)
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

ARGS=(
  -m "$MAIN_GGUF"
  -c "$CTX"
  -ngl "$NGL"
  -ctk "$CTK"
  -ctv "$CTV"
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

echo "info: Qwen27 audit server (stable)" >&2
echo "info: MAIN=${MAIN_GGUF}" >&2
echo "info: CTX=${CTX} NGL=${NGL} KV_CACHE=${KV_CACHE} FA=${FA} FIT=${LLAMA_FIT}" >&2
echo "info: PORT=${HOST}:${PORT}" >&2

exec "$SERVER" "${ARGS[@]}" "$@"
