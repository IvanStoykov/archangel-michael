# **Engineering Report: Runtime Optimization and Configuration Diagnostics for the Hermes Agentic Runner on Local GGUF Architectures**

## **Command-Line Architecture, Parsing Diagnostics, and Execution Flow Controls**

The open-source Hermes agentic runner serves as a highly modular, multi-platform runtime designed for autonomous, stateful task execution.1 The runner operates across three primary command-line execution interfaces: hermes chat for launching interactive sessions, hermes run for executing specific tasks non-interactively, and hermes serve to initialize background daemon services and messaging gateways.3 For recurring workflows, the framework incorporates an integrated cron scheduling engine, enabling scheduled operations such as nightly security audits.4  
When executing these core utilities, the runner coordinates stable updates and development channels via defined release-governance lanes to preserve existing work.6 For local code auditing, the runner behaves as a coordinator that wraps command-line utilities—such as Semgrep or CodeQL—within sandboxed execution environments.7 This orchestration can be modeled as a Markov Decision Process (MDP), wherein the runner structures its context window to maximize the expected utility of sequential decisions.2 The state space ![][image1] at any given step ![][image2] is formulated as:  
![][image3]  
where ![][image4] is the set of system instructions and environment variables, ![][image5] represents the accumulated multi-turn conversation history, and ![][image6] denotes the history of tool execution outputs and observations up to step ![][image2].2 The probability of selecting a specific tool-calling action ![][image7] is determined by the model parameterization ![][image8] under the policy ![][image9].2 To guide the agent toward task resolution and avoid endless execution loops, a discounted utility function is applied:  
![][image10]  
where ![][image11] is a discount factor restricting the utility of excessively prolonged step-by-step sequences, encouraging the agent to conclude its tasks efficiently.2  
Operational diagnostic failures during execution frequently stem from command-line parsing anomalies within the argparse configuration of the runner. A notable issue identified in hermes\_cli/main.py is the argparse destination variable collision.8 When parsing the command hermes mcp add \--command \<cmd\>, the parser maps the \--command parameter to the same destination attribute as the top-level subparser, which tracks the global routing instruction (args.command).8 Consequently, when a practitioner omits the \--command flag, argparse overwrites the active global command value with None.8 This routes the execution back to the default interactive chat loop (cmd\_chat) rather than the intended MCP registration logic.8 To circumvent this, distinct destination mappings must be assigned to subparser flags.8  
Additionally, the command-line interface implements a session-name coalescing helper, \_coalesce\_session\_name\_args(), located in hermes\_cli/main.py.9 This function prevents multi-word, unquoted session titles passed during session resumption via \-c or \--continue from being parsed as distinct commands.9 However, because the internal \_SUBCOMMANDS allowlist does not register all top-level subcommands, invoking commands like logs, debug, or claw directly after a session reference can cause the parser to fold the command into the session title.9 For example:  
hermes \-c main\_session logs  
This command is parsed as resuming a session titled main\_session logs rather than viewing execution logs.9 Operators must explicitly quote session names or use top-level flags to bypass the coalescing parser.9

hermes \-c "main\_session" logs

For rapid, single-turn tasks, the runner provides a oneshot execution path accessed via hermes \-z.10 This pathway is designed to quickly execute prompt instructions without loading persistent system context files or long-term episodic memories.10 However, a flag-wiring discrepancy exists in hermes\_cli/main.py.10 While the top-level parser registers the \--ignore-rules flag and correctly maps it to the HERMES\_IGNORE\_RULES=1 environment variable, the oneshot dispatcher in hermes\_cli/oneshot.py fails to forward this parameter to the underlying AIAgent constructor.10 Consequently, the oneshot path defaults to loading local context files and memories, which can degrade performance.10 To resolve this, the oneshot executor must be patched to read from the environment and explicitly toggle skip\_context\_files=True and skip\_memory=True.10  
To manage local compute resources, the core agent class AIAgent in run\_agent.py establishes an iteration cap of 90 tool-calling iterations per turn.11 When an analytical task demands intensive scanning, hitting this limit triggers a process termination hazard.12 Historically, on budget exhaustion, AIAgent.run\_conversation() would silently exit the Python interpreter without raising exceptions or executing registered exit handlers.12 This is caused by unhandled termination cycles in the post-execution cleanup chain during trajectory saving (\_save\_trajectory) and session persistence (\_persist\_session).12  
To prevent these silent exits and allow the model to wrap up complex analysis, a multi-tiered token budget pressure mechanism is implemented within the execution loop in run\_agent.py.13 This mechanism dynamically injects system prompts into the active API context based on the remaining iteration budget:

