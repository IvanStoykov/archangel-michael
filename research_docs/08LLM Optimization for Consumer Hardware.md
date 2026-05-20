# **High-Performance Inference Optimization for Qwen 3.6 MoE and Gemma 4 Architectures on Hybrid Core Hardware**

Executing contemporary state-of-the-art large language models on consumer-grade hardware requires a comprehensive understanding of both model architecture and underlying hardware constraints.1 The hardware platform under evaluation consists of an NVIDIA GeForce RTX 4070 GPU with 12GB of VRAM 3, an Intel Core i7-13700KF hybrid processor 4, and 32GB of system RAM 5 operating under a Linux environment.3  
Deploying complex models like the Qwen 3.6 35B-A3B MoE (which features 35 billion total and 3 billion active parameters) 1 or the Gemma 4 architectures 2 on a 12GB GPU introduces a structural imbalance: the physical model weights and active KV caches exceed the available graphics memory. To resolve this bottleneck, this report evaluates speculative decoding, KV cache quantization, adaptive context fitting, and heterogeneous CPU/GPU workload scheduling to establish highly optimized local execution profiles.

## **Speculative Decoding and Multi-Token Prediction**

### **Native Multi-Token Prediction Heads vs Independent Draft Models**

Speculative decoding leverages a smaller, computationally lightweight mechanism to draft candidate tokens that are subsequently verified in parallel by the primary target model.8 In the context of the Qwen 3.6 35B-A3B architecture, two distinct speculative paradigms exist: native Multi-Token Prediction (MTP) heads and independent draft models.10  
Native MTP heads represent an integrated architectural approach where the speculative generation layers are bundled directly with the core model weights in a single GGUF file.11 Because the MTP layers share the target model's base token embeddings, this configuration increases the total model size by a negligible margin of approximately 2.5%, equivalent to a single extra transformer block.11  
During execution, native MTP utilizes the hidden representations of the model's trunk to project future tokens.8 In contrast, independent draft models, such as the Qwen 2.5 Coder 1.5B or 7B, represent entirely separate computational graphs.10 This separate execution structure introduces severe bottlenecks on a 12GB VRAM GPU.12

| Metric | Native MTP Head (Bundled GGUF) | Independent Draft Model (e.g., Qwen 2.5 Coder 1.5B) |
| :---- | :---- | :---- |
| **VRAM Footprint** | \~2.5% increase (shares parent token embeddings) 11 | 1.5 GB to 3.0 GB dedicated allocation 12 |
| **Context Memory Synchronization** | Perfect, zero-copy synchronous state sharing 10 | Disjointed buffers; requires redundant prompt processing 10 |
| **Typical Acceptance Rate** | 75% to 94% on domain-predictable tasks 12 | 50% to 65% on complex, non-linear outputs 13 |
| **Inference Latency Overhead** | Highly parallelized, single-batch verification 8 | High, due to multi-graph scheduling and host transfer 10 |

Native MTP heads achieve superior acceptance rates, especially in structured domains like code generation, mathematical reasoning, and structured JSON outputs.10 Because the MTP head is trained jointly with the base model, its speculative token projections align closely with the target model's output distribution.8 This alignment yields an empirical generation speedup of 1.4x to 2.2x over unspeculative baselines without any degradation in quality.8  
Conversely, independent draft models lack joint state alignment, which lowers draft acceptance rates and increases computational overhead due to separate memory transfers and graph executions.10

### **Optimal Draft Parameters and Checkpointing under VRAM Boundaries**

Maximizing generation throughput for the Qwen 3.6 35B-A3B on a 12GB VRAM platform requires precise tuning of speculative execution parameters.12 The primary configurations include the maximum speculative depth, governed by \--spec-draft-n-max (or \-spec-draft-n-max), the minimum draft token selection probability, controlled by \--spec-draft-p-min, and the context checkpointing frequency, defined by \--ctx-checkpoints.13  
For local execution under hybrid CPU/GPU split modes, configuring \--spec-draft-n-max 2 provides the optimal balance.8 While raising the speculative depth to 3 or 4 can theoretically increase peak generation speeds, it also increases the computational burden on the speculative forward pass.12 If speculative tokens are rejected, the system incurs a latency penalty during rollback.12  
To stabilize acceptance rates during complex, non-deterministic generation sequences, the minimum draft probability \--spec-draft-p-min should be configured to 0.75.13 This parameter acts as a dynamic threshold, pruning speculative draft paths whose cumulative probability falls below the target margin.13

\--spec-type draft-mtp \--spec-draft-n-max 2 \--spec-draft-p-min 0.75 \--ctx-checkpoints 512 \--cache-reuse 256 \--jinja \--swa-full

