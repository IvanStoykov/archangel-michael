> **⚠️ Truncated source file** — This document was saved mid-export (~11 KB missing). It cuts off in section 2 (CPG Database). Re-run the subagent or restore from chat history for the full analysis.

# Archangel System Gap Analysis: Codebase vs. Research Design Blueprint

I have completed a comprehensive review of the 8 research documents in `research_docs/` and analyzed the current Go daemon codebase. Below is a detailed gap analysis highlighting the current capabilities of the daemon, the design requirements from the research papers, and the specific features that are not yet implemented, categorized by architectural area.

---

## 1. Summary of Current Codebase Capabilities

The current Go daemon (`archangel-michael`) provides the foundation for the local agent runtime:

* **Configuration & Curated Profiles:** `config/config.go` manages daemon parameters (from `archangel.yml`) and CRUD profiles (from `models.yml`), supporting in-memory environment variable expansion (`os.ExpandEnv`).
* **REST API Server:** `endpoints/router.go` and `endpoints/handlers.go` expose routes for status checks, log streaming (SSE), CRUD operations on curated model profiles, mock daemon process controls (Start, Stop, Restart), and mock task controls.
* **gRPC Stream Server:** `grpc/grpc.go` exposes a protobuf endpoint that broadcasts daemon events to streaming subscribers.
* **Simulation Loop (`main.go`):** Runs a mock agent execution loop (1 to 90 iterations) implementing regex-based thinking extraction (`ExtractExecutableContent`), mock budget warnings (Caution at 75, Warning at 85), and a mock Doom-Loop check using a sliding tool history window.
* **Safety & Watchdog Helpers:** Implements `IsLocalEndpoint` to identify local LLM endpoints (via IP, link-local, private subnet checks, and localhost strings) to disable the GGUF prefill stale timeout (180s). Implements `CalculateCompressionThreshold` to prevent compression locking on exactly 64,000 token context windows.

---

## 2. CPG (Code Property Graph) Database

### Research Requirements (Doc 04)

* **Unified Directed Multigraph:** Combine the Abstract Syntax Tree (AST), Control Flow Graph (CFG), and Program Dependence Graph (PDG) for mixed Go (backend) and Dart/

---

**[Document ends here — remainder not saved]**
