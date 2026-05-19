#!/usr/bin/env bash
# Preflight for llama-gemma-26b: binary + GGUF paths.
set -euo pipefail

LLAMA_ROOT="${LLAMA_ROOT:-/home/ivan/git/atomic-llama-cpp-turboquant}"
SERVER="${LLAMA_ROOT}/build/bin/llama-server"
GGUF_A="${LLAMA_ROOT}/gemma-4-26b-a4b/gemma-4-26B-A4B-it-UD-Q4_K_XL.gguf"
GGUF_B="${LLAMA_ROOT}/.scratch/gemma-4-26b-a4b/gemma-4-26B-A4B-it-UD-Q4_K_XL.gguf"

ok=0
if [[ -x "$SERVER" ]]; then
  echo "OK  llama-server: $SERVER"
else
  echo "MISSING  llama-server — build: cmake --build build --target llama-server"
  ok=1
fi

if [[ -f "$GGUF_A" ]]; then
  echo "OK  GGUF: $GGUF_A"
elif [[ -f "$GGUF_B" ]]; then
  echo "OK  GGUF: $GGUF_B"
else
  echo "MISSING  Gemma 26B GGUF at:"
  echo "       $GGUF_A"
  echo "       $GGUF_B"
  echo "  Place gemma-4-26B-A4B-it-UD-Q4_K_XL.gguf in one of these paths."
  ok=1
fi

echo ""
echo "Start server (f16-base, 16K ctx):"
echo "  ./scripts/start_backend.sh --profile llama-gemma-26b"
echo "OOM on 12GB GPU: NGL=50 ./scripts/start_backend.sh --profile llama-gemma-26b"

exit "$ok"