The Qwen 3.6 35B-A3B architecture integrates Gated DeltaNet linear attention layers alongside standard gated attention, making it a hybrid recurrent transformer.1 Unlike pure attention architectures where key-value cache histories are static, recurrent architectures maintain internal states that must be rolled back to precise historical states when speculative token validation fails.16  
If a draft sequence is rejected, the recurrent state must rollback to a previous token step.16 Without explicit state preservation, the engine is forced to re-evaluate the entire prompt context from scratch, causing latency spikes.16  
Implementing the \--ctx-checkpoints 512 and \--cache-reuse 256 flags resolves this issue by instructing the inference engine to cache recurrent memory states at frequent intervals.18 In cases where draft validation fails, the system rolls back to the nearest valid snapshot, completely avoiding full prompt reprocessing.16  
The \--jinja flag is also critical, as it ensures consistent template tokenization across multi-turn chats; without it, subtle token formatting discrepancies will invalidate the cached checkpoints.18

## **Key-Value Cache Compression and Quantization**

### **Quantization Tradeoffs of 8-bit versus 4-bit and TurboQuant Formats**

The Key-Value (KV) cache is a primary driver of memory consumption as context windows expand.19 On a 12GB hardware footprint, running large models like the Qwen 3.6 35B-A3B or the Gemma 4 31B requires aggressive cache optimization.12 Comparing standard 8-bit quantization (-ctk q8\_0 \-ctv q8\_0) with highly compressed 4-bit configurations (q4\_0, q4\_1, or turbo3 / turbo4) highlights critical trade-offs between speed, accuracy, and VRAM utilization.21  
Symmetric 8-bit (q8\_0) quantization delivers near-lossless precision, maintaining over 93.7% to 98.2% of the unquantized 16-bit baseline's quality across standard benchmarks.21 However, q8\_0 only provides a ![][image1] reduction in cache memory compared to FP16, which is often insufficient for maintaining deep context windows on limited hardware.19  
Transitioning to symmetric 4-bit quantization, such as q4\_0 or q4\_1, halves the memory footprint again but introduces severe precision degradation.19 This degradation is best captured by 99.9% Kullback-Leibler (KL) divergence, which exposes tail-end quality loss that standard perplexity benchmarks hide.21  
At 4-bit precision, this tail damage breaks function-calling capabilities, ruins structured JSON schemas, and leads to semantic confusion, word repetition, and memory decay at context depths exceeding 50,000 tokens.19  
An asymmetric quantization strategy of configuring \-ctk q8\_0 \-ctv q4\_0 (or q4\_1) solves this dilemma.21 Empirical analysis confirms that the Key (![][image2]) cache vector is highly sensitive to quantization because it directly controls the orientation of the attention scores.19 The Value (![][image3]) cache vector is significantly more resilient to quantization.19  
By preserving the ![][image2] cache at 8-bit precision while compressing the ![][image3] cache to 4-bit, the system achieves a balanced, highly optimized memory profile that maintains high semantic recall and function-calling precision while saving substantial VRAM.21

| Cache Configuration | VRAM per 10K Context (Est.) | KLD Precision Retention | Semantic Integrity at 50K+ | Optimal Use Case |
| :---- | :---- | :---- | :---- | :---- |
| **f16 / bf16** | \~1.6 GB | 100% | Flawless | Short context, high precision 23 |
| **q8\_0 / q8\_0** | \~0.8 GB | 93.7% – 98.2% 21 | Excellent | Sub-32K context deployments 19 |
| **q8\_0 / q4\_0 (Asymmetric)** | \~0.6 GB | \~89.5% | Good | Balance of precision and context 21 |
| **q4\_0 / q4\_0** | \~0.4 GB | \~68.0% 21 | Poor (Repetition, logic decay) 19 | Extreme context limits only 19 |
| **turbo3 / turbo3** | \~0.35 GB 25 | Low | Moderate (Fails structural checks) 21 | RAG & heavy prefill workflows 23 |
| **turbo4 / turbo4** | \~0.4 GB 23 | Low | Moderate (High latency overhead) 21 | Deep-context agentic generation 23 |

The alternative extreme compression methods, such as TurboQuant (turbo3 and turbo4), utilize specialized matrices and Fast Walsh-Hadamard Transforms (FWHT) to rotate vectors prior to low-bit quantization.21 This rotation minimizes outliers, allowing turbo3 to squeeze memory footprints down to roughly 3.5 bits per value.25  
However, TurboQuant presents clear hardware limitations. On standard CUDA backends, the computational overhead of running dequantization kernels can slow generation speeds by 10% to 17% compared to optimized native CPU/GPU paths.21 Furthermore, because TurboQuant is often packaged in specialized research forks, maintaining compatibility with mainline feature updates is difficult.23

### **Context Scaling Limits and Out-Of-Memory Avoidance**

