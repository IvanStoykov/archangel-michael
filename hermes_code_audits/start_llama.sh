#!/usr/bin/env bash
# Start llama-server for an audit profile (default: 27B atomic on 8080).
# Usage:
#   ./start_llama.sh                         # llama-qwen-27b-stable (port 8080)
#   ./start_llama.sh llama-qwen-27b-stable-nextn
#   ./start_llama.sh llama-qwen-35b-mtp          # 35B MoE draft-mtp havenoammo (8001)
#   ./start_llama.sh llama-qwen-35b-mtp-unsloth  # 35B MoE draft-mtp Unsloth GGUF (8001)
#   ./start_llama.sh llama-qwen-27b-q8
#   ./start_llama.sh --stop
#   NGL=10 ./start_llama.sh llama-qwen-27b-stable
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "${1:-}" == "--fit" ]]; then
  exec "$SCRIPT_DIR/scripts/llama-fit-qwen27.sh"
fi

PROFILE="llama-qwen-27b-stable"
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --stop|--foreground) ARGS+=("$arg") ;;
    llama-qwen-*) PROFILE="$arg" ;;
    *) ARGS+=("$arg") ;;
  esac
done

exec "$SCRIPT_DIR/scripts/start_backend.sh" --profile "$PROFILE" "${ARGS[@]}"
