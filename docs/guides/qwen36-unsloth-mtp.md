# Qwen3.6 MTP — Unsloth guide notes (for audits)

Sources:

- [Unsloth Qwen3.6 — MTP guide](https://unsloth.ai/docs/models/qwen3.6#mtp-guide)
- [Unsloth 35B-A3B MTP GGUF](https://huggingface.co/unsloth/Qwen3.6-35B-A3B-MTP-GGUF)
- [havenoammo 35B-A3B MTP GGUF](https://huggingface.co/havenoammo/Qwen3.6-35B-A3B-MTP-GGUF) (Reddit thread quant)

This repo keeps **27B on port 8080** (atomic) and **35B MTP on port 8001** (upstream `draft-mtp`) as separate profiles.

**Narrative tutorial (~10× speed, bigger model):** [../tutorials/10x-speed-with-a-bigger-model.md](../tutorials/10x-speed-with-a-bigger-model.md)

## What MTP is

Multi-Token Prediction: the model drafts several future tokens, then the main model verifies them in one pass. llama.cpp flag (May 2026+):

```bash
--spec-type draft-mtp --spec-draft-n-max N
```

(Old name `--spec-type mtp` was renamed to `draft-mtp` in llama.cpp on **May 13, 2026**.)

Upstream merged further MTP speedups around **May 17, 2026** (~1.8× → ~2× vs baseline in their notes).

## Hardware (total RAM + VRAM)

| Model | 4-bit MTP (approx) |
|-------|-------------------|
| 27B | 19 GB |
| 35B-A3B | 24 GB (+ ~1 GB headroom for MTP vs non-MTP) |

RTX 4070 12 GB: use **q8_0 KV**, `--fit`, and **draft-n-max 2** for MoE. Unsloth’s lab uses `draft-n-max 6` on large GPUs; tune **1–6** on your hardware. For MoE they explicitly recommend **≤2** draft tokens: acceptance drops from ~83% to ~50% at 4 drafts, so extra forwards stop paying off.

MTP GGUFs need ~**1 GB** extra RAM/VRAM headroom vs non-MTP quants.

## Model repos

| Repo | Profile |
|------|---------|
| [havenoammo/Qwen3.6-35B-A3B-MTP-GGUF](https://huggingface.co/havenoammo/Qwen3.6-35B-A3B-MTP-GGUF) | `llama-qwen-35b-mtp` |
| [unsloth/Qwen3.6-35B-A3B-MTP-GGUF](https://huggingface.co/unsloth/Qwen3.6-35B-A3B-MTP-GGUF) | `llama-qwen-35b-mtp-unsloth` |

Must be an **MTP** GGUF (combined main + MTP head). Plain non-MTP quants will not accelerate with `draft-mtp`.

## Sampling (from Unsloth)

**Agentic / code audits** (thinking, precise coding):

| Parameter | Value |
|-----------|--------|
| temperature | **0.6** |
| top_p | 0.95 |
| top_k | 20 |
| min_p | 0.0 |
| presence_penalty | **0.0** |
| repeat_penalty | 1.0 |
| chat-template | `{"preserve_thinking": true}` |

**General chat** (thinking): temp 1.0, presence_penalty 1.5.

**Instruct (no thinking):** `--chat-template-kwargs '{"enable_thinking":false}'`, temp 0.7, top_p 0.8.

Hermes overnight audits use **thinking + preserve_thinking** via `scripts/serve-qwen35-draft-mtp.sh` defaults.

## Vision / mmproj

Unsloth’s multimodal **non-MTP** 35B downloads include `mmproj-F16.gguf` and pass `--mmproj` to `llama-cli`. **MTP** repos in this repo use combined text MTP GGUFs for audits (no mmproj in `serve-qwen35-draft-mtp.sh`). If you add vision later, follow Unsloth’s `hf download … --include "*mmproj-F16*"` pattern.

## Do not use

- **Ollama** for Qwen3.6 GGUF (vision/mmproj split; use llama.cpp) — same as Unsloth warning.
- **CUDA 13.2** (reported gibberish; use older CUDA if you hit this).
- Plain (non-MTP) quants with `--spec-type draft-mtp` — no speedup; you need an **MTP** GGUF.

## Throughput expectations (Unsloth benchmarks)

Marketing / lab numbers (RTX 6000 class): 27B ~160 tok/s, 35B-A3B ~220 tok/s with MTP.

On **RTX 4070 12 GB**, expect much less unless the model is fully GPU-resident. Your Cursor UI ~65 tok/s on 35B-A3B is plausible; 27B partial-GPU ~7 tok/s is a different stack.

| Model type | MTP speedup (Unsloth) | Lab decode (RTX 6000 class) |
|------------|----------------------|-----------------------------|
| Dense 27B | ~**1.4–2×** | ~160 tok/s |
| MoE 35B-A3B | ~**1.15–1.25×** | ~220–240 tok/s |

Overall claim: **~1.4–2.2×** faster generation with no accuracy loss vs non-MTP, when the stack is GPU-resident.

## Commands (this repo)

```bash
# One-time
./scripts/setup-upstream-llama-mtp.sh
./scripts/download-qwen35-mtp-gguf.sh              # havenoammo
# or: HF_REPO=unsloth/Qwen3.6-35B-A3B-MTP-GGUF ./scripts/download-qwen35-mtp-gguf.sh

./scripts/preflight-qwen35-mtp.sh
./start_llama.sh llama-qwen-35b-mtp                 # port 8001
./scripts/bench_decode_35b.sh
./run_audits.sh --profile llama-qwen-35b-mtp --task 1
```

Tune draft depth on your machine:

```bash
SPEC_DRAFT_N_MAX=2 ./scripts/bench_decode_35b.sh   # start here on 12 GB
SPEC_DRAFT_N_MAX=4 ./scripts/bench_decode_35b.sh   # only if acceptance stays high
```