Scaling the active context window past 16,000 tokens on a 12GB GPU without triggering CUDA out-of-memory (OOM) faults requires a combination of model-level optimizations and host-level memory management.12  
Gemma 4 models employ a hybrid attention mechanism that fundamentally alters context memory scaling.2 Of the 60 attention layers present in the Gemma 4 31B architecture, only 10 layers utilize global attention.2 The remaining 50 layers employ a localized sliding window attention (SWA) constrained to 512 or 1024 tokens.2 Consequently, the vast majority of the KV cache does not grow linearly with context length.28  
This architectural design allows Gemma 4 to natively sustain context windows up to 128,000 tokens with a very small VRAM footprint, reducing the need for aggressive quantization or offloading of the cache itself.28  
When running Qwen 3.6 35B-A3B, however, the KV cache scales linearly across all layers, demanding strict memory limits.1 To safely manage this, the \--fit on command should be coupled with a restricted slot configuration.22 Setting \--parallel 1 (or \-np 1\) prevents the inference engine from allocating concurrent block spaces, which would otherwise multiply VRAM consumption.12  
Furthermore, context checkpoints (--ctx-checkpoints), while necessary to prevent recurrent rollback recomputation, consume substantial memory.26 For large MoE models, each default checkpoint can require over 500 MiB of system memory.26 Under long-context scenarios (e.g., 80K+ tokens), having \--ctx-checkpoints 32 (the default) will consume massive memory, leading to a quick system memory depletion and triggering Linux OOM killer.15 Users must restrict this via \--ctx-checkpoints 4 or \--ctx-checkpoints 8 to bound the checkpoint memory footprint.26

## **Adaptive Context Fitting and Dynamic GPU Offloading**

### **Dynamic Layer Balancing with Fit Flags**

The \-fit or \--fit command in llama.cpp acts as an automated system loop that dynamically configures model parameters to match the available VRAM of the hardware.22 Rather than requiring manual, trial-and-error calculations for GPU offloaded layers (-ngl or \--n-gpu-layers), the \-fit on flag analyzes the physical capacity of the graphics card at startup and dynamically balances the offloaded components.22

\--fit on \--fit-ctx 16384 \--fit-target 1536

This auto-allocation routine operates on three primary parameters:

1. **\-fit on**: Activates the automated fitting loop.22 It overrides manually declared offloading split parameters and executes a greedy allocation strategy, trying various layer and tensor splits behind the scenes to maximize GPU execution.33  
2. **\-fit-ctx \<n\> (or \-fitc \<n\>)**: Defines the minimum context length the engine must guarantee when calculating the GPU memory layout.22 By default, this is set to 4096 tokens.22 If the user plans to run deep contexts (e.g., 16,384 tokens), setting \-fitc 16384 forces the fitting loop to reserve the necessary VRAM blocks for the KV cache upfront.33 If this is omitted, the engine may maximize layer offloads based on a small context, leading to immediate OOM crashes once the active context grows and the KV cache expands into occupied VRAM.30  
3. **\-fit-target \<MiB\> (or \-fitt \<MiB\>)**: Configures the exact safety headroom to leave free on the GPU.22 This buffer is calculated *after* accounting for pre-existing VRAM allocations from the operating system and background applications.35

### **Establishing Optimal VRAM Headroom and Multimodal Overheads**

Setting the correct \-fit-target is critical to ensuring stable inference.12 If \-fitt 0 is set, the fitting loop will attempt to consume all available VRAM.30 This aggressive offloading almost always causes system instability.30 If even a minor background task requests VRAM, or if the graphics driver allocates temporary scratch buffers, the driver will either throw a CUDA memory allocation failure or silently drop performance to a fraction of normal speeds.35

\-fitt 1536

For a 12GB RTX 4070 platform, the optimal \-fit-target depends on the system's display configuration 12:

* **Primary GPU (Display Active)**: If the RTX 4070 is rendering the desktop environment (X11, Wayland, or Windows DWM), a target of \-fitt 1536 or \-fitt 2048 must be set.12 This reserves 1.5 GB to 2.0 GB of VRAM, protecting the system against display lag and application crashes.12  
* **Secondary/Headless GPU**: If the system's display is driven by the CPU's integrated graphics, leaving the RTX 4070 entirely headless, the headroom can be safely reduced to \-fitt 128 or \-fitt 512\.12 This allows almost the entire 12GB frame buffer to be allocated to model weights and KV blocks.12

Additionally, multimodal processing requires unique memory considerations.1 Both Qwen 3.6 35B-A3B and Gemma 4 models natively support vision processing through secondary projection architectures.1 The vision transformer projector (ViT) demands substantial compute and memory, typically consuming approximately 1.0 GB to 1.5 GB of VRAM during image processing.35  
If the model is executed with vision capabilities enabled, the fit target must be increased to at least 1536 MiB.35 Alternatively, the user can apply the \--no-mmproj-offload command.15 This forces the vision projection tensors to load and execute within system RAM.15 While this introduces a minor latency penalty during image prefill, it frees up 1.5 GB of VRAM, which can be allocated to model layers or context depth.35

## **Hybrid Workload Execution and Heterogeneous Processing**

### **Servicing Engine Efficiency: vLLM vs llama.cpp Offloading**

For resource-constrained environments running Gemma 4 E4B models, managing memory allocation across the GPU and CPU boundary is key to preventing system failures.37 Evaluating the memory management of vLLM against llama.cpp reveals different design philosophies.12

VLLM\_USE\_SIMPLE\_KV\_OFFLOAD=1 vllm serve google/gemma-4-31B-it \--kv-offloading-size 32

