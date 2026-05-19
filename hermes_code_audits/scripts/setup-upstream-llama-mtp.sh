#!/usr/bin/env bash
# Build upstream ggml-org/llama.cpp with CUDA for RTX 4070 (sm_89).
# Required for --spec-type draft-mtp (Qwen3.6 35B A3B MTP); not in atomic fork.
set -euo pipefail

ROOT="${LLAMA_UPSTREAM_ROOT:-/home/ivan/git/llama-cpp-upstream}"
BUILD="${ROOT}/build"

if [[ ! -d "${ROOT}/.git" ]]; then
  echo "Cloning llama.cpp into ${ROOT} ..."
  git clone --depth 1 https://github.com/ggml-org/llama.cpp "$ROOT"
fi

mkdir -p "$BUILD"
cd "$BUILD"

cmake .. \
  -DCMAKE_BUILD_TYPE=Release \
  -DGGML_CUDA=ON \
  -DLLAMA_CURL=ON \
  -DGGML_NATIVE=ON \
  -DGGML_CUDA_GRAPHS=ON \
  -DGGML_CUDA_F16=ON \
  -DGGML_CUDA_FA_ALL_QUANTS=ON \
  -DCMAKE_CUDA_ARCHITECTURES=89

cmake --build . --config Release \
  --target llama-server llama-bench \
  --parallel "$(nproc)"

SERVER="${BUILD}/bin/llama-server"
_help="$("$SERVER" --help 2>&1 || true)"
if [[ "$_help" != *draft-mtp* ]]; then
  echo "error: ${SERVER} lacks --spec-type draft-mtp; pull newer llama.cpp (MTP merged ~May 2026)" >&2
  exit 1
fi

echo "OK: ${SERVER}"
