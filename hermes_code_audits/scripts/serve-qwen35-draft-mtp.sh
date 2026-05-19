#!/usr/bin/env bash
# Upstream draft-mtp server for llama-qwen-35b-mtp* profiles (port 8001).
# Unsloth MTP guide: https://unsloth.ai/docs/models/qwen3.6#mtp-guide
#   Agentic/coding: temp 0.6, preserve_thinking, draft-n-max 2 on 12GB MoE
# Requires: ./scripts/setup-upstream-llama-mtp.sh && download script for your HF repo
#
# Tune headroom if dGPU drives your monitor (Reddit author used iGPU for display):
#   FIT_TARGET=2048 ./scripts/serve-qwen35-draft-mtp.sh
#   PORT=8001 ./scripts/serve-qwen35-draft-mtp.sh
set -euo pipefail

LLAMA_ROOT="${LLAMA_UPSTREAM_ROOT:-/home/ivan/git/llama-cpp-upstream}"
SERVER="${LLAMA_SERVER:-${LLAMA_ROOT}/build/bin/llama-server}"

# havenoammo UD quant (Reddit); unsloth UD-Q4_K_XL also works — set MAIN_GGUF
MODEL_DIR="${MODEL_DIR:-/home/ivan/models/Qwen3.6-35B-A3B-MTP}"
MAIN_GGUF="${MAIN_GGUF:-${MODEL_DIR}/Qwen3.6-35B-A3B-MTP-UD-Q4_K_XL.gguf}"

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-8001}"
# 128K context (model supports 256K train). CACHE_RAM=0 avoids ~1GB prompt-cache OOM on 12GB.
CTX="${CTX:-131072}"
CACHE_RAM="${CACHE_RAM:-0}"
N_PREDICT="${N_PREDICT:-32768}"
FIT_TARGET="${FIT_TARGET:-1536}"
FIT_CTX_MIN="${FIT_CTX_MIN:-4096}"
SPEC_DRAFT_N_MAX="${SPEC_DRAFT_N_MAX:-2}"
# Unsloth: try 1–6 on big GPUs; MoE on 12GB — stay at 2 (acceptance drops above ~2)
CTX_CHECKPOINTS="${CTX_CHECKPOINTS:-32}"
UBATCH="${UBATCH:-512}"
BATCH="${BATCH:-1024}"
THREADS="${THREADS:-10}"
THREADS_BATCH="${THREADS_BATCH:-12}"
# Audit / agentic coding (Unsloth "precise coding" thinking mode)
TEMP="${TEMP:-0.6}"
TOP_P="${TOP_P:-0.95}"
TOP_K="${TOP_K:-20}"
MIN_P="${MIN_P:-0.0}"
PRESENCE_PENALTY="${PRESENCE_PENALTY:-0.0}"
REPEAT_PENALTY="${REPEAT_PENALTY:-1.0}"
CHAT_TEMPLATE_KWARGS="${CHAT_TEMPLATE_KWARGS:-{\"preserve_thinking\": true}}"

if [[ ! -x "$SERVER" ]]; then
  echo "error: build upstream first: ./scripts/setup-upstream-llama-mtp.sh" >&2
  exit 1
fi
if [[ ! -f "$MAIN_GGUF" ]]; then
  echo "error: GGUF not found: ${MAIN_GGUF}" >&2
  echo "  ./scripts/download-qwen35-mtp-gguf.sh" >&2
  exit 1
fi

ARGS=(
  -m "$MAIN_GGUF"
  -fit on
  -fitt "$FIT_TARGET"
  -fitc "$FIT_CTX_MIN"
  -c "$CTX"
  -n "$N_PREDICT"
  -fa on
  -np 1
  -ctk q8_0
  -ctv q8_0
  -ctkd q8_0
  -ctvd q8_0
  -ctxcp "$CTX_CHECKPOINTS"
  --no-mmap
  --no-warmup
  --cache-ram "$CACHE_RAM"
  --spec-type draft-mtp
  --spec-draft-n-max "$SPEC_DRAFT_N_MAX"
  --chat-template-kwargs "$CHAT_TEMPLATE_KWARGS"
  --temp "$TEMP"
  --top-p "$TOP_P"
  --top-k "$TOP_K"
  --min-p "$MIN_P"
  --presence-penalty "$PRESENCE_PENALTY"
  --repeat-penalty "$REPEAT_PENALTY"
  --host "$HOST"
  --port "$PORT"
  -ub "$UBATCH"
  -b "$BATCH"
  -t "$THREADS"
  -tb "$THREADS_BATCH"
)

echo "info: Qwen3.6 35B A3B draft-mtp (Reddit-style)" >&2
echo "info: SERVER=${SERVER}" >&2
echo "info: MAIN=${MAIN_GGUF}" >&2
echo "info: CTX=${CTX} CACHE_RAM=${CACHE_RAM} FIT_TARGET=${FIT_TARGET} SPEC_DRAFT_N_MAX=${SPEC_DRAFT_N_MAX} TEMP=${TEMP} PORT=${PORT}" >&2

exec "$SERVER" "${ARGS[@]}" "$@"
