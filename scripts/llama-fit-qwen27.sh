#!/usr/bin/env bash
# Print recommended llama-server args for Qwen 27B on this machine.
# Uses llama-fit-params if built; otherwise prints manual defaults.
set -euo pipefail

LLAMA_ROOT="${LLAMA_ROOT:-/home/ivan/git/atomic-llama-cpp-turboquant}"
FIT_BIN="${LLAMA_ROOT}/build/bin/llama-fit-params"
MAIN_GGUF="${MAIN_GGUF:-/home/ivan/models/Qwen3.6-27B-MTP/Qwen3.6-27B-Q4_K_M.gguf}"
CTX="${CTX:-32768}"

echo "=== Qwen 27B fit / tune (RTX 4070 12GB) ==="
echo "GGUF: ${MAIN_GGUF}"
echo ""

if [[ ! -f "$MAIN_GGUF" ]]; then
  echo "ERROR: GGUF not found" >&2
  exit 1
fi

if [[ -x "$FIT_BIN" ]]; then
  echo "Running llama-fit-params (may take 1–2 min) ..."
  echo ""
  # shellcheck disable=SC2046
  "$FIT_BIN" \
    --model "$MAIN_GGUF" \
    -c "$CTX" \
    -ctk f16 -ctv f16 \
    -fa on \
    -fit on \
    -fitt 1536 \
    -fitc 4096 \
    2>&1 | tee /dev/stderr | tail -5
  echo ""
  echo "Pipe the line above into llama-server, or set env from stable profile:"
  echo "  NGL=14 CTX=${CTX} KV_CACHE=f16 ./start_llama.sh"
else
  echo "llama-fit-params not built. Build with:"
  echo "  cd ${LLAMA_ROOT} && cmake --build build --target llama-fit-params"
  echo ""
  echo "Manual defaults (verified on this host):"
  echo "  export MAIN_GGUF=${MAIN_GGUF}"
  echo "  export CTX=${CTX}"
  echo "  export NGL=14          # OOM → try 10 or 8"
  echo "  export KV_CACHE=f16    # OOM at runtime → q8_0"
  echo "  export LLAMA_FIT=on"
  echo "  ./start_llama.sh"
  echo ""
  echo "If cudaMalloc OOM: pkill ollama; NGL=10 KV_CACHE=q8_0 CTX=16384 ./start_llama.sh"
fi
