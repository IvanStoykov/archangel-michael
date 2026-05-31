
# Project Feature Proposal: Iterative AI-Assisted Debugger

**Project Integration:** Archangel Michael

## 1. Executive Summary

The proposed feature addresses a core challenge in "vibe coding" or working with unfamiliar repositories: the difficulty of debugging code bases where the architecture and flow are unknown. By integrating a runtime debugger with local, low-overhead AI orchestration, this tool enables step-by-step, function-by-function trace analysis. This approach benefits both the human programmer (by visual mapping) and local LLMs (by minimizing context window bloat).

---

## 2. Key Problems Solved

- **Unfamiliar Repositories:** Simplifies the cognitive load of navigating unknown or AI-generated code bases when errors occur.
- **Context Window Constraints:** Solves the hardware/token limitations of local LLMs by feeding them isolated, function-level snippets rather than the entire code base.
- **Over-reliance on Logging:** Eliminates the need to write verbose print/log statements by capturing live runtime variables directly.
- **Opaque debugging choices:** Preserves a clear record of who chose each trace path and why—whether the operator steered the session or the agent acted alone—so sessions remain explainable after context is flushed or the chat ends.

---

## 3. Core Architectural Concepts

### A. Dynamic Runtime Integration

- **Live Variable Inspection:** The system integrates directly with a runtime debugger to capture the actual states of memory and variables during execution.
- **AI Access to Debug State:** Rather than relying purely on static analysis, the local AI agent has programmatic access to the active debug variables at each step.

### B. Iterative, Guided Code Tracing

- **Function-by-Function Progression:** The UI flips through the execution flow piece-by-piece, method-by-method, rather than line-by-line, providing high-level structural clarity.
- **Automated Explanations:** At each execution boundary, the system:

1. Highlights the active function.
2. Explains its specific intent and logic.
3. Details exactly what data/variables are being passed across the function boundary.

### C. Context Optimization for Local LLMs

- **State Summarization:** As the execution leaves a function, the AI generates a concise summary of the state change.
- **Context Aggregation & Clearing:** To keep processing fast and highly accurate on lower-intelligence or resource-constrained local models, old code context is flushed out, retaining only the function summary and the active variable state for the next step.

### D. Decision Logging

Debugging sessions involve many branching choices—whether to step into a function, follow a thread, accept a hypothesis, or apply a fix. Archangel Michael records each consequential decision as a structured, append-only log so later review (by the same developer, a teammate, or a resumed agent) does not depend on reconstructing intent from chat history or truncated LLM context.

#### Human-in-the-loop decisions

When the programmer steers the trace, the agent captures:

- **What was decided** (e.g., "skip this frame," "break on exception," "treat this variable as the root cause").
- **Who decided** (human operator, with session/user identity when available).
- **Why** — the human's stated reasoning in their own words (from explicit input, a quick rationale field, or a summarized paraphrase of the conversational turn).
- **Context at decision time** — function name, stack depth, relevant variable snapshot or summary, and the agent's last recommendation (if any), so the choice can be understood in situ.

The log is written at the moment the decision is applied, not inferred afterward.

#### Autonomous (agent-only) decisions

When no human is involved, the agent records the same decision envelope but fills **why** from the model's reasoning trace:

- **Options considered** — e.g., step in vs. step over, which thread to pin, which frame to summarize next.
- **Evidence used** — DAP payload excerpts (stack, locals), prior step summaries, and any static snippet in the active prompt.
- **Conclusion** — the chosen route and a concise justification (from structured model output or a dedicated reasoning channel, not only the final action).
- **Confidence or caveats** (optional) — when the model flags ambiguity (async frames, optimized-out variables, inconclusive state).

This makes autonomous runs auditable: a failed or surprising fix can be traced back to which interpretation of runtime state led the agent down that path.

#### Log shape and integration

Each entry is a small JSON object (timestamp, `decision_type`, `actor`: `human` | `agent`, `action`, `rationale`, `debug_context_ref`) appended to a session-scoped decision log. Entries may be streamed alongside existing debug/SSE tooling for live visibility, and persisted for post-mortems. Decision logs are intentionally separate from verbose stdout or LLM chat logs: they are a curated trail of *choices*, not raw token dumps. Summaries produced during context flushing (Section 3.C) can reference decision IDs so long-running sessions stay coherent without reloading full history.

---

## 4. Dual Use-Cases


| Human-in-the-Loop Debugging                                                                   | Fully Autonomous AI Debugging                                                                                 |
| --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Serves as a visual, guided tour through the execution flow.                                   | The local AI leverages the debugger to trace logic flaws without human intervention.                          |
| Empowers the programmer to catch logical edge cases quickly via conversational walk-throughs. | Drastically improves agentic code-editing success rates by injecting real runtime values into the LLM prompt. |
| Human steering and rationale are captured in the decision log at each branch. | Agent reasoning and evidence for each autonomous branch are logged for audit and replay. |