vLLM utilizes PagedAttention to manage the KV cache as fixed-size virtual memory blocks.38 By default, vLLM implements a static allocation model, pre-allocating up to 90% of available VRAM (configured via \--gpu-memory-utilization 0.90) at startup.38 While this ensures high concurrent throughput, it leaves very little memory for other applications, which can cause system crashes in mixed-workload environments.38  
vLLM's CPU KV offloading path (enabled via VLLM\_USE\_SIMPLE\_KV\_OFFLOAD=1 and \--kv-offloading-size) aims to move inactive KV cache blocks to system RAM.38 However, this offloading path is unstable on consumer-grade hardware and standard Linux configurations.39  
The system frequently crashes with assertion errors during LRU cache eviction once the host memory buffer saturates, and it experiences driver-level failures when paired with advanced attention backends like FlashInfer.39

llama-server \-m gemma-4-E4B.gguf \-ngl 999 \-c 16384

In contrast, llama.cpp's native CPU offloading model is built for hybrid environments.12 Instead of statically claiming the entire GPU frame buffer, llama.cpp offloads individual model layers sequentially.12 Any layer that cannot fit within the designated VRAM headroom is executed on the CPU using optimized AVX2 and vector instructions.12  
Furthermore, llama.cpp uses OS memory mapping (mmap) to load weights into system RAM on demand.12 On a 32GB system, this prevents host-level OOM crashes by allowing the Linux kernel to dynamically page weight buffers in and out of system memory, providing robust stability during long-running sessions.12

### **CPU Core Isolation and Thread Affinity for Intel Hybrid Processors**

The Intel Core i7-13700KF utilizes a heterogeneous hybrid architecture consisting of 8 Performance Cores (P-Cores, supporting Hyper-Threading for 16 threads) and 8 Efficient Cores (E-Cores, supporting 8 threads), delivering a total of 24 logical execution threads.4 While hybrid architectures excel in multi-tasking consumer workloads, they present significant challenges for synchronous, parallel mathematical libraries like those used in LLM inference.44

taskset \-c $(cat /sys/devices/cpu\_core/cpus)./llama-server \-t 8

The CPU backend in llama.cpp parallelizes tensor operations (such as matrix multiplication) by splitting the input matrices into equal chunks and distributing them across the active thread pool.44 When threads are allowed to span both P-Cores and E-Cores, severe bottlenecks occur.44  
Because E-Cores operate at lower clock frequencies and have lower instructions-per-cycle (IPC) than P-Cores, they take significantly longer to complete their assigned blocks.5 Since llama.cpp threads spinlock and must synchronize at completion barriers, the fast P-Cores end up idling while waiting for the slow E-Cores to finish.44 This synchronization bottleneck can degrade performance by up to 800%.44  
To maximize CPU inference speed, the execution must be strictly isolated to the P-Cores.44 Hyper-Threading (SMT) on the P-Cores should also be bypassed for raw compute tasks.44 Running two threads on the same physical P-Core forces them to share execution units, leading to cache thrashing and pipeline stalls.44 Therefore, the optimal thread count should be set to exactly 8, matching the physical P-Core count.5  
On Linux, the operating system's thread scheduler can be overridden using CPU affinity masks.47 The physical topology of the i7-13700KF maps the high-performance cores and their SMT siblings to the lower logical index range, while the efficient cores occupy the higher indices.49 The precise mapping can be retrieved dynamically from the system's sysfs interface 49:

* **P-Core Logical IDs**: Found in /sys/devices/cpu\_core/cpus (typically logical CPUs 0-15, representing the SMT pairs of the 8 P-Cores).49  
* **E-Core Logical IDs**: Found in /sys/devices/cpu\_atom/cpus (typically logical CPUs 16-23).49

Bash  
\# Execute the llama.cpp server isolated strictly to the physical P-Cores  
taskset \-c $(cat /sys/devices/cpu\_core/cpus)./llama-server \-m model.gguf \-t 8

By binding the inference engine to the P-Cores, the P-Cores can run at maximum turbo boost frequencies without scheduling interrupts.44  
To maintain system responsiveness, background tasks, agentic pipeline loop logic, and other system daemons should be bound to the E-Cores.4 This ensures that background processing does not interfere with the high-priority compute threads on the P-Cores.4

Bash  
\# Bind background agent orchestration scripts or local UI tools to the E-Cores  
taskset \-c $(cat /sys/devices/cpu\_atom/cpus) python background\_agent.py

## **Optimal Hardware Execution Profiles**

To run Qwen 3.6 35B-A3B and Gemma 4 models on an RTX 4070 (12GB VRAM), Intel i7-13700KF, and 32GB RAM setup under Linux, the configurations are consolidated into two primary performance profiles:

