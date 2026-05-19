# Hermes overnight code audits (atomic-llama-cpp)

Sequential static audits using **atomic-llama-cpp-turboquant** `llama-server` + Hermes file tools.

**Ollama is not used** for audits (broken tool calling on Gemma).

## Quick start

```bash
cd /home/ivan/git/hermes-overnight-audits

# Optional: tune VRAM for your GPU
./start_llama.sh --fit          # prints llama-fit-params or manual defaults

# Terminal 1 — start server (wait for "API ready", ~1–3 min load)
./start_llama.sh

# Terminal 2
./scripts/apply_profile.sh llama-qwen-27b-stable
./scripts/smoke_test.sh --profile llama-qwen-27b-stable
./run_audits.sh --profile llama-qwen-27b-stable --task 1
./run_audits.sh --profile llama-qwen-27b-stable
```

All-in-one: `./run_with_llama.sh --task 1`

## Stable server (RTX 4070 12 GB)

| Setting | Default | Notes |
|---------|---------|--------|
| Model | `Qwen3.6-27B-Q4_K_M.gguf` | `/home/ivan/models/Qwen3.6-27B-MTP/` |
| **KV_CACHE** | **f16** | Use **q8_0** profile if runtime OOM |
| **CTX** | **32768** | Never **131072**; 8192 too small for Hermes tools |
| **NGL** | **14** | Partial GPU; OOM → `NGL=10` or `8` |
| **--fit** | **on** | Auto-tune ctx/layers to free VRAM (`FIT_TARGET=1536` MiB headroom) |
| Port | **8080** | |

Script: [`scripts/atomic-serve-qwen27-stable.sh`](scripts/atomic-serve-qwen27-stable.sh)  
NextN: [`scripts/atomic-serve-qwen27-nextn.sh`](scripts/atomic-serve-qwen27-nextn.sh) · profile `llama-qwen-27b-stable-nextn`

Hermes: `model.context_length=65536` (Agent minimum 64K; server `CTX=32768` is the real cap — Hermes steps down on `exceed_context_size` errors), `compression.enabled=false`.

### Decode benchmark (baseline vs NextN)

```bash
./scripts/bench_decode.sh              # restarts server twice; ~10–15 min on 4070
NGL=14 CTX=8192 ./scripts/bench_decode.sh
./start_llama.sh llama-qwen-27b-stable-nextn   # use NextN for audits if bench wins
./run_audits.sh --profile llama-qwen-27b-stable-nextn --task 1
```

## Profiles (27B atomic — port **8080**)

| Profile | KV | CTX | Use |
|---------|----|-----|-----|
| **`llama-qwen-27b-stable`** | f16 | 32768 | **Default 27B audits** |
| `llama-qwen-27b-stable-nextn` | f16 | 32768 | 27B + atomic NextN (~7 tok/s decode on partial GPU) |
| `llama-qwen-27b-q8` | q8_0 | 32768 | If f16 OOMs during long agent runs |
| `llama-qwen-27b-turbo3-lab` | turbo3 | 8192 max | Speed experiments only — not audits |
| `llama-gemma-26b` | f16 | 8192 | Needs Gemma GGUF on disk |

## Profile: 35B MoE + draft-mtp (port **8001**, separate stack)

Uses **upstream** [ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) `--spec-type draft-mtp`. Does **not** replace or stop 27B profiles on 8080.

Unsloth MTP guide (sampling, draft depth, Ollama warning): [docs/guides/qwen36-unsloth-mtp.md](docs/guides/qwen36-unsloth-mtp.md) · [Unsloth docs](https://unsloth.ai/docs/models/qwen3.6#mtp-guide)

```bash
# One-time
./scripts/setup-upstream-llama-mtp.sh
./scripts/download-qwen35-mtp-gguf.sh    # ~22GB Q4_K_XL
./scripts/preflight-qwen35-mtp.sh

# Server (port 8001)
./start_llama.sh llama-qwen-35b-mtp

# Bench / audits
./scripts/bench_decode_35b.sh
./run_audits.sh --profile llama-qwen-35b-mtp --task 1
```

| Profile | Port | HF repo | GGUF |
|---------|------|---------|------|
| **`llama-qwen-35b-mtp`** | 8001 | [havenoammo](https://huggingface.co/havenoammo/Qwen3.6-35B-A3B-MTP-GGUF) | `Qwen3.6-35B-A3B-MTP-UD-Q4_K_XL.gguf` |
| `llama-qwen-35b-mtp-unsloth` | 8001 | [unsloth](https://huggingface.co/unsloth/Qwen3.6-35B-A3B-MTP-GGUF) | `Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf` |

OOM at load: `FIT_TARGET=2048 ./start_llama.sh llama-qwen-35b-mtp` or use `MAIN_GGUF_ALT` (Q3_K_XL) in the profile.

35B audits use **`SERVER_CTX=131072` (128K)** + `compression.enabled=true` (threshold 0.85). Prompt cache is off (`CACHE_RAM=0`) to reduce OOM. If load fails: `CTX=98304 ./start_llama.sh llama-qwen-35b-mtp` or Q3 quant.

### VideoBytes function marathon (all night)

Seven package-level audits (~62 Go files), each report is function-by-function tables + findings:

```bash
./start_llama.sh llama-qwen-35b-mtp          # keep running
./scripts/run_videobytes_marathon.sh --dry-run
./scripts/run_videobytes_marathon.sh --continue-on-error   # tmux-friendly
```

Outputs: `results/vb_audit_{core,codecs,drivers,endpoints,utils,grpcapi,vms}.md`

Deprecated: `ollama-*`, `vllm-*`.

## Tuning ladder (OOM or crash)

```bash
pkill ollama   # free VRAM if needed

# 1) See what fits (build fit-params once)
cd ~/git/atomic-llama-cpp-turboquant
cmake --build build --target llama-fit-params
~/git/hermes-overnight-audits/scripts/llama-fit-qwen27.sh

# 2) Lower GPU layers
NGL=10 ./start_llama.sh

# 3) Smaller KV cache (less VRAM, small quality tradeoff)
./start_llama.sh llama-qwen-27b-q8

# 4) Smaller context
CTX=16384 ./start_llama.sh

# 5) Lab only — turbo3, never above 16K ctx
./start_llama.sh llama-qwen-27b-turbo3-lab
```

**Never:** `CTX=131072` + `turbo3` (CUDA crash on this card).

## Environment variables (passed to serve script)

| Var | Default | Purpose |
|-----|---------|---------|
| `NGL` | 14 | GPU layers |
| `CTX` | 32768 | Server context |
| `KV_CACHE` | f16 | `f16`, `q8_0`, or `turbo3` |
| `LLAMA_FIT` | on | Built-in `--fit` auto memory tuning |
| `FIT_TARGET` | 1536 | MiB VRAM headroom for `--fit` |
| `NO_WARMUP` | 0 | Set `1` for faster first API ready |

## tmux

```bash
tmux new -s audits
./run_with_llama.sh
# Ctrl+B, D
```

## Troubleshooting

| Problem | Fix |
|---------|-----|
| cudaMalloc OOM at load | `NGL=10 ./start_llama.sh` or `pkill ollama` |
| OOM during audit | `llama-qwen-27b-q8` profile |
| `below the minimum 64,000 required by Hermes` | Re-run `./scripts/apply_profile.sh llama-qwen-27b-stable-nextn` (config must be **≥65536**, not 30720) |
| `exceed_context_size` / audit stalls mid-task | Server `CTX=32768`; Hermes will step down after 400. Re-apply profile; avoid huge tool dumps in one turn |
| Short partial output, fake `<search_files>` XML | Old **Ollama/Gemma** run — use `llama-qwen-27b-stable`, not `ollama-*` |
| Low decode tok/s (~2–4) | Partial GPU (`NGL=14`); try `NGL=20` or `./scripts/bench_decode.sh` for NextN |
| NextN never starts / 300s wait | `verify-qwen36-nextn-gguf.py` needs **numpy** — use `VERIFY_NEXTN_GGUF=0` (default in bench) or `pip install numpy` |
| NextN OOM on start | Lower `CTX`, `NGL=10`, or `llama-qwen-27b-q8` profile |
| Hermes compression timeout (6+ min) | Same context mismatch; fix profile then restart audit |
| CUDA crash | Drop turbo3 / 131K ctx; use stable profile |
| Stale lock | `rm -f results/.run_audits.lock` |

**Stop server:** `./start_llama.sh --stop`

## Files

- `results/*.md` — valid outputs
- `results/*.partial.md` — failed attempt captures
- `logs/backend.log` — llama-server
- `logs/run.jsonl` — audit events
