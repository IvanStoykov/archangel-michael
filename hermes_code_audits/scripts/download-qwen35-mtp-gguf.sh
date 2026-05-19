#!/usr/bin/env bash
# Download Qwen3.6-35B-A3B MTP GGUF for draft-mtp profiles.
#
# Default (Reddit / havenoammo):
#   ./scripts/download-qwen35-mtp-gguf.sh
# Unsloth UD quant:
#   HF_REPO=unsloth/Qwen3.6-35B-A3B-MTP-GGUF \
#   MODEL_DIR=/home/ivan/models/Qwen3.6-35B-A3B-MTP-unsloth \
#   HF_PATTERN='*UD-Q4_K_XL*' \
#   ./scripts/download-qwen35-mtp-gguf.sh
#
# Requires one of: `hf download`, `huggingface-cli download`, or Python huggingface_hub.
# See: https://unsloth.ai/docs/models/qwen3.6#mtp-guide
set -euo pipefail

MODEL_DIR="${MODEL_DIR:-/home/ivan/models/Qwen3.6-35B-A3B-MTP}"
REPO="${HF_REPO:-havenoammo/Qwen3.6-35B-A3B-MTP-GGUF}"
PATTERN="${HF_PATTERN:-*UD-Q4_K_XL*}"

mkdir -p "$MODEL_DIR"

pick_python() {
  if [[ -n "${PYTHON:-}" ]]; then
    echo "$PYTHON"
    return
  fi
  local c
  for c in python3 python; do
    if command -v "$c" >/dev/null 2>&1; then
      echo "$c"
      return
    fi
  done
  echo ""
}

hf_cli_download() {
  hf download "$REPO" \
    --include "$PATTERN" \
    --local-dir "$MODEL_DIR"
}

huggingface_cli_download() {
  huggingface-cli download "$REPO" \
    --include "$PATTERN" \
    --local-dir "$MODEL_DIR"
}

python_snapshot_download() {
  local py="$1"
  REPO="$REPO" MODEL_DIR="$MODEL_DIR" PATTERN="$PATTERN" "$py" <<'PY'
import os
import sys

try:
    from huggingface_hub import snapshot_download
except ImportError:
    print("error: huggingface_hub not installed for this Python", file=sys.stderr)
    print("  pip install -U 'huggingface_hub[cli]'", file=sys.stderr)
    sys.exit(1)

repo = os.environ["REPO"]
local_dir = os.environ["MODEL_DIR"]
pattern = os.environ["PATTERN"]

kwargs = {
    "repo_id": repo,
    "local_dir": local_dir,
    "allow_patterns": [pattern],
}
# local_dir_use_symlinks removed in hub >= 0.23
try:
    snapshot_download(**kwargs, local_dir_use_symlinks=False)
except TypeError:
    snapshot_download(**kwargs)

print(f"OK: snapshot_download -> {local_dir}")
PY
}

can_hf() {
  command -v hf >/dev/null 2>&1 && hf download --help >/dev/null 2>&1
}

can_huggingface_cli() {
  command -v huggingface-cli >/dev/null 2>&1 \
    && huggingface-cli download --help >/dev/null 2>&1
}

can_python_hub() {
  local py="$1"
  [[ -n "$py" ]] && "$py" -c "from huggingface_hub import snapshot_download" >/dev/null 2>&1
}

echo "Downloading ${REPO} (${PATTERN}) -> ${MODEL_DIR}"

if can_hf; then
  echo "Using: hf download"
  hf_cli_download
elif can_huggingface_cli; then
  echo "Using: huggingface-cli download"
  huggingface_cli_download
else
  PY="$(pick_python)"
  if can_python_hub "$PY"; then
    echo "Using: ${PY} (huggingface_hub.snapshot_download)"
    python_snapshot_download "$PY"
  else
    echo "error: no working Hugging Face downloader found." >&2
    echo "" >&2
    echo "Your huggingface-cli may be too old (needs a 'download' subcommand)." >&2
    echo "Install or upgrade, then re-run:" >&2
    echo "  pip install -U 'huggingface_hub[cli]'" >&2
    echo "  # optional faster Hub downloads (Xet):" >&2
    echo "  export HF_XET_HIGH_PERFORMANCE=1" >&2
    echo "  # optional: HF_TOKEN from https://huggingface.co/settings/tokens" >&2
    exit 1
  fi
fi

ls -lh "$MODEL_DIR"/*.gguf 2>/dev/null || ls -lh "$MODEL_DIR"
echo ""
echo "Next:"
echo "  ./scripts/preflight-qwen35-mtp.sh"
if [[ "$REPO" == unsloth/* ]]; then
  echo "  ./start_llama.sh llama-qwen-35b-mtp-unsloth"
  echo "  ./run_audits.sh --profile llama-qwen-35b-mtp-unsloth --task 1"
else
  echo "  ./start_llama.sh llama-qwen-35b-mtp"
  echo "  ./run_audits.sh --profile llama-qwen-35b-mtp --task 1"
fi
echo "  Guide: docs/guides/qwen36-unsloth-mtp.md"