| Architectural Domain | Qwen 3.6 35B-A3B MoE Optimization Profile | Gemma 4 31B Dense Optimization Profile |
| :---- | :---- | :---- |
| **Execution Command** | taskset \-c 0-15./llama-server \-m qwen3.6-35b-mtp.gguf 11 | taskset \-c 0-15./llama-server \-m gemma4-31b.gguf 43 |
| **Quantization Scheme** | Q4\_K\_M or APEX-I-Balanced (Weights) 11 | Q4\_K\_M or Q4\_0 (Weights) 15 |
| **CPU Thread Alloc** | \-t 8 (Limits work to physical P-Cores) 15 | \-t 8 (Prevents hyper-threaded pipeline drag) 15 |
| **KV Cache Type K** | \-ctk q8\_0 (Ensures precise attention keys) 21 | \-ctk q8\_0 (Maintains coordinate precision) 21 |
| **KV Cache Type V** | \-ctv q4\_0 (Compresses value states) 21 | \-ctv q4\_0 (Leverages robustness of value states) 21 |
| **Context Checkpoints** | \--ctx-checkpoints 4 (Avoids host RAM exhaustion) 26 | \--ctx-checkpoints 4 (Bounds sliding window state limits) 26 |
| **Dynamic Memory Fit** | \--fit on (Enables dynamic offloading loops) 22 | \--fit on (Enables automated layer scaling) 22 |
| **Memory Headroom** | \-fitt 1536 (Preserves 1.5GB VRAM for OS) 12 | \-fitt 1536 (Preserves safety margin for background tasks) 12 |
| **Target Context Fit** | \-fitc 16384 (Guarantees VRAM for 16K cache) 22 | \-fitc 16384 (Initial pre-allocation sizing) 22 |
| **Speculative Engine** | \--spec-type draft-mtp (Runs native MTP) 13 | Disabled (or \--spec-type dflash if using speculator) 15 |
| **Speculative Depth** | \--spec-draft-n-max 2 (Prevents rollback penalties) 8 | Disabled |
| **Draft Probability** | \--spec-draft-p-min 0.75 (Ensures high acceptance) 13 | Disabled |
| **Multimodal Loading** | \--no-mmproj-offload (Loads projector to host RAM) 15 | \--no-mmproj-offload (Saves 1.5GB VRAM) 15 |
| **Memory Mapping** | \--no-mmap (Locks weights in RAM for stability) 12 | \--no-mmap (Locks weights in RAM) 12 |

#### **Works cited**