## 5. Viability
This idea is **highly viable**, and you absolutely do not need to build language-specific compiler or debugger integrations from scratch.

There is an industry standard designed exactly for this: the **Debug Adapter Protocol (DAP)**.

Created by Microsoft to decouple VS Code's UI from underlying execution engines, DAP abstracts debugging into a standardized, JSON-based wire protocol (communicating over `stdin`/`stdout` or TCP). Since almost every modern language has a mature DAP implementation (e.g., `debugpy` for Python, `delve` via DAP for Go, `vscode-js-debug` for Node, Native GDB/LLDB DAP modes), your tool, **Archangel Michael**, only needs to act as a **DAP Client**.

Here is a breakdown of how this protocol directly solves your design needs and how viable the implementation is.

---

### How DAP Maps to Your Architecture

Instead of managing binaries, you spin up the language’s existing Debug Adapter as a background process and exchange JSON messages. Here is how your specific feature ideas translate into DAP commands:

#### 1. "Flip through the code piece-by-piece, method-by-method"

DAP handles execution flow natively. While traditional debugging relies on `next` (step over a line) or `stepIn` (step into a line), you can automate navigation to match your high-level structural tracing:

* **The Mechanism:** When execution pauses, you can query the call stack using the `stackTrace` request.
* **Method Boundaries:** By analyzing the source locations returned in the stack frames, your orchestration layer can track when a boundary changes (e.g., when a frame is pushed or popped) and essentially fast-forward line steps until the execution scope lands on a new method or function entry.

#### 2. "See the variables / Access to debug variables"

You don’t need to parse memory or inspect logs. DAP provides structured variables cleanly.

* **The Mechanism:** When the debugger stops at a function, you send a `scopes` request to get the available scopes (Local, Global, Closures).
* **Extracting Values:** You take the `variablesReference` ID from the scope response and send a `variables` request. The adapter returns a clean JSON array containing all active variable names, types, and values in that current frame.

#### 3. "Context cleared, just useful contexts used"

This is the strongest aspect of your architectural idea. Instead of feeding an LLM 2,000 lines of historical context or messy stdout log blocks, your DAP client acts as a high-fidelity state-reducer:

* **The Prompt Payload:** For every function jump, you assemble a compact, pristine payload for the local LLM containing:
1. The static source code of the *current* function block.
2. The JSON object representing the active `Local` variables.
3. The immediate parent frame name from the stack trace.


* **Flushing:** Once the LLM processes or summarizes that step, you discard the heavy code strings from the agent's context window, preserving only the textual summary and moving on to the next payload.

---

### Implementation Viability & Technical Trade-offs

```
  +---------------------------+
  |  Archangel Michael Agent  |
  +-------------+-------------+
                | JSON over stdin/stdout or TCP
                v
  +---------------------------+
  |    DAP Client Layer       |  <-- You implement this (or use an open-source library)
  +-------------+-------------+
                | Abstracted DAP Protocol
                v
  +---------------------------+
  |   Language Debug Adapter  |  <-- Pre-existing (e.g., debugpy, Delve, GDB-DAP)
  +-------------+-------------+
                | Native Debug API
                v
  +---------------------------+
  |      Target Program       |  <-- The code being debugged
  +---------------------------+

```

#### The Pros: Why this works beautifully

* **Universal Interface:** You write the wrapper *once*. Supporting a new language simply means pointing Archangel Michael to a different adapter executable and sending the appropriate `launch` or `attach` configuration JSON.
* **Perfect for Local LLMs:** Low-parameter or heavily quantized local models (like a Qwen-style coder or Gemma) fail when overwhelmed by context. Providing them an explicit execution state-machine step drastically lowers the reasoning penalty.

#### The Cons: Complexities to plan for

* **Async Events & Asynchronous Code:** If the target code heavily utilizes multi-threading, event loops, or async/await syntax, step-by-step function tracing becomes noisy. Frames will thrash between threads. Your orchestration layer will need a robust way to filter `threadId` or pin focus to a single execution lane.
* **The Overhead of Step-Filtering:** Evaluating variables at every single method boundary adds communication overhead. For heavy local workloads, running headless optimization passes or limiting tracing exclusively to the failure path (e.g., using `exceptionInfo` requests to break only when an error throws, then working backward) will prevent performance bottlenecks.

---

### Action Plan for Archangel Michael

If you want to validate this quickly without writing a protocol parser from scratch, don't reinvent the wheel.

1. Look into existing open-source DAP client libraries in your language of choice (for Python, there are libraries like `sans-io-dap` or you can reference how headless test-runners implement it).
2. Wire up a basic script that launches a target file via its standard debug adapter, programmatically sets a breakpoint at `main`, issues a `next` command, and prints out the resulting `variables` JSON object.

Once you see how clean that variable payload is, hooking it up to inject directly into your file-editing LLM pipeline will be straightforward.
