#!/bin/bash
# DEPRECATED for local overnight runs: Kanban workers often fail without
# kanban_complete, dispatch all tasks at once, and block on auto_decompose.
# Use ./run_audits.sh instead (sequential, resume, atomic writes).
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"

echo "WARNING: Prefer ./run_audits.sh for reliable local overnight audits."
echo "==> Creating overnight-audits board..."
hermes kanban boards create overnight-audits 2>/dev/null || true
hermes kanban boards switch overnight-audits

echo "==> Queueing 8 tasks..."

hermes kanban create "VideoBytesGo: fMP4 → progressive MP4 conversion bug" \
  --body "$(cat "$DIR/01_videobytes_fmp4.txt")" \
  --workspace "dir:/home/ivan/git/VideoBytesGo" \
  --max-runtime 2h

hermes kanban create "VideoBytesGo: buffer safety & RTSP ingest races" \
  --body "$(cat "$DIR/01b_videobytes_buffer_rtsp.txt")" \
  --workspace "dir:/home/ivan/git/VideoBytesGo" \
  --max-runtime 2h

hermes kanban create "factory-man: quota solver correctness audit" \
  --body "$(cat "$DIR/02_factoryman_scheduler.txt")" \
  --workspace "dir:/home/ivan/git/factory-man" \
  --max-runtime 2h

hermes kanban create "factory-man: drift detector & shift assignment audit" \
  --body "$(cat "$DIR/02b_factoryman_drift_shifts.txt")" \
  --workspace "dir:/home/ivan/git/factory-man" \
  --max-runtime 2h

hermes kanban create "ophanim: LPR aggregator & edge ingest concurrency" \
  --body "$(cat "$DIR/03_ophanim_lpr_ingest.txt")" \
  --workspace "dir:/home/ivan/git/ophanim" \
  --max-runtime 2h

hermes kanban create "ophanim: retention safety & auth security audit" \
  --body "$(cat "$DIR/03b_ophanim_retention_auth.txt")" \
  --workspace "dir:/home/ivan/git/ophanim" \
  --max-runtime 2h

hermes kanban create "liminal: Phase 2 whisper.cpp architecture design" \
  --body "$(cat "$DIR/04a_liminal_phase2_architecture.txt")" \
  --workspace "dir:/home/ivan/git/liminal" \
  --max-runtime 2h

hermes kanban create "liminal: server security & Flutter client review" \
  --body "$(cat "$DIR/04b_liminal_security_flutter.txt")" \
  --workspace "dir:/home/ivan/git/liminal" \
  --max-runtime 2h

echo ""
echo "==> Board loaded. Tasks:"
hermes kanban list
echo ""
echo "==> To start the dispatcher, run in a separate terminal:"
echo "    hermes gateway run"
echo ""
echo "==> To watch progress live:"
echo "    hermes kanban watch --board overnight-audits"
echo ""
echo "==> To read a completed task's output:"
echo "    hermes kanban log <task-id>"