1. Qwen3.6 35B A3B \- API Pricing & Benchmarks | OpenRouter, accessed May 20, 2026, [https://openrouter.ai/qwen/qwen3.6-35b-a3b](https://openrouter.ai/qwen/qwen3.6-35b-a3b)  
2. google/gemma-4-E4B \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/google/gemma-4-E4B](https://huggingface.co/google/gemma-4-E4B)  
3. Low Performance due to low utilisation of GPU \- User discussions \- GROMACS forums, accessed May 20, 2026, [https://gromacs.bioexcel.eu/t/low-performance-due-to-low-utilisation-of-gpu/6414](https://gromacs.bioexcel.eu/t/low-performance-due-to-low-utilisation-of-gpu/6414)  
4. i913900t Performance Review: Is This the Right Low-Power High-End Processor for My Compact Build? \- AliExpress, accessed May 20, 2026, [https://www.aliexpress.com/s/wiki-ssr/article/i913900t](https://www.aliexpress.com/s/wiki-ssr/article/i913900t)  
5. Intel® Core™ i7-13700KF Processor (30M Cache, up to 5.40 GHz) \- Product Specifications, accessed May 20, 2026, [https://www.intel.com/content/www/us/en/products/sku/230489/intel-core-i713700kf-processor-30m-cache-up-to-5-40-ghz/specifications.html](https://www.intel.com/content/www/us/en/products/sku/230489/intel-core-i713700kf-processor-30m-cache-up-to-5-40-ghz/specifications.html)  
6. i7-13700KF only E-Cores are used during games (I wanna force it to work with fixed p-cores speed) | Tom's Hardware Forum, accessed May 20, 2026, [https://forums.tomshardware.com/threads/i7-13700kf-only-e-cores-are-used-during-games-i-wanna-force-it-to-work-with-fixed-p-cores-speed.3833909/](https://forums.tomshardware.com/threads/i7-13700kf-only-e-cores-are-used-during-games-i-wanna-force-it-to-work-with-fixed-p-cores-speed.3833909/)  
7. Qwen3.6-35B-A3B: Agentic Coding Power, Now Open to All, accessed May 20, 2026, [https://qwen.ai/blog?id=qwen3.6-35b-a3b](https://qwen.ai/blog?id=qwen3.6-35b-a3b)  
8. Qwen3.6 \- How to Run Locally | Unsloth Documentation, accessed May 20, 2026, [https://unsloth.ai/docs/models/qwen3.6](https://unsloth.ai/docs/models/qwen3.6)  
9. Gemma 4 26B A4B Inference: MoE Model, 256K Context | Modular, accessed May 20, 2026, [https://www.modular.com/models/gemma-4-26b-a4b-it](https://www.modular.com/models/gemma-4-26b-a4b-it)  
10. Feature Request: Pipeline speculative decoding strategies (draft-mtp → ngram-mod) · Issue \#23184 · ggml-org/llama.cpp \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/23184](https://github.com/ggml-org/llama.cpp/issues/23184)  
11. mudler/Qwen3.6-35B-A3B-APEX-MTP-GGUF \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/mudler/Qwen3.6-35B-A3B-APEX-MTP-GGUF](https://huggingface.co/mudler/Qwen3.6-35B-A3B-APEX-MTP-GGUF)  
12. 80 tok/sec and 128K context on 12GB VRAM with Qwen3.6 35B A3B ..., accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1t82zxv/80\_toksec\_and\_128k\_context\_on\_12gb\_vram\_with/](https://www.reddit.com/r/LocalLLaMA/comments/1t82zxv/80_toksec_and_128k_context_on_12gb_vram_with/)  
13. MTP+llama.cpp: a look at Qwen3.6-27B \- DGX Spark / GB10 ..., accessed May 20, 2026, [https://forums.developer.nvidia.com/t/mtp-llama-cpp-a-look-at-qwen3-6-27b/370298](https://forums.developer.nvidia.com/t/mtp-llama-cpp-a-look-at-qwen3-6-27b/370298)  
14. Strix Halo Llama.cpp MTP Benchmarks: 27B Gets Much Faster, 35B ..., accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1teypb8/strix\_halo\_llamacpp\_mtp\_benchmarks\_27b\_gets\_much/](https://www.reddit.com/r/LocalLLaMA/comments/1teypb8/strix_halo_llamacpp_mtp_benchmarks_27b_gets_much/)  
15. llama.cpp speculative checkpointing was merged : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sprdm8/llamacpp\_speculative\_checkpointing\_was\_merged/](https://www.reddit.com/r/LocalLLaMA/comments/1sprdm8/llamacpp_speculative_checkpointing_was_merged/)  
16. ggml-org/llama.cpp b9180 on GitHub \- NewReleases.io, accessed May 20, 2026, [https://newreleases.io/project/github/ggml-org/llama.cpp/release/b9180](https://newreleases.io/project/github/ggml-org/llama.cpp/release/b9180)  
17. Server forces full prompt re-processing on subsequent requests (SWA/recurrent memory error) · Issue \#21831 · ggml-org/llama.cpp \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/21831](https://github.com/ggml-org/llama.cpp/issues/21831)  
18. Does anyone have a working Qwen-Coder-Next configuration on llama.cpp? \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1rlu85e/does\_anyone\_have\_a\_working\_qwencodernext/](https://www.reddit.com/r/LocalLLaMA/comments/1rlu85e/does_anyone_have_a_working_qwencodernext/)  
19. Developers who use local AI \- Q4\_0 vs Q8\_0 KV quant? : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1tfqfvt/developers\_who\_use\_local\_ai\_q4\_0\_vs\_q8\_0\_kv\_quant/](https://www.reddit.com/r/LocalLLaMA/comments/1tfqfvt/developers_who_use_local_ai_q4_0_vs_q8_0_kv_quant/)  
20. google/gemma-4-26B-A4B · Community resources for Gemma 4 deployment — mobile, local, and cloud paths \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/google/gemma-4-26B-A4B/discussions/6](https://huggingface.co/google/gemma-4-26B-A4B/discussions/6)  
21. Here are my KV cache quantization benchmarks: TurboQuant is ..., accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1thu6os/here\_are\_my\_kv\_cache\_quantization\_benchmarks/](https://www.reddit.com/r/LocalLLaMA/comments/1thu6os/here_are_my_kv_cache_quantization_benchmarks/)  
22. llama-server(1) — llama.cpp-tools — Debian unstable \- Debian Manpages, accessed May 20, 2026, [https://manpages.debian.org/unstable/llama.cpp-tools/llama-server.1.en.html](https://manpages.debian.org/unstable/llama.cpp-tools/llama-server.1.en.html)  
23. Qwen 3.6-35B-A3B KV cache bench: f16 vs q8\_0 vs turbo3 vs ..., accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sy7srk/qwen\_3635ba3b\_kv\_cache\_bench\_f16\_vs\_q8\_0\_vs/](https://www.reddit.com/r/LocalLLaMA/comments/1sy7srk/qwen_3635ba3b_kv_cache_bench_f16_vs_q8_0_vs/)  
24. Can we already use Google's TurboQuant (TQ) for KV Cache in llama-server? Or are we waiting for a PR? : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sshpmh/can\_we\_already\_use\_googles\_turboquant\_tq\_for\_kv/](https://www.reddit.com/r/LocalLLaMA/comments/1sshpmh/can_we_already_use_googles_turboquant_tq_for_kv/)  
25. arte-fact/llamacpp-gfx-906-turbo · GitHub, accessed May 20, 2026, [https://github.com/arte-fact/llamacpp-gfx-906-turbo](https://github.com/arte-fact/llamacpp-gfx-906-turbo)  
26. llama.cpp Gemma 4 using up all system RAM on larger prompts : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sdqvbd/llamacpp\_gemma\_4\_using\_up\_all\_system\_ram\_on/](https://www.reddit.com/r/LocalLLaMA/comments/1sdqvbd/llamacpp_gemma_4_using_up_all_system_ram_on/)  
27. Gemma 4 \- LM Studio, accessed May 20, 2026, [https://lmstudio.ai/models/gemma-4](https://lmstudio.ai/models/gemma-4)  
28. Ollama Gemma4:31b on 3090 \- FP,Q8,Q4 Benchmark : r/LocalLLM \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLM/comments/1sc6qrp/ollama\_gemma431b\_on\_3090\_fpq8q4\_benchmark/](https://www.reddit.com/r/LocalLLM/comments/1sc6qrp/ollama_gemma431b_on_3090_fpq8q4_benchmark/)  
29. Qwen/Qwen3.6-35B-A3B \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/Qwen/Qwen3.6-35B-A3B](https://huggingface.co/Qwen/Qwen3.6-35B-A3B)  
30. Llama-CPP never frees up VRAM ? : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1rv94lg/llamacpp\_never\_frees\_up\_vram/](https://www.reddit.com/r/LocalLLaMA/comments/1rv94lg/llamacpp_never_frees_up_vram/)  
31. Completely lost with AI instructions for RTX 4090 and 32 GB RAM : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sqv10p/completely\_lost\_with\_ai\_instructions\_for\_rtx\_4090/](https://www.reddit.com/r/LocalLLaMA/comments/1sqv10p/completely_lost_with_ai_instructions_for_rtx_4090/)  
32. Feature Request: Disk-based context checkpoint offloading (\`--cache-disk\`) · Issue \#20697 · ggml-org/llama.cpp \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/20697](https://github.com/ggml-org/llama.cpp/issues/20697)  
33. r/LocalLLaMA on Reddit: Llama.cpp's "--fit" can give major speedups ..., accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1qyynyw/llamacpps\_fit\_can\_give\_major\_speedups\_over\_ot\_for/](https://www.reddit.com/r/LocalLLaMA/comments/1qyynyw/llamacpps_fit_can_give_major_speedups_over_ot_for/)  
34. If someone needs a deeper dive into llama.cpp's automated offloading mechanisms ("--fit") : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1r2x5aa/if\_someone\_needs\_a\_deeper\_dive\_into\_llamacpps/](https://www.reddit.com/r/LocalLLaMA/comments/1r2x5aa/if_someone_needs_a_deeper_dive_into_llamacpps/)  
35. Llama.cpp's auto fit works much better than I expected : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1srvqar/llamacpps\_auto\_fit\_works\_much\_better\_than\_i/](https://www.reddit.com/r/LocalLLaMA/comments/1srvqar/llamacpps_auto_fit_works_much_better_than_i/)  
36. llama.cpp/tools/llama-bench/README.md at master · ggml-org/llama.cpp · GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/blob/master/tools/llama-bench/README.md](https://github.com/ggml-org/llama.cpp/blob/master/tools/llama-bench/README.md)  
37. \[Bug\]: vLLM fails to start on RDNA 4 (gfx1201) inside containers — amdsmi, circular import, and torch.cuda.device\_count() all broken · Issue \#40081 \- GitHub, accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/40081](https://github.com/vllm-project/vllm/issues/40081)  
38. vLLM or Ollama on Blackwell? Benchmarks, Landmines, and What Agents Actually Need, accessed May 20, 2026, [https://allenkuo.medium.com/vllm-or-ollama-on-blackwell-benchmarks-landmines-and-what-agents-actually-need-5dc539bb28ef](https://allenkuo.medium.com/vllm-or-ollama-on-blackwell-benchmarks-landmines-and-what-agents-actually-need-5dc539bb28ef)  
39. \[Bug\]: SimpleCPUOffloadScheduler crashes with AssertionError: Expected N hit tokens, got 0 (TOCTOU race in update\_state\_after\_alloc) · Issue \#39702 · vllm-project/vllm \- GitHub, accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/39702](https://github.com/vllm-project/vllm/issues/39702)  
40. \[Bug\]: OffloadingConnector GPU-\>CPU KV offload crashes with cuMemcpyBatchAsync failed at index 1 (error 1 / CUDA\_ERROR\_INVALID\_VALUE) · Issue \#39491 · vllm-project/vllm \- GitHub, accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/39491](https://github.com/vllm-project/vllm/issues/39491)  
41. \[Bug\]: Negative prompt token counter crashes engine under CPU KV offloading \+ high concurrency · Issue \#36533 · vllm-project/vllm \- GitHub, accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/36533](https://github.com/vllm-project/vllm/issues/36533)  
42. \[Bug\]: GPT-OSS with CPU KV cache offload break with FlashInfer · Issue \#33572 \- GitHub, accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/33572](https://github.com/vllm-project/vllm/issues/33572)  
43. Deploy Google Gemma 4 on GPU Cloud: MoE and Dense Model Guide (2026) \- Spheron, accessed May 20, 2026, [https://www.spheron.network/blog/deploy-gemma-4-gpu-cloud/](https://www.spheron.network/blog/deploy-gemma-4-gpu-cloud/)  
44. Performance 3x better when use performance core only on Intel gen 12th cpu · ggml-org llama.cpp · Discussion \#572 \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/discussions/572](https://github.com/ggml-org/llama.cpp/discussions/572)  
45. Unlock Unprecedented Performance Boosts with Intel's P-Cores: Optimizing Lama.cpp-based Programs for Enhanced LLM Inference Experience\! : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1codot3/unlock\_unprecedented\_performance\_boosts\_with/](https://www.reddit.com/r/LocalLLaMA/comments/1codot3/unlock_unprecedented_performance_boosts_with/)  
46. Examine multi-threaded performance patterns in llama.cpp \- Arm Learning Paths, accessed May 20, 2026, [https://learn.arm.com/learning-paths/servers-and-cloud-computing/llama\_cpp\_streamline/6\_multithread\_analyze/](https://learn.arm.com/learning-paths/servers-and-cloud-computing/llama_cpp_streamline/6_multithread_analyze/)  
47. Free 10%+ Speedup for CPU/Hybrid Inference on Intel CPUs with Efficiency Cores \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1nhcsmz/free\_10\_speedup\_for\_cpuhybrid\_inference\_on\_intel/](https://www.reddit.com/r/LocalLLaMA/comments/1nhcsmz/free_10_speedup_for_cpuhybrid_inference_on_intel/)  
48. CPU affinity · ikawrakow ik\_llama.cpp · Discussion \#1488 \- GitHub, accessed May 20, 2026, [https://github.com/ikawrakow/ik\_llama.cpp/discussions/1488](https://github.com/ikawrakow/ik_llama.cpp/discussions/1488)  
49. Determine which are P-cores/E-cores (Intel CPU) \- Unix & Linux Stack Exchange, accessed May 20, 2026, [https://unix.stackexchange.com/questions/799993/determine-which-are-p-cores-e-cores-intel-cpu](https://unix.stackexchange.com/questions/799993/determine-which-are-p-cores-e-cores-intel-cpu)  
50. Decode tip: If your Intel CPU has any E-cores, make sure you set affinity to \*only\* the P-cores for a massive speed increase. \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/vhsdecode/comments/1nme9es/decode\_tip\_if\_your\_intel\_cpu\_has\_any\_ecores\_make/](https://www.reddit.com/r/vhsdecode/comments/1nme9es/decode_tip_if_your_intel_cpu_has_any_ecores_make/)  
51. Bizarre Performance Characteristics of Alder Lake CPU |, accessed May 20, 2026, [https://sillycross.github.io/2022/06/11/2022-06-11/](https://sillycross.github.io/2022/06/11/2022-06-11/)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABoAAAAZCAYAAAAv3j5gAAAA/UlEQVR4Xu2TrQoCQRSFLxZBk8Fk8xkEg76AUatNQSwGo1bBFxB8AYNFUAwWg0EUs813MCsI/tzLzuB4dnZXYYqwH3ywc+4yJ9xdoph/osye2Sd7YBOfYzeM2LFxvpBXmDcyJ8ilRUsmOiNN9kttmdDBAChgYDJgS5AFFXXZKYaKKrvBMAopeWCo6LFzyGrsFrJIjuQVpXBg0GcX6llKdsbsK+SjkJIsDixI2Z683+EnMuSVJHEQQJM9sSschCE/KC5/AmcTKdE7abNLYxaKbfF3DBQN8i9eyvTOArnR+3NGkTq7xlDRYmcYanLkv1x7Nd7TDDEAKhjExMS44QUPDjq3JeFIOAAAAABJRU5ErkJggg==>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABMAAAAaCAYAAABVX2cEAAAA1UlEQVR4XmNgGAWUgtlA/AmI/yPhVygqGBi+IMmBsDeqNCaAKcQGmoD4PLogLsDIADHoFroEEFwGYl90QXwgmwFiWDiSGBMQ/wNiLiQxosBLBlQvGgLxUyQ+SQA5vKZB2ccQ0qQBkOYLDBAXakH5uCIDL4CF1x8ksSVQsXwkMaLAawbsriDLdbg0vWWAiCuiS+ACzAwQDafRJYBAlQEi9x5dAhfoZ4BoCEWXgAKYqwXRJZDBMgZIfnwHxV8ZIAkUBmQYIC4CpbXHDBC195DkR8EoGLoAALqKPUMnIoY7AAAAAElFTkSuQmCC>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABEAAAAZCAYAAADXPsWXAAAAo0lEQVR4XmNgGAXYACMQfwDi/0j4LYoKCPjLgJAHsbGC+QwQBQ5o4sgAJI8XJDBAFFWjicPARiA2RhdEB8oMEEO2oUsAARcQP0MXxAVAhnxEFwSCX+gC+AAs4JBBMhDXoInhBdgMQecTBOiGXANiUSQ+UeA7A8IQUEAfQZIjGsxjgBjiB8T30OSIBgkMmF4iGSgyQAxIR5cgFZxGFxgFo4AaAADynCc6DrNJsgAAAABJRU5ErkJggg==>