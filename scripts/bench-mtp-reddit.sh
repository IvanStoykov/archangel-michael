#!/usr/bin/env bash
# Run am17an mtp-bench.py against upstream draft-mtp server (Reddit repro).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BENCH_PY="${BENCH_PY:-${AUDIT_ROOT}/.cache/mtp-bench.py}"
PORT="${PORT:-8001}"
# mtp-bench.py uses native POST {url}/completion (not OpenAI /v1)
BENCH_URL="${BENCH_URL:-http://127.0.0.1:${PORT}}"

if [[ ! -f "$BENCH_PY" ]]; then
  mkdir -p "$(dirname "$BENCH_PY")"
  curl -fsSL -o "$BENCH_PY" \
    https://gist.githubusercontent.com/am17an/228edfb84ed082aa88e3865d6fa27090/raw/7a2cee40ee1e2ca5365f4cef93632193d7ad852a/mtp-bench.py
fi

if ! curl -sf "${BENCH_URL}/health" >/dev/null; then
  echo "error: no API at ${BENCH_URL} — start server first:" >&2
  echo "  ./start_llama.sh llama-qwen-35b-mtp" >&2
  exit 1
fi

echo "Running mtp-bench.py -> ${BENCH_URL}"
python3 "$BENCH_PY" --url "$BENCH_URL" "$@"