| Budget Warning Tier | Activation Threshold | Injected System Prompt |
| :---- | :---- | :---- |
| **Caution Tier** | **![][image12]** 13 | \`\` 13 |
| **Warning Tier** | **![][image13]** 13 | \`\` 13 |

These prompts force the local GGUF model to transition from active tool-calling (e.g., executing repetitive file-system scans) to an analytical synthesis phase.13 To extend the execution boundary for long-running audits without globally inflating the iteration cap, the runner configuration supports a bounded continuation model via max\_auto\_continues.14 When enabled, upon reaching the iteration cap, the runner appends a continuation prompt to the thread, extends the session budget by one additional max\_iterations window, and repeats the execution up to the configured limit without triggering a hard cutoff.14  
The table below outlines the primary command-line configurations and options available for configuring the Hermes runner:

| Command & Flags | Argument Parameters | Operational Function |
| :---- | :---- | :---- |
| hermes chat 3 | \-m \<alias\> / \--provider \<name\> 15 | Initiates an interactive terminal session using a specific model and provider configuration. |
| hermes run 3 | \--agent \<name\> / \--dry-run 4 | Executes agentic code non-interactively; the \--dry-run flag executes logic without writing changes. |
| hermes \-z 10 | \<prompt\> / \--ignore-rules 10 | Runs a single instruction in oneshot mode, bypassing conversational loops and rules. |
| hermes guard 16 | install-hook / uninstall-hook 16 | Scans staged git files for secrets; can be integrated as a pre-commit verification hook. |
| hermes plan 16 | "\<intent\>" 16 | Analyzes current repository state and proposes safe git operations without altering code. |
| hermes schedule 4 | enable / disable / list 4 | Manages background cron jobs and scheduled tasks. |

## **Hyperparameter Tuning and Sampling Optimizations for Local GGUF Inference**

Local GGUF models, such as Qwen 2.5 Coder 32B or Qwen 3.6 35B-A3B, require meticulous hyperparameter tuning when deployed for code-auditing tasks.17 Because these models are executed locally via inference engines like llama.cpp or oMLX, default generation profiles can lead to degraded tool-calling mechanics.19  
A primary failure mode of local models during long-running static analysis is the repetitive tool-calling loop, where the model repeatedly queries the same file directory or executes the same terminal diagnostics.2 This behavior is directly tied to the sampling temperature.20 In earlier iterations of the Hermes runner, the inference temperature parameter was not exposed in config.yaml or the command-line interface, defaulting instead to the backend default of ![][image14].20 High temperatures introduce excessive entropy, causing local GGUF models to experience factual drift and degrade their structured output parser alignment.20 Conversely, setting the temperature to ![][image15] can cause deterministic deadlocks where the model repeatedly generates the exact same incorrect tool call.22  
To enforce tool-calling stability on local backends, parameters must be configured under the provider-specific params or the root model section.11 For custom providers running local GGUF models, the runner defaults to a temperature of ![][image16] to maintain analytical focus while allowing sufficient variation to recover from invalid tool syntax.22  
Furthermore, local GGUF deployments are prone to serializing tool calls across separate turns.22 When the model needs to inspect several files, it may wait for an execution response for each file individually, prolonging the analysis and increasing token consumption.22 To resolve this, the runner injects the parallel\_tool\_calls: true parameter into outbound /v1/chat/completions payloads.22 This forces the local model to batch multiple tool calls (e.g., executing four sequential file reads) in a single assistant turn, reducing token overhead and accelerating analysis.22  
Specific models require targeted overrides within the runtime driver.23 For instance, Kimi-based cognitive backends are hardcoded to strip global temperature variables, pinning the active temperature to exactly ![][image17] within the final request builder to prevent API validation failures.23  
The table below outlines the optimal hyperparameter settings for local GGUF models executing long-running code audits:

| Hyperparameter | Optimal Setting | Operational Objective |
| :---- | :---- | :---- |
| model.temperature | 0.20 to 0.30 22 | Suppresses hallucinated tool calls and stabilizes structured output parsing during multi-turn analysis. |
| model.top\_p | 0.85 to 0.90 24 | Limits the token selection pool to high-probability candidates, eliminating low-probability syntax errors. |
| model.top\_k | 40 to 50 24 | Restricts cumulative probability distribution tail sampling, preventing structural drift. |
| parallel\_tool\_calls | true 22 | Instructs the model to generate and execute multiple tool calls concurrently in a single turn. |
| api\_max\_retries | 0 or 1 25 | Prevents infinite retry loops on local GGUF engine failures, enabling rapid fallback to alternative endpoints. |

## **Output Formatting, Artifact Validation, and Session Integrity**

Executing automated static audits on local GGUF models requires strict output schema validation.7 Without structured boundaries, local models often return unverified capability claims or fail to generate parseable artifacts.26  
A common failure mode of local models is generating unverified capability claims, such as stating a static analysis rule does not exist without checking the codebase, or claiming a configuration file is absent when it is present.27 To mitigate this, the Hermes runner supports pre-response validation architectures to evaluate model responses before they are returned 27:

* **Prompt-Level Gate**: System prompt guidelines force the model to self-audit and run verification tools prior to output generation.27 This represents low operational complexity but relies on model adherence and consumes token context.27  
* **Middleware Hook**: A hook registered in run\_agent.py intercepts text responses and runs custom checks: hook.run(text\_response, context).27 This moderate complexity configuration allows programmatic execution of local diagnostic checks prior to delivery.27  
* **Declarative Schema Gate**: Cross-references generated claims against a validated, declarative system capability schema.27 This high complexity configuration provides a robust framework to eliminate false claims.27

For complex static analysis pipelines, the runner implements a Managed Agent Runtime contract layer, which transitions execution from unstructured interactions into durable, observable, and verifiable processes.26 Under this contract, the auditor utilizes a structured ProfileRun manifest to enforce inputs, workspace environments, and output criteria.26

JSON  
{  
  "run\_id": "audit-run-2026-05-20-001",  
  "profile": "sast-vulnerability-scanner",  
  "status": "running",  
  "workspace": {  
    "kind": "worktree",  
    "path": "/opt/workspace/target-repo"  
  },  
  "sandbox": {  
    "backend": "docker"  
  },  
  "artifacts": {  
    "contract": {  
      "required\_outputs": \["reports/vulnerability-report.json", "patches/vulnerabilities-resolved.patch"\]  
    }  
  }  
}

During execution, the runner enforces strict artifact validation contracts.26 The agent must write target deliverables to disk, and the orchestrator validates these outputs against declarative schemas or executable validation tools.26 If the local model fails to generate these artifacts, the session terminates with a structured validation error rather than a generic success state, preventing false negatives during automated scans.26  
Long-running audits are highly sensitive to load-time configuration parsing.28 Historically, config.yaml was parsed without schema enforcement, causing unrecognized or misspelled parameters to be silently ignored.28 To harden local deployments, the runner utilizes Pydantic and jsonschema schemas during startup to validate configuration keys.28 This prevents runtime failures caused by misconfigured parameters, such as malformed local base URLs.29  
Furthermore, output rendering constraints on specific platforms can cause premature session termination.30 When deploying memory plugins (e.g., Honcho), using root-level logical blocks like anyOf, oneOf, or allOf in JSON schema tool parameters can cause local inference engines to return HTTP 400 errors.31 To maintain session stability, these parameter blocks must be removed or handled via runtime verification.31  
For reporting and review, the html-artifacts skill (skills/creative/html-artifacts/SKILL.md) is used to generate self-contained, offline-compatible HTML dashboards rather than standard linear markdown.32 This allows the runner to export interactive code-auditing reports containing side-by-side patch comparisons, code differences, and severity ratings without requiring connection to external rendering services.32  
On messaging platforms such as Feishu/Lark, rendering long markdown tables in linear chat blocks often breaks formatting or degrades readability.30 The runner implements a Feishu platform adapter workaround, \_build\_post\_with\_md(), which intercepts output containing markdown tables using a \_MARKDOWN\_TABLE\_RE regex.30 Instead of transmitting plain text, the adapter converts the output message type from text to post with tag: md.30 This triggers native rich-text rendering on the target platform, ensuring complex code-audit metrics and scanning tables display correctly without incurring additional LLM token overhead.30  
Similarly, the platform integration pipeline supports the extraction of media deliverables using a strict tagging convention.33 When the model generates an absolute file path tagged with MEDIA:/absolute/path/to/file, the gateway intercepts this tag.33 The platform-specific adapters (such as the Signal or Telegram integrations) parse the path, perform file size and validation checks, and route the file natively as a rich attachment (using send\_image() or send\_document()) while removing the raw tag string from the user's visible chat bubble.33 This prevents formatting errors or broken path strings on platforms that do not support raw local paths.33

## **Timeout Management, Retries, and Stream Preservation**

Executing long-running static analysis tasks on local hardware involves managing execution budgets across several overlapping layers: the local inference prefill, the terminal command environment, the global task agent, and the downstream push steps.19

### **Prefill watchdog bypass and Docker DNS validation**

A major obstacle when running large GGUF models (e.g., Qwen 2.5 Coder 32B) on local hardware is the prefill timeout deadlock.19 When a long-context codebase audit is sent to a local GGUF server, the initial prompt processing (prefill) phase can easily exceed the default 180-second stream stale timeout.19 If the prefill phase exceeds this limit, the runner emits \[stream\_generate\] Aborting request and disconnects.19 This abort signal forces the local GGUF backend to terminate prompt evaluation (prefill interrupted).19 Crucially, the runner immediately retries the exact same heavy request, causing the local model to restart prefill from scratch.19 This creates an infinite loop that blocks the model from ever generating a response.19  
To resolve this issue, the runner uses the is\_local\_endpoint() helper in agent/model\_metadata.py to identify local endpoints and disable the stream stale timeout.38 However, early versions of this function only checked IP addresses, localhost, and loopback interfaces, causing it to miss local GGUF servers accessed via Docker or Podman DNS names (e.g., http://ollama:11434 or http://hermes-litellm).39 The patched helper employs a multi-tiered validation approach to identify local endpoints and prevent timeouts:

* **Unqualified Hostname Inspection**: Hostnames without periods (e.g., ollama, litellm) are automatically classified as local network targets.39  
* **DNS Resolution Fallback**: Hostnames are resolved to IP addresses via socket.gethostbyname(). If the IP falls within private ranges (RFC 1918), the endpoint is classified as local.39  
* **Explicit Local Endpoints Register**: Practitioners can explicitly register local hostnames under the model.local\_endpoints key in config.yaml.39

By identifying these local hostnames, the runner disables the 180-second stale stream watchdog, allowing the GGUF backend to complete long prefill phases uninterrupted.38

### **Aligning Command, Agent, and Gateway Timeouts**

When executing long-running audits, practitioners must align several runtime timeout thresholds.35 Under default settings, a task's runtime budget and the underlying terminal command timeout can conflict:

   
           |  
           \+---\>  \<--- Programmatic Reset on Tool Activity  
           |  
           \+---\>

If the global task allows a worker to run for 3600 seconds but the underlying terminal execution environment is capped at a default terminal.timeout of 180 seconds, long-running audits (such as deep CodeQL compilation steps) will be forcefully terminated.35 To prevent this, the runner coordinates these budgets: when a task defines a custom max\_runtime\_seconds, the terminal runner inherits this value (minus a small grace window), allowing the shell process to execute for the full duration of the task.35  
Similarly, the global execution layer has transitioned from a hard 10-minute wall-clock timeout (HERMES\_AGENT\_TIMEOUT, default 600s) to an activity-based timeout model.37 Instead of terminating a task because it exceeds 10 minutes, the runner monitors a \_last\_activity timestamp, which is updated on every successful tool call or API response.37 This distinguishes active, long-running processes (such as a subagent executing multiple file scans via delegate\_task) from frozen network connections.37 For background operations, independent limits like HERMES\_CRON\_TIMEOUT (default 1800s) are applied to manage offline executions.37

### **Stage Isolation and Partial Stream Recovery**

For complex, multi-stage tasks (e.g., checkout ![][image18] scan ![][image18] compile ![][image18] publish), failure at a downstream step (such as pushing the report to a messaging gateway) can cause the runner to re-execute the *entire* workflow from step 1\.36 This is highly inefficient and consumes excessive tokens.36 The runner addresses this through stage isolation, output persistence, and partial retries.36 By saving the outputs of successful steps to local disk, the runner can resume execution directly from the failed step, preventing redundant analysis.36  
Furthermore, to handle transient network issues or Server-Sent Events (SSE) drops during generation, the runner implements partial stream preservation in run\_agent.py.40 Instead of discarding the response and retrying from scratch, the system saves the accumulated text and tool-call tokens in a local response cache.40 If the connection is re-established, the runner attempts to merge the new tokens with the preserved partial stream, reducing token overhead and maintaining conversation continuity.40

## **Advanced Environment Variables, Custom Providers, and Context Semantics**

Optimizing multi-turn interaction with local GGUF models requires proper configuration of environment variables, custom providers, and context limits in config.yaml.17

### **Preserving Placeholders and Expanding Environment Variables**

In enterprise deployments, secrets (such as API keys) are typically stored in .env files rather than hardcoded in config.yaml.4 However, early versions of the runner resolved these environment variables (e.g., ${MY\_API\_KEY}) during execution and wrote the plaintext values back to config.yaml whenever the configuration was saved, creating a security risk.42 The patched configuration loader preserves raw environment placeholders during disk writes.42 The system maintains a raw copy of the configuration on disk, expanding variables only in memory at the point of use in network transport layers.42  
Additionally, environment variable expansion must be active across all execution paths.44 For example, running cron jobs without an explicitly defined model can fail if the system fails to expand ${HERMES\_MODEL} or ${OPENROUTER\_FALLBACK\_MODEL} from .env, passing the raw unexpanded variable to the local endpoint and triggering API errors.44 The modern runner ensures that the credential resolution pool is correctly seeded with expanded variables across all CLI, cron, and gateway contexts.44

### **Resolving Stale Environment Overrides and Auxiliary Model Routing**

When changing model settings in a running session, the runner must resolve conflicting environment variables.46 For instance, auxiliary tools like vision\_tools.py and browser\_tool.py historically used os.getenv("AUXILIARY\_VISION\_MODEL") directly, bypassing the central configuration.46 If a user updated the model in config.yaml without restarting the process, the auxiliary tools continued to route requests to the old model.46 The centralized auxiliary client now manages all model resolution.46 Under this architecture, the runner queries the active config.yaml as the source of truth, falling back to environment variables only if no configuration override is specified.46  
The runner also separates the configuration paths for display and execution.47 Historically, custom endpoints had to be declared in both providers: (for the /model picker) and custom\_providers: (for runtime execution), which caused configuration drift and resolution failures.47 The configuration format has been consolidated: both the model selection interface and the runtime resolver now query custom\_providers: as the single source of truth for custom local endpoints.47  
Furthermore, custom platform adapters (e.g., the Webhook adapter) read their settings from platforms.\<platform\>.extra.48 To prevent custom configurations from being silently dropped during parsing, PlatformConfig.from\_dict() extracts known keys (e.g., enabled, token) and automatically merges any unrecognized keys into the config.extra dictionary.49 This ensures custom parameters (such as a local webhook port) are preserved and routed to the adapter.49

### **Managing Context Limits and the 64K Compression Lock**

To prevent out-of-memory errors on local backends during long-running tasks, the runner implements an active context compression framework (context: engine: compressor).41 This framework compresses historical turns when context usage exceeds a set threshold 41:

YAML  
context:  
  engine: compressor  
  compression:  
    enabled: true  
    threshold: 0.70  
    target\_ratio: 0.20  
    protect\_last\_n: 20  
    hygiene\_hard\_message\_limit: 400

Under this configuration, when the token count exceeds 70% of the model's context window, the compressor compresses historical context down to 20% of the threshold, while preserving the last 20 messages uncompressed to maintain immediate conversation history.41  
However, when configuring a local model with a context window of exactly 64,000 tokens (a common configuration when splitting a 192K context GGUF model across three parallel slots, resulting in 64K per slot), the compressor historically failed to trigger 17:  
![][image19]  
For a 64,000 token context window and a 70% compression threshold, the calculation was:  
![][image20]  
This forced the active compression threshold to 100% of the context window.17 As a result, the local GGUF server would run out of context and fail before the prompt tokens could reach the 64,000 threshold to trigger compression.17  
To prevent this compression lock, the threshold calculation includes a safety check.17 If the floor calculation sets the threshold to 100% of the context window or higher, the system falls back to a percentage-based threshold:

Python  
self.threshold\_tokens \= max(  
    int(self.context\_length \* threshold\_percent),  
    MINIMUM\_CONTEXT\_LENGTH,  
)  
if self.threshold\_tokens \>= self.context\_length:  
    self.threshold\_tokens \= int(self.context\_length \* threshold\_percent)

This fallback ensures that compression triggers reliably at 44,800 tokens (![][image21]), preventing context overflow failures on multi-slot local setups.17  
During recovery from a context overflow, the runner distinguishes between the model's base context window and the session's temporary effective context window.50 When an overflow occurs, the system can apply temporary recovery actions (such as shrinking the context, clamping output tokens, or tier downgrading) without overwriting the base model capability on disk.50 Resetting the session via /new or /reset restores the base context window, maintaining performance for subsequent tasks.41

### **Mitigating Anthropic Claude Code OAuth Usage Exceptions**

When utilizing the runner alongside Anthropic's subscription models (e.g., Sonnet or Opus via Claude Code OAuth tokens), long-running audits can trigger unexpected HTTP 400 errors.34 This occurs because Anthropic's safety filters analyze inbound system prompts for specific agentic keywords, such as session\_search, skill\_manage, and MEDIA:/.34 Because the runner injects its available skills catalog and system instructions (which can exceed 600 tokens per turn) into the system prompt, these filtered keywords are frequently present.51 If the subscription lacks usage-based billing, Anthropic's API rejects the request with an "out of extra usage" error.34  
The runner employs two primary workarounds to bypass these keyword filters 34:

* **Keyword Obfuscation**: The runner replaces sensitive terms in outbound system prompts with masked alternatives, such as changing session\_search to ses\*sion\*\_sea\*rch and skill\_manage to sk\*ill\*\_man\*age.34 These masked strings bypass Anthropic's regex filters while remaining semantic enough for the model to interpret and execute.34  
* **MCP Prefix Removal**: The runner strips the mcp\_ prefix from registered tool names.51 This prevents tools like mcp\_\_mcp\_browser from triggering overage checks, preserving access to MCP-based tools during subscription-gated runs.51

## **Conclusion and Actionable Configuration Blueprint**

To ensure stability and performance during long-running static analysis tasks on local GGUF models, practitioners should implement the following consolidated config.yaml profile. This configuration incorporates the prefill watchdog bypass, aligned timeouts, optimal sampling parameters, and context compression settings detailed in this report.

YAML  
\# Consolidated config.yaml for Local GGUF Code-Auditing Runners  
\_config\_version: 5

model:  
  default: qwen2.5-coder-32b-instruct  
  provider: custom  
  base\_url: http://ollama:11434/v1  
  context\_length: 64000  
  local\_endpoints:  
    \- ollama  
    \- hermes-litellm  
    
  \# Sampling parameters for tool-calling stability  
  temperature: 0.20  
  top\_p: 0.90  
  top\_k: 40  
  parallel\_tool\_calls: true  
  api\_max\_retries: 1

agent:  
  max\_iterations: 150  
  max\_auto\_continues: 3  
  gateway\_timeout: 1800  
  preserve\_env\_refs: true

context:  
  engine: compressor  
  compression:  
    enabled: true  
    threshold: 0.70  
    target\_ratio: 0.20  
    protect\_last\_n: 20  
    hygiene\_hard\_message\_limit: 400

terminal:  
  timeout: 600

providers:  
  custom:  
    stale\_timeout\_seconds: 600

platforms:  
  telegram:  
    enabled: true  
    extra:  
      base\_url: "http://tg-bot-api:8081/bot"  
      local\_mode: true

#### **Works cited**

1. How to Use Hermes Agent \- Apidog, accessed May 20, 2026, [https://apidog.com/blog/use-hermes-agent/](https://apidog.com/blog/use-hermes-agent/)  
2. Breaking the Chains of Walled-Garden AI: Why I Built with Hermes Agent (And How to Run It Globally) \- DEV Community, accessed May 20, 2026, [https://dev.to/moni121189/breaking-the-chains-of-walled-garden-ai-why-i-built-with-hermes-agent-and-how-to-run-it-globally-3nn0](https://dev.to/moni121189/breaking-the-chains-of-walled-garden-ai-why-i-built-with-hermes-agent-and-how-to-run-it-globally-3nn0)  
3. Awesome Hermes Agent | Skills Market... \- LobeHub, accessed May 20, 2026, [https://lobehub.com/skills/aradotso-trending-skills-awesome-hermes-agent](https://lobehub.com/skills/aradotso-trending-skills-awesome-hermes-agent)  
4. How to Build a Cron-Based AI Automation with Hermes Agent: Scheduling and Skills, accessed May 20, 2026, [https://www.mindstudio.ai/blog/build-cron-based-ai-automation-hermes-agent](https://www.mindstudio.ai/blog/build-cron-based-ai-automation-hermes-agent)  
5. Hermes Agent — Open-Source AI Agent with Persistent Memory, accessed May 20, 2026, [https://hermes-agent.org/](https://hermes-agent.org/)  
6. Add fixed-cycle stable version release mechanism (monthly/optional weekly) · Issue \#8063 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/8063](https://github.com/NousResearch/hermes-agent/issues/8063)  
7. Feature: Code Security Audit Skill — SAST Scanning, Vulnerability Validation, and Automated Patching (inspired by RAPTOR) · Issue \#382 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/382](https://github.com/NousResearch/hermes-agent/issues/382)  
8. \[Bug\]: hermes mcp add silently launches chat instead of registering MCP server \#19785, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/19785](https://github.com/NousResearch/hermes-agent/issues/19785)  
9. CLI session-name coalescing swallows real top-level subcommands like 'logs', 'debug', and 'claw' · Issue \#12649 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/12649](https://github.com/NousResearch/hermes-agent/issues/12649)  
10. ignore-rules (oneshot path never reads HERMES\_IGNORE\_RULES) · Issue \#26633 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/26633](https://github.com/NousResearch/hermes-agent/issues/26633)  
11. Feature Request: Make max\_iterations configurable via config.yaml · Issue \#18601 · NousResearch/hermes-agent · GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/18601](https://github.com/NousResearch/hermes-agent/issues/18601)  
12. run\_conversation silently kills the Python process when iteration budget is exhausted · Issue \#8049 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/8049](https://github.com/NousResearch/hermes-agent/issues/8049)  
13. Feature: Iteration Budget Pressure — Warn the LLM Before Max Iterations Hit · Issue \#414 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/414](https://github.com/NousResearch/hermes-agent/issues/414)  
14. Configurable auto-continue when max tool-call iterations are reached · Issue \#16068 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/16068](https://github.com/NousResearch/hermes-agent/issues/16068)  
15. CLI doesn't honor user-defined providers via chat \--provider or \-m  
16. simandebvu/hermes-cli: Intent driven git \- GitHub, accessed May 20, 2026, [https://github.com/simandebvu/hermes-cli](https://github.com/simandebvu/hermes-cli)  
17. BUG: Context auto-compression never triggers when context\_length \== MINIMUM\_CONTEXT\_LENGTH (64000) · Issue \#14690 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/14690](https://github.com/NousResearch/hermes-agent/issues/14690)  
18. models rejected with "context window below minimum 64000 tokens" — Telegram completely down · Issue \#24140 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/24140](https://github.com/NousResearch/hermes-agent/issues/24140)  
19. \[Bug\]: Infinite retry loop caused by stream stale timeout during local LLM prefill phase · Issue \#7069 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/7069](https://github.com/NousResearch/hermes-agent/issues/7069)  
20. Feature Request: Configurable Temperature Parameter for Model Inference · Issue \#17565 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/17565](https://github.com/NousResearch/hermes-agent/issues/17565)  
21. \[Feature\]: Configurable timeouts for auxiliary call\_llm and context compression · Issue \#3404 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/3404](https://github.com/NousResearch/hermes-agent/issues/3404)  
22. Custom OpenAI-compatible providers: temperature and parallel\_tool\_calls request fields not propagated · Issue \#18470 · NousResearch/hermes-agent · GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/18470](https://github.com/NousResearch/hermes-agent/issues/18470)  
23. \[Bug\] Kimi provider (\`kimi-for-coding\`) fails with HTTP 400 temperature error — need per-model temperature override support · Issue \#11765 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/11765](https://github.com/NousResearch/hermes-agent/issues/11765)  
24. hermes-agent/skills/mlops/models/audiocraft/SKILL.md at main \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/blob/main/skills/mlops/models/audiocraft/SKILL.md](https://github.com/NousResearch/hermes-agent/blob/main/skills/mlops/models/audiocraft/SKILL.md)  
25. custom provider: stale\_timeout\_seconds and HERMES\_API\_CALL\_STALE\_TIMEOUT are ignored — hardcoded 30s timeout · Issue \#25249 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/25249](https://github.com/NousResearch/hermes-agent/issues/25249)  
26. \[Feature\]: Managed Agent Runtime contracts on top of agent\_control / Kanban / SessionDB \#26675 \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/26675](https://github.com/NousResearch/hermes-agent/issues/26675)  
27. Feature request: Pre-response validation hook for capability claims · Issue \#22956 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/22956](https://github.com/NousResearch/hermes-agent/issues/22956)  
28. Config schema validation: missing Pydantic/schema enforcement · Issue \#27342 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/27342](https://github.com/NousResearch/hermes-agent/issues/27342)  
29. Extend base\_url validation to custom\_providers entries and STT/TTS config \#6566 \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/6566](https://github.com/NousResearch/hermes-agent/issues/6566)  
30. \[feishu\] Fix markdown table rendering: use post+tag:md instead of force-text workaround · Issue \#27529 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/27529](https://github.com/NousResearch/hermes-agent/issues/27529)  
31. \[Bug\]: honcho\_conclude tool schema uses anyOf at root, rejected by Anthropic API (HTTP 400\) · Issue \#10812 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/10812](https://github.com/NousResearch/hermes-agent/issues/10812)  
32. Bundle an html-artifacts skill for durable HTML deliverables · Issue \#26452 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/26452](https://github.com/NousResearch/hermes-agent/issues/26452)  
33. Signal gateway: MEDIA: tag attachments not extracted/delivered · Issue \#5105 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/5105](https://github.com/NousResearch/hermes-agent/issues/5105)  
34. Anthropic Claude subscription auth returns 'You're out of extra usage' in Hermes even after restart/re-login \#6475 \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/6475](https://github.com/NousResearch/hermes-agent/issues/6475)  
35. \[Bug\]: Kanban max\_runtime\_seconds does not align terminal command timeout · Issue \#26173 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/26173](https://github.com/NousResearch/hermes-agent/issues/26173)  
36. \[Feature\]: Cron job stage persistence \+ partial retry mechanism \- Real world case: 2 million tokens wasted due to push failures · Issue \#17071 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/17071](https://github.com/NousResearch/hermes-agent/issues/17071)  
37. gateway agent timeout (HERMES\_AGENT\_TIMEOUT) kills legitimate long-running tasks · Issue \#4815 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/4815](https://github.com/NousResearch/hermes-agent/issues/4815)  
38. \[Bug\]:Hermes reconnects after 180s of provider silence even though oMLX is still processing · Issue \#5889 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/5889](https://github.com/NousResearch/hermes-agent/issues/5889)  
39. is\_local\_endpoint misses Docker/Podman DNS names — stale timeout fires on local LLM proxies · Issue \#7905 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/7905](https://github.com/NousResearch/hermes-agent/issues/7905)  
40. Preserve partial stream content on retry instead of restarting from scratch · Issue \#5453 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/5453](https://github.com/NousResearch/hermes-agent/issues/5453)  
41. \[Bug\]: \`max\_history\_depth\` Not Enforced in Context Compression · Issue \#25538 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/25538](https://github.com/NousResearch/hermes-agent/issues/25538)  
42. bug: \`save\_config\` writes resolved plaintext back to config.yaml, destroying \`${ENV\_VAR}\` references and leaking secrets · Issue \#11551 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/11551](https://github.com/NousResearch/hermes-agent/issues/11551)  
43. \[Bug\]: Hermes rewrites raw config.yaml with expanded defaults and resolved env secrets \#4775 \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/4775](https://github.com/NousResearch/hermes-agent/issues/4775)  
44. Cron job fails to expand ${ENV\_VAR} from .env file · Issue \#15890 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/15890](https://github.com/NousResearch/hermes-agent/issues/15890)  
45. hermes-agent/website/docs/developer-guide/provider-runtime.md at main \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/blob/main/website/docs/developer-guide/provider-runtime.md](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/developer-guide/provider-runtime.md)  
46. Tool-layer env vars defeat config.yaml auxiliary routing for vision and extraction models · Issue \#14693 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/14693](https://github.com/NousResearch/hermes-agent/issues/14693)  
47. model picker and runtime named custom providers use different config sources · Issue \#7054 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/7054](https://github.com/NousResearch/hermes-agent/issues/7054)  
48. Telegram | Hermes Agent, accessed May 20, 2026, [https://hermes-agent.nousresearch.com/docs/user-guide/messaging/telegram](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/telegram)  
49. fix(gateway): PlatformConfig.from\_dict now maps top-level platform keys into extra by rainow · Pull Request \#10208 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/pull/10208](https://github.com/NousResearch/hermes-agent/pull/10208)  
50. Architecture: separate base vs effective context in overflow recovery · Issue \#9181 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/9181](https://github.com/NousResearch/hermes-agent/issues/9181)  
51. OAuth Anthropic \+ Pro/Max plan: mcp\_ tool-name prefix triggers "out of extra usage" 400 on every tool-bearing request · Issue \#28849 · NousResearch/hermes-agent \- GitHub, accessed May 20, 2026, [https://github.com/NousResearch/hermes-agent/issues/28849](https://github.com/NousResearch/hermes-agent/issues/28849)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABMAAAAaCAYAAABVX2cEAAAA7ElEQVR4XmNgGPFADYhnArEvklgJEpsowArE/4B4NhDzAbEdEP8H4hog/oykjigA0miDLsgAEa9CF8QHFjBANGEDIHGQq4kGIA34DCMJwAzrRZcgB3QzIAyE4RkoKkgEeQyYBt5CUUEmcGHAHY7c6ALIIBhdAAoWM2Aa5gPEF9DE4MAPiAvQBaGglAHTsPNAHIgmBgdngXgduiAU/GVARIIWA2pYroApQgYwSR408bUM2LMQuktRwBMgZgLiDwwQhe+h9AIkNTAAChKc4UUqOAfEQeiC5AKYF0Hhx4ssQQ5oBeL9DLiT0igYFgAANls5wU/MQWYAAAAASUVORK5CYII=>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAcAAAAcCAYAAACtQ6WLAAAAe0lEQVR4XmNgGOTAEYht0QVh4D8Qr0UXhAGQZAG6IAjoM0AkmZAFbYDYC4h3QyV9oXwwKALiEqjEWygfhFEASDIXXRAEdBkgkozoEiCwhgEiiRW8ZsAjCZLYhMTfhsQGS1pA2RlIbDAASYK8UwfEK5AlYADkeQl0wREPAGL/GMEfWDMiAAAAAElFTkSuQmCC>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAmwAAAA4CAYAAABAFaTtAAADWklEQVR4Xu3dO6gcVRgH8KOx8AG+UWy0ESsRFK1ECIidiDZRLEQFUSstBFHxgYVp0kYQBRsxkEILA0GRRHw1KmolVgZEFBVEBUnw+R1mJjn73dm9O5u7gcTfD/7snP/Mnrt3q8PJnUkpAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACcNK7PxSngu1wAAByPByP/9nkusm/m7Hqdm8ZnRp6KfBz5JPJF5Ms+y3gx8lIuwx25mOjR0s1xeuTP0n1Xi74nCzYAYMt8GLm4GV9YusXIidL+rHp8QRovq157ZeSi/jgb65b1e2Rb5PvIwb67pHRz1lcAgLUaW8g8kYs1qTtWt/bHV7UnwuuRF1I3T/4d8nh/ZG/qlvVu5NLIZaXb7WvVn1PnnufXXAAArKIuOu7N5QmSF1atRecWub9sfG8eTzG8d2xhVhdk7+Sy8UwuAABW8W3pFiVDzpo9PdddkW82yfbh4jnOy0Vj1UXWX+XYrt1g1bneKt0O2zx13qtzmdySCwCA43G4rL64mertXDTujhzJ5SYejrxRus9fX2v2RD6K/NRcN0Wd6/lcNpb5rpa5BgBgrmtyUWYXGJ82x1vtg1w06h2WD+VySXmB9FvkkdRVt+diRJ2r7rKNqTdn/Ji6sTnz5wEAmOTaNK7/HPpVf3x56RYb9XXMA6XbBVuU245ePe7sXIQzyuqLnPp4krEbA1r196mP/rgi9WMOlI3vH7T9OWX+nJt9BwAAc+0qGxcj7bguND5vxuvwT3O8PfJK6T7De5GXI6/148FpaZzlc3UhNXQ7Itf1x/m6Os7dYDh3fj9+PPLLsdNHjb3/sVwAAEzxZv9aHwj7WeSe5lz1d+meabZO9W/Vbs7linaW7jlprfoQ3mHB9WTf1YVXu1AcLLoBYnfpnr92Yz7RGJvzj1wAAGylYceo7iit09jO1CL35WKi9yN3Rl5N/ddpPNXYnHVHEABgberjPtZ508Gg7vBN8UMuJqp/w3aozP6XWDdFnm7GqzhUZud8tjkGADjp3ZCLU8DPuQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACA/6X/AN2mm1rSnySrAAAAAElFTkSuQmCC>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA0AAAAYCAYAAAAh8HdUAAAAcElEQVR4XmNgGN4gB4j/E8Br4KqhAtOBmAVJrAsqjhWsBeLn6IIMEA1x6IL4QD8DHltwAZgfSAIgDVPQBfEBPwaIJn50CXzgFgOZTutGF8QHYBGMDoTQBZABSEMSmth9IP6GJoaRVNDxYoTSUTCUAAAO7CZR5gc5ZAAAAABJRU5ErkJggg==>

[image5]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABgAAAAZCAYAAAArK+5dAAABJ0lEQVR4XmNgGAWDCbAA8Ukg/o+EcQGQnDe6ID4QzQDRpALlv4LyOeEqEGAJA37LMYARA0QDE5IYzCfYAEj8C7ogPvANiFXRxH4yYLeAnwEiHo4uQSrAFQfrGbCLkwxAhkxCF2SAiIN8RzZYxAAJX1g4vwPit0gYJJ4NV00iyATidUD8lwFiEIgNwmuAeDkQH4WKM8I0oAF2dAFcAGTIZXRBIPjEgD/88cnBgQQDRKE6ugQD7ogHAUEg/ocuiA3sYcBtCEgcJI8OYBbjcwAcgBScRRcEAm4GiFwDlB8G5cMAKN5AvsALtBggmrAptGZAuPAilBZAkifochBwYYDkVFwAFDygjOaLJg6yiKjwJxc0AfF0KHs7sgS1AKiYByXhA2jio2CkAAAxDU4iK2oQxQAAAABJRU5ErkJggg==>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABcAAAAZCAYAAADaILXQAAABGUlEQVR4XmNgGAUkAhcgFoGyGYG4D4irgFgYroJM8BaIF0LZn4D4HxD/R8KFUDmSgAADRLMulA9iuyGkGVKgYiBMMgBpCoCy7wHxZSQ5GID5QhxdAh94AcQfoGw9BogBoLBGB6DgAsmFokvgAgoMEA38UP5mIJaFy6ICUHiD1FqhS+ACHxmID8ddDMSrBQNSIgkW5kQDkGKQJmIASG09mhg7Gh8FEOvyGgbs6rCJwQExhrMyQNTAci0MCDIQ8PVdBojGO+gSUAAz2BpNHOYovI5jYkAo+A7E6lBxUFgug4rzQMXQwV8GiOvxAjYg/s2A6RpnZEVYAE4XUwpAZRHe8KYENAHxdCh7O7IENQALA6RIPoAmPgqGIwAAf9BHaBk1XaAAAAAASUVORK5CYII=>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABYAAAAaCAYAAACzdqxAAAAA3UlEQVR4XmNgGAUkAncgvoYuSA3wH4qpCpYy0MBgJiD+xEADg98AMQsDlQ3WBeJVUPYfBioajGwQKEVQxeBmIPZB4m9ggBgsgiRGFviBxu9mgBjsiCZOErjBAEkJyPgbA8TgPCR1MAAKfyF0QXQgBcQn0AWBQI8BYvAcdAkGIsMelyI2BojcaSSxV1AxvEmRA4ifM0C8jA2AIg2k+TeaeB8Q96OJwcE0IP4AxG8ZIAZ/QZVm+MuAkAeFNyhMzZHkCIYvOQBnEFACYFkdBK4gS1ADgILtMrrgKBgF2AEAXZk9I5SYYQoAAAAASUVORK5CYII=>

[image8]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAbCAYAAABFuB6DAAAAmklEQVR4XmNgGJqgGYj/APEvIPZEk4ODnUB8EYn/H4kNB9IMmBIgvhaaGFhwIxaxRGSBdKggG5IYI1QsA0mM4QdUEBmEQ8UkkAVBAjD8F40PB3ZQgTBkQagYisIadAEg4IKKlSMLToUKIoNAqJgUsmA3VBAZfATid2hiDN4MqAqF0PgoAFkCxPZH4qMAkARIwT8gVkWTGwUDAQAoOSvbufznagAAAABJRU5ErkJggg==>

[image9]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAG0AAAAaCAYAAAC939IvAAAEDklEQVR4Xu2ZXagWRRjH/x1U8AMzTFMQgkIshUgtytRauygwTEX8CJFCL/TGuzAtCNMiEMy00MtDV2k3SpGSCR4RFcQvEjLFm+hCBc2yNEuznj+zc868zzuz7868HyLuD/68Z/+z87Hz7M4+OweoqKi4N5mnjYogr2jjbvClaJo2C3hA9J82E3hO9LI2Ixgh2ip6Q9Qv917rK24Js0TrlTdA9LvyovkUphFOJPWX6FflfdV7di0zRXu12YBLaE3Qnkd60K6LDohGi54R/S1aidaMy3JH9KxoJOrbnSs6rrwkbIA0j8L4vuD4zi9iPML9xDIFaUFj3+9oE+FrTIE3wRrnmG1Pco6t94jyomEjR7SZ45vod0WnlNcItnE+/22WFxAftLcQ7vsGTJvN8jbq++DxR8pbBrOqJbMIpuFXdYEwEP6g8ZhLVFneFy0UbYepO7S2OJqpiA/aH6i/DstlbSTC9jd7vK+VR0JjKcWPCDewE6ZsjvJD54e4nf8yeKzLSW8GJj+xQePTxL6/1wUlGQRzE4dYBf+80PtYmzA+84IkfE8SmQH/ncOOfOeH+EH0UP73Api6y/uKk5iO+KAxO7TXavVNzRnFhObJ8i9MeZfjcXWh96TjWf4RfafNstjB/Ca6KrqZH58WDXfOs3yA4sG7PCw66hw/DVP3c8dL4UXEB43MRn3g+ASWge9jKgTbuiLqcXQr9338JPpZm2Ww77OluqCALxAeiIZ3n8tgmLpMuZvhJaQFzWUC+gKnGaKNBjwG086byqdnXw2aPfD33ZCziK/YjXJ1lsB8E11TYl1+C2qOof7dGSJDXNB8aT5hFsfxjHG8B2G+s2KYD9MONw8sXGXocYn08S3KzWMdoTutiNUoVyd0TqhPnxciQ/mgDRPt0mYOP651v5tQ/x5vhH1funA10Z7LGfhv3oaw0aJ12ge/6IsGQ/aLHtdmjg4ad1ysRz3hlIXIUD5oGxFO6bnSMFGyuOO46PhkIsJZL5dTPSc89uUEFuYO0R/078E0nJLJ6QG6bENxuQ4aeV10UnlFZCgfNJvVcUfGhRsEehzE5xHfuF1Y1j//m9uAG5wyHzy/7OsAn8F8aDJT5OPJ907sGs4OJyuP2zJslxnUn6hPQujzYtgnM1Vu99iPzhOIuADEBY2pNTkHM26Ojb+He8/og0tpaC64wcx3cgim9TawZXbzi26AtrBbdEibTRB7ARnKBy2GT0RbtOmwTxuJMGu/oM1OEDvRRdi2umvcMBnaEzSuDlwxuPvh26Zr1TWznVHa7AQfinZoM5FfYFL+snuSGdoTNP4PjONYq3yyTjROmwlwp6lHm53koOgpbXaADO0JWhFjtZFAF0wOcddZoY2KIIu1UVFRUVFRcT/xPxRxEcMpQtcYAAAAAElFTkSuQmCC>

[image10]: <data:image/png;base64,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>

[image11]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAFAAAAAaCAYAAAAg0tunAAACuUlEQVR4Xu2Yy8tNURjGH9ciueRSEiIpExkwkVtkKpmJMqIQuUUiLSTChCKllAnJH0AGX5/IH2DgMhBi4j6n8D7W2p9znrP2OWvvfTqfo/2rp+/sZ+29zzrPWftd73eAmpqa9mxUowdMMm0O6mtemqap2QPGmhaZrphGyljfcNZ0XM3AXdMv0zPTeBkryiHTTjUDDn0aICfNgJQx8P6ccDwqHM8cOiON26bv8NdSu5qHh3AYxgCXmXbAf7uZUnlguqSm8cj0XryLiIedyj8X4HPTK9M600LTLNMM05TGkzrADzVaTXj/qnjLg1+WrgXID3rLtEcHjJVq5MDJVK1J8xEPJHtctS7ODf4m8VPpSoBP8LceUJ+bh/FCjmO8QXzVFOU84gEugfcPiD89+EfET6VygHtNX+ALNGFB5k1nh+Ob8Nt6Jx6qURLeJxbgWnif822EpYH+dfFT4bW71Qw4JAQYm+wa02B4zXA7Mc+03bS0g1J4G6SwnnKu+8Rnn0j/tPip8NpY2SIOCQHmkS3tlEZ2FfwjtKGDUngNXw6UEfBzOio+nxT6W8RPJbaqMxwqBhhbnXncU6Mk900/1QxwPnm7cNFeMCO2qjMcKgT4Da07XjvYmHKVVIX9X94Xx2CfincYrecvNq0XLw9eu1/NgEOFALVhTYEfkO1GFbJaF2MFWsd4fDni6XkxsvrJZjyGQ4UAUyYQ44PpMfymMRnlAuV75008W3F34Ff9jebhP1yDb+i5ucXg/9Kf4BfJu/CX8+b9GnHIn0dbeNFHNQuyzXTMdBJ+hzzVPNwWvvcZNQvCfjJrzcriUDLAE6YLavaQcSj/BGToaiqDQ8kAv5omqtljBlDsB4hGuPJXq1kCh5IB/lBjmGBdmqBmAgvUKAhD46/S58LrvmarGj1gqulgUDdas5qampr/kt8RcZTxRoyDHAAAAABJRU5ErkJggg==>

[image12]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAJ8AAAAXCAYAAAAV+J1VAAAD6UlEQVR4Xu2aWahOURTHlyEZr6QMD+LBkCEUQvIgmVIelEjGKCKU4UnKlAylPMiLqZREIdI1hPAgD8hMpisv5jnztP537d23vnXPuffce8+93O/sX/07Z//X93337NU+e6+zzyUKBAKBQCAQCGSNnayPrD9Ou/Oiwi/KxaHh+eFABK8oP2eWR5Qfv5cfzhblJQqcYXW2Zh3nMusBq4kNpMRhkr+BnA42MdCQdceaWaMeq5h1iCRR4/PDpcQNykLgGOsDq70NVBPkrJE7fjcxsIA11ppZYxGrvzuPm/1+WqMA2U7Sz942UEXeu+MPkpxiptM8M+1M8lqdvyNJVJHyurI2qHahs4YkB6NtoBJgAC9x50NIfu98LlxK1E2eOXQSUNehrQvgvazmqp0V5pHkYqoNJOAgq4FqR60oF007c6DeO2o8myibtCyxjqT/TW2gAmzO1jvPz4ZzWSNz4Wyi6z3tIVGbXDuqWC50dpHUaj1sICG+3tPom/q5DvxjWpOMgSTq5r6TCm+s4fCJ6slaa2L/G2OsUQ2Ok9S9bWygEvRlLbMmc5Mkp13c8X+hI2tcQg1130mFuCScIolhM7Sm9sHSorpPjajNrrEesxqbWFVAGWOfbEEzkpxiVrQPH5kDCTptTUd9Klv7eQaxXrJWsT45T38Wdz3OMYvoGGZQLDejYjwwneSaLrBWO89/dgJrD+ub88FvFY+61vLAQHtKUvij9k2L8q7DX+8w5U1y3gHWVdZZ1kOSJ+YjrC8ks6XnBWs+6747AszUPgc7SPYuq5KTWgF3O14BXbIBxWeSjmv8pqlH14PaR2HtBx9ADK/lsER2iPHaOs9jz3u58xUkg1DHqsJ+a6TAQpLriRvM2L6Jut5pVLa/W9x5O8q/4fSeq/2tJyRP51Mo/U3zVEDSMfVjfw/1Dd7dRtGHcneWZyPlJ0KjE7GYyg4+i/X8rIbZCMJ1YY8R6M9iZq3ot2sbrCJ4R44aGsKN62dzi95X9Uxm3VVt3aeWpg2Ws65E+ADeUmsWAisp/m2HTgQ6f0K145Kk2ca6ZTxPkoHdSXl1jYmsG6qt+4s9Vt9u5c59bWpzCEoo2i8IdMewXEYlAjWLriejklGR152kSLc+BvZJ1UYMZcQI5SUB71WTqqbBUqn/yUD3t0i1N5M8BHq8P8Ad/cuBraxz7rygwJ2IpQXT/kzlzyb5L5HbrBkkicFTM+pGLDVvSWobEOWBFiS/jW2JWc7DS3/Upzhi4Pmlzc+s/VhfWftcOykDK6GaBEsu8oB8XKdcblASYcn1MfQRYHXALFnMmkPy8IdrRE7wHVBCufwGAoFAIBDIHn8B8ZcjGOtQlNEAAAAASUVORK5CYII=>

[image13]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAJYAAAAXCAYAAADp7bafAAACfklEQVR4Xu2au2sUURTGj1EkPkslhWARBKNooaC9hBjSBSzjA1NpIfgoBV8I6h9gE5KAIJLGQkJ8oI2FWCgIKgg+ChvF9wPfiX6f9w45c3Zmd2c3Snb3/OAjc79vWGZnzt579m5EHMdxHMdxnH/NMPQJ+h01mkoDkzKdU1vSsePkowsnixtQpzUbnDvQY2iBDZyZYQ40AV2UUFj96fgveQXXDIxDH6EOGzj1sQ/aGI/zZq1f1mhChiS8z3U2cGrjjTp+L6GwlipvFXRKjZud4xLuwVYbOMXQMxT7KI4fKe88tFiNW4U9Eu7FgA2cyrC/umQ8uxxmLY2twkkJ73+hDZzy6P5Ke7yZZ+L4h8pahRHoJ9RlA6c63lojksxaa6ATJptt9FqjDi5L6DOX2cApRt4yd01C9lRm/z7PC2sUZC50D3oGtZvMqYF50HVrRtqktNdK2Ay9go5Cn6Onzz0Uj/np1xlnvpdQT45Hdki4ppvQsegl526DzkHfo0+mVJ51reVgET2HbknoNZ0ZgJ/S19BtGyi+QF+NN1/SD1D3X9o/INOFRZjxpyAuWytyvOXRS7DHa+PxYQkFprNaGLOGUx+8oR8k7F+xn+BvgVmsh/Ya77SkZwyNfsD7pbSwLNZLZiPOIhSvi3toRJ/LGbHSazsNxhHJ34XXD/ggdEWNsx6+9c5CD4yXUE3RrlSe04Doh8wlLGl4tf9E0v2bLSJSyVsNLcrwWbRX1ZgZl/Zu5VVDXwE5/wHuwnOb4i60S/mDEv5b4CG0U8ID57dL9mlcdt9B2+O5WR5ZIuG170O7o8cfiNkP8i+LijmVzIgboG/QhTiulk0F5DiO4ziOU44/EsqrjTdMMp8AAAAASUVORK5CYII=>

[image14]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABoAAAAZCAYAAAAv3j5gAAAA/0lEQVR4Xu2UMWoCYRCFn0UuIIKp7TyGN7DxDiaiqNh5gRReQyzSBISU3kFQsBELRRQtbLUwzjCr7IzzaxbSBPaDx7Lf/5hZ2GWBlP9Ih/Jm5RNKlBnlh9I3Z4oB5QQpct718UPalHPsvgqZ8ZSki7hfdNyHcXckWVSG//RH+F6RZNEI/sAFfK9IsugAf+AUvldwoWZlAO56A8fwvYILdSsDrOEPnMD3Ci40rAwQekdz+F7BhaaVAbrwB/76q2tZGVGh5I3jftZxQ+MUOUipZw+IDPyXv4F8zldeIZ2XmLvxSdlRVpRldN1CfktxviD/Qsse0v+GLCno45SUlL/gAnq9R8qMXCmBAAAAAElFTkSuQmCC>

[image15]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABoAAAAZCAYAAAAv3j5gAAABO0lEQVR4Xu2UMUtCYRSG34IIabOhoM2tJcIlhAjCP1BRLSJu0RjRlJuTc/8hoi0IGhr6A7UECS3RFgoOLUENWef4He27b+c6hEtwH3iR85xzD+I9CGT8J2Yk15Ivya1kItkeybrkEeHZU+olWEAYylk9a/XkcCKdQ0kvqvcRnnV5k5yTu5O8k/PQpYuOa5Lro41dcsfmR7EBf+YDjl8zuUq+Zj5PPuYGzkLhGY4/MFkkv2N+hXzMK5yFQguOb5hcIr9pvkI+Rvu/Fgr3cPyeyWXy2+bL5GNe4CwUHuD4wTsqka+a19NPI+0dPcHx0yb/cnV1+DPu1SkqT8hdmY/RA5kjpzN8meouyfXxvr3WW1Gtf0nqeK6NcM4D5hFmpiKX4EzyaZ86qGfPXEiOWApdSQc/v0Ih2c7IyBgH39ySVXjPMtrtAAAAAElFTkSuQmCC>

[image16]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABoAAAAZCAYAAAAv3j5gAAABO0lEQVR4Xu2UTytFQRjGh5SkexcsKN9BUrqUbHwCwkayY2txNyytrG2trCQpKwtfwIKU8gFs7VjcUv4+j3nn9p7HOBfZqPOrp3PnN+975pw50w2h4j/Rj5whb8gF0lWcLmUHeUKekX2ZKzAS4gJ9Nh60cXe74msekHH7nfqYLC3kUNwl8ihOaSB3SM25iRAXunKuDSeWxG2ZL4Nbxpob8dm3mjE5LX7V/IB45Qipi8sutGEy7XNi0Ty35ydMhdjHByiwbROj4ufML4vvBHteVZK1ECfHxC+YnxVfxjHyojKRvhFf2bNinkf/O6wj9yo9vSHe8DenLjGJ3IrL9lLuijs17+EBGRI3jFyLI9r7Qe7pOZ53Y/4l0fm6Huc0566uwEGIH5JXFvLYKydI0433wucFUjZdXUVFxR/wDoHyU/O0IztrAAAAAElFTkSuQmCC>

[image17]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABoAAAAZCAYAAAAv3j5gAAABUklEQVR4Xu2UvSuFYRjGbxaJxUdRRgslySKSxSCL8rkgm9lgYRJ/gGKWssjEZPAPGNhlVmQxKGXxcV/nvs/T/V4eTsmi3l9dvd2/53rO+3bO8x6Rkv9Ek+ZC86G50tQVl2syonkW279La4kusUKjz20+16fGz1xqHsN8o9kIc+JFc0LuWvNKLseO2ENVaff5PLgEFhbIbbqvBToH5HporjAmVh4lv+K+lXxkQqwz6/NUWPvCmlh5kPy8+yHyEXzd6CxptjQtmjvNQ+gktsXK/eSn3S+Sj9yKdZ7Iw+2Tk1VfGCA/536cfASvATr4jAgcUqD6Gw2TX3aPo/8dh2KdbvLZGzW4/M2pmxTr9JLP3ghA7pHDe8BlHJAOcujgMLB7J1ch9/SYZ8KMv6Tckx5p3sLcLNbpDK7AsdgGXFHEsWfONOsslVOxPfd+7Ssul5SU/AWf/pFVSUdVwK4AAAAASUVORK5CYII=>

[image18]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABUAAAAYCAYAAAAVibZIAAAAU0lEQVR4XmNgGAWjYFCAvegC1AD/0AWoAWyAuAxdkBrgHBCbowsiAxMy8S0g3sdAZfAXiBnRBSkB/9EFKAUTgJgdXZBS8BtdgBrAAF1gFIwCGgIAYTgLotElupAAAAAASUVORK5CYII=>

[image19]: <data:image/png;base64,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>

[image20]: <data:image/png;base64,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>

[image21]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACYAAAAZCAYAAABdEVzWAAABsklEQVR4Xu2VPShGURjHHyUfKVEWm5ASg2JSJpkkWXyVCaWMNmUxKQYGE4uy2WQTm3yUokRkkmRQikIi/k/nHJ73cZ/rvokM91e/3vP8z7nvOfee9z2XKOX/0KGDCBp08JvUwSf/+QKbMrs/OINdOtQcwh04CgdgH+yFPV7JBLyDD3BQ9TFvsMq3S+EjfIVjsA0u+DHXfkwsPNDyVow7huuiPoJbomb4Gqsu95/8RBPBF7eQe/zVsNIrv7RY1QHOSny7zNcSXY9Tgi0M7OsAbMN6UR/Q10kYzhZVLZF1LiXcQotmuKKysLUanT/Dft9uhcOq70ckWUAgKud6Gd6IbBJ2ijprlryaqAUwVi7Jg1eiriX3r94U2bfwJBU6JHsBVi7h8ywg/0RF8ET0mQyRPYm1ACsPTMF2UV/AVVGfi7YJn8bWJPcU3ceZddcF8FJlPH5e1LOibRJ3990U3cdZow49fOJr9MLmRNskbmEM942IetpnUcyQOy40u3BN1Im2kieRP1RNIbkxe+Ter/xqyckY4eAttCbMp8+b4R8/v9b+jFMdKGrIHbYbuiMlJSUlS94BuQl7cpPHMhYAAAAASUVORK5CYII=>