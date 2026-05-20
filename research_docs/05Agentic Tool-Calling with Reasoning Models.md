# **System Architecture and Optimization of Local Deliberative Models in Agentic Tool-Calling Pipelines**

The integration of advanced deliberative models—such as Qwen 3.6 Coder, QwQ, and DeepSeek-R1—into local agentic workflows has introduced a fundamental shift in the computational linguistics landscape.1 Unlike traditional instruction-tuned models that generate immediate responses, deliberative architectures utilize a distinct cognitive phase to plan, self-correct, and evaluate subproblems before committing to an output.2 Implementing these systems within local environments using execution engines such as llama.cpp and Ollama requires resolving deep architectural conflicts.5 These conflicts typically arise when unconstrained thought generation meets the strict formatting rules of structured tool calling.6

## **Architectural Conflicts in Vocabulary-Constrained Grammars**

Integrating deliberative models into automated pipelines requires structured outputs, typically formatted as JSON schemas, to programmatically trigger external APIs or tools.9 Standard structured decoding frameworks enforce these structures by applying grammar-based token masking at the logit level.11 However, this method directly conflicts with the autoregressive execution path of deliberative models that rely on special tokens to trigger and bound their cognitive scratchpads.2

### **Mathematical Constraints of Vocabulary Masking**

Let ![][image1] represent the complete vocabulary of the language model. At decoding step ![][image2], the model produces raw logit scores ![][image3], which are converted into a probability distribution over the next token ![][image4] via the standard softmax operator:  
![][image5]  
In grammar-constrained sampling, a Context-Free Grammar (CFG) or Finite State Machine (FSM) defines a valid subset of the vocabulary ![][image6] based on the current parser state ![][image7].9 The probability distribution is modified by masking invalid tokens to negative infinity:  
$$P'(t\_k \\mid t\_{\<k}) \= \\begin{cases}  
\\frac{e^{z\_k(t\_k)}}{\\sum\_{w \\in V\_{\\text{valid}}(S\_k)} e^{z\_k(w)}} & t\_k \\in V\_{\\text{valid}}(S\_k) \\  
0 & t\_k \\notin V\_{\\text{valid}}(S\_k)  
\\end{cases}$$  
When a user requests a JSON response, a standard grammar initializes the root rule to match only the opening characters of a JSON object, such as a curly brace or whitespace.9 This structural constraint conflicts with the model's post-trained alignment, which assigns a high probability to the thinking start token (e.g., \<think\> or \<|channel\>) at the beginning of the response 2:  
![][image8]  
Because the grammar restricts the valid vocabulary ![][image9] to characters conforming strictly to a valid JSON schema, the probability of generating the starting deliberative token is forced to zero.6

### **Cognitive Degradation and State-Machine Deadlocks**

When a grammar-constrained sampler forces a deliberative model to bypass its thinking tokens and output structured JSON directly, two distinct failure modes occur:  
First, forcing the model to bypass its planning tokens degrades its cognitive performance.6 Because the self-attention mechanism cannot allocate autoregressive context to a scratchpad, the model's performance on logical operations, mathematical calculation, and complex code generation drops significantly.14  
Second, if the model's attention weights strongly favor thinking behavior but the grammar sampler restricts output to characters inside the JSON alphabet, the model enters a state of high perplexity.6 It repeatedly tries to emit reasoning tokens, which are masked out, forcing the sampler to select highly improbable tokens that fit the schema.6 This results in infinite loops of repetitive characters or a complete failure to generate content before hitting the token limit.6

| Failure Mode | Status Code | Mechanism | Root Cause |
| :---- | :---- | :---- | :---- |
| **Loud Failure** | 500 Failed to parse input | The model bypasses grammar constraints to output its reasoning trace but includes markdown code fences (e.g., \`\`\`json) before the JSON payload, which are then rejected by the client-side PEG parser.6 | The grammar engine does not track the thinking boundary, causing it to fail open when thinking is active.6 |
| **Quiet Failure** | 200 OK | The model generates unstructured text or arbitrary JSON keys that violate the specified schema.6 | The grammar engine becomes completely inactive when enable\_thinking is true, ignoring the requested schema.6 |
| **Indefinite Generation / Crash** | Core Dump | The server throws an exception: Unexpected empty grammar stack after accepting piece.6 | The grammar sampler attempts to resolve lazy triggers but gets stuck in a deadlocked state machine when evaluating boundary tokens.6 |

These issues occur because standard grammar samplers are not aware of the boundaries between thinking and non-thinking tokens.6 They apply constraints globally across the entire sequence instead of isolating the reasoning phase from the final structured response.6

## **Mechanics of Token Isolation and Context Preservation**

To run deliberative models in agentic tool-calling pipelines, inference engines must decouple the unconstrained thinking phase from the constrained tool-calling phase.2 This requires coordinated handling across the parser, the structured decoding engine, and the conversation history.2

### **Parser-Level Decoupling in vLLM and xgrammar**

Modern inference engines like vLLM isolate thinking traces by splitting the generation process into two distinct states.2 To support this behavior, vLLM utilizes model-specific parsers (such as deepseek\_r1 or qwen3) to track the exact boundaries of the thinking block.2 The structured output engine, powered by backends like xgrammar, registers a Reasoner class that defines the exact start and end tokens of the reasoning trace.2  
During generation, the engine dynamically determines whether to apply the grammar schema by calling internal state methods.21 While the model is within the thinking block (where the internal tracking variable reasoning\_ended is False), the engine bypasses grammar enforcement, allowing the model's reasoning capabilities to run unconstrained.2 Once the engine detects the end token, reasoning\_ended transitions to True.21 The engine then immediately activates the compiled Pushdown Automaton (PDA) in xgrammar to enforce strict schema compliance for all subsequent tokens.2  
A notable bug occurs in vLLM v0.19.0 when a user sets enable\_thinking: false on models that expect reasoning.21 Because the model never generates the start or end tokens, the system permanently flags that reasoning has not ended, disabling grammar enforcement for the entire response.21

### **Isolating Thinking Tokens from Tool-Call Parsing Engines**

In addition to isolating tokens during generation, runtime engines must also isolate them during parsing.2 In an agentic pipeline, deliberative models write mock code, structured examples, or draft tool payloads within their thinking phase.2 If a tool-calling engine scans the entire output stream indiscriminately, it will trigger false tool calls based on draft code written inside the thinking block.2  
To prevent this, production-grade runtimes process the output fields separately.2 The tool-calling parser is configured to parse functions and arguments exclusively from the content or response field, completely ignoring the thinking or reasoning\_content field.2 This strict isolation ensures that the agent loop only executes tool calls that have been finalized and placed in the designated response field.2

### **The preserve\_thinking Lifecycle and KV Cache Optimization**

In multi-turn agent loops, preserving historical thinking traces is critical for maintaining task consistency and optimizing inference performance.18 By default, standard chat templates strip the \<think\> blocks of past assistant turns before sending the prompt to save context window tokens.18 However, this practice degrades the model's capabilities in agentic workflows.18  
If prior reasoning blocks are stripped, the model loses access to its own intermediate steps and planning.18 In a subsequent turn, it may contradict its earlier plan or fail to remember dynamically generated values.18 Furthermore, when an engine strips the \<think\> block from an assistant turn, the prompt structure changes between turns.18 This invalidates the Key-Value (KV) cache prefix, forcing the engine to recompute the entire conversation history.18  
To resolve this, the preserve\_thinking configuration ensures that the \<think\> block is kept in the context history.18 The lifecycle requires coordinated steps across the agent loop:

┌────────────────────────────────────────────────────────────────────────┐  
│                              CLIENT TURN 1                             │  
│ User: "Perform step 1 of the task."                                    │  
└─────────────────────────────────┬──────────────────────────────────────┘  
                                  ▼  
┌────────────────────────────────────────────────────────────────────────┐  
│                            ASSISTANT TURN 1                            │  
│ reasoning\_content: "\<think\>Step 1 requires tool A...\</think\>"          │  
│ content: "\<tool\_call\>A\</tool\_call\>"                                    │  
└─────────────────────────────────┬──────────────────────────────────────┘  
                                  │  Client captures both fields  
                                  ▼    
┌────────────────────────────────────────────────────────────────────────┐  
│                              CLIENT TURN 2                             │  
│ User: "Here is the result of tool A. Now perform step 2."              │  
└─────────────────────────────────┬──────────────────────────────────────┘  
                                  │  Prompt formatted with  
                                  │  preserve\_thinking \= True   
                                  ▼  
┌────────────────────────────────────────────────────────────────────────┐  
│                          COMPILED PROMPT FOR TURN 2                    │  
│ \<|im\_start|\>user\\nPerform step 1...\<|im\_end|\>\\n                        │  
│ \<|im\_start|\>assistant\\n\<think\>Step 1 requires tool A...\</think\>\\n      │  
│ \<tool\_call\>A\</tool\_call\>\<|im\_end|\>\\n                                   │  
│ \<|im\_start|\>tool\\nresult\_of\_A\<|im\_end|\>\\n                             │  
│ \<|im\_start|\>assistant\\n                                                │  
└────────────────────────────────────────────────────────────────────────┘

By keeping the \<think\> blocks in the assistant turns, the prompt prefix remains unchanged.18 The engine can reuse the existing KV cache, which reduces latency and overall token recalculation costs.18

| Feature / Metric | preserve\_thinking: false | preserve\_thinking: true |
| :---- | :---- | :---- |
| **Prompt Serialization** | Prior assistant \<think\> blocks are stripped during prompt compilation.18 | Prior assistant \<think\> blocks are preserved in their original markup.18 |
| **Model Recall & Consistency** | High failure rate in multi-turn tasks; the model loses its previous reasoning paths.18 | High task consistency; the model can reference its prior steps.18 |
| **KV Cache Behavior** | Prompt variations invalidate the prefix cache, causing processing overhead.18 | The prompt prefix remains matched, enabling full KV cache reuse.18 |
| **Token Consumption** | Low initial token count, but requires re-processing the entire prompt context.18 | Gradual context growth, but avoids prompt re-processing overhead.18 |

## **Production Integration Blueprints and Agent Loops**

Developing robust, production-grade agent loops requires using runtime-specific APIs to isolate and preserve thinking traces.2 The following blueprints demonstrate how to implement these patterns in both Ollama and OpenAI-compatible endpoints (such as vLLM or llama-server).2

### **Ollama Native Agentic Tool-Calling Loop**

Ollama handles deliberative models by isolating the thinking trace into a distinct thinking field, keeping it separate from the final response content.7 The following implementation shows a multi-turn math agent that solves complex problems by calling tools iteratively.

Python  
import json  
from ollama import chat, ChatResponse

\# Define math tools  
def calculate\_sum(a: float, b: float) \-\> float:  
    """Add two numbers together.  
      
    Args:  
        a: The first number.  
        b: The second number.  
    Returns:  
        The sum of the two numbers.  
    """  
    return float(a \+ b)

def calculate\_product(a: float, b: float) \-\> float:  
    """Multiply two numbers together.  
      
    Args:  
        a: The first number.  
        b: The second number.  
    Returns:  
        The product of the two numbers.  
    """  
    return float(a \* b)

def run\_ollama\_agent():  
    model\_name \= "qwen3"  
    available\_tools \= {  
        "calculate\_sum": calculate\_sum,  
        "calculate\_product": calculate\_product  
    }  
      
    \# Initialize conversation history  
    messages \=  
      
    step \= 1  
    while True:  
        print(f"\\n--- \[Agent Iteration {step}\] \---")  
          
        \# Request generation with both tools and thinking enabled  
        response: ChatResponse \= chat(  
            model=model\_name,  
            messages=messages,  
            tools=list(available\_tools.values()),  
            think=True  \# Instructs Ollama to separate thinking tokens   
        )  
          
        \# Log the isolated thinking trace  
        if hasattr(response.message, 'thinking') and response.message.thinking:  
            print(f"\[\*\] Thinking Trace:\\n{response.message.thinking.strip()}")  
          
        \# Append assistant message to preserve history  
        messages.append(response.message)  
          
        \# Process tool calls if requested  
        if response.message.tool\_calls:  
            for tool\_call in response.message.tool\_calls:  
                func\_name \= tool\_call.function.name  
                func\_args \= tool\_call.function.arguments  
                  
                print(f"\[\!\] Executing Tool: {func\_name} with arguments {func\_args}")  
                  
                if func\_name in available\_tools:  
                    result \= available\_tools\[func\_name\](\*\*func\_args)  
                    print(f"\[=\] Tool Output: {result}")  
                      
                    \# Append tool result to the conversation history  
                    messages.append({  
                        "role": "tool",  
                        "tool\_name": func\_name,  
                        "content": str(result)  
                    })  
                else:  
                    messages.append({  
                        "role": "tool",  
                        "tool\_name": func\_name,  
                        "content": f"Error: Tool '{func\_name}' is not registered."  
                    })  
            step \+= 1  
        else:  
            \# If no tool calls are returned, the task is complete  
            print(f"\\n\[+\] Final System Answer:\\n{response.message.content}")  
            break

if \_\_name\_\_ \== "\_\_main\_\_":  
    run\_ollama\_agent()

### **OpenAI-Compatible Agent Loop with Thinking Preservation**

For runtimes like vLLM or llama-server, the OpenAI-compatible endpoint is used.1 The agent must capture the reasoning\_content field from the assistant's response and pass it back in the conversation history of the next request, while setting preserve\_thinking to true.2

Python  
import os  
import json  
from openai import OpenAI

def mock\_db\_lookup(user\_id: str) \-\> str:  
    """Retrieve system metadata for a given user identifier.  
      
    Args:  
        user\_id: Unique string identifying the user.  
    Returns:  
        A JSON string containing user privileges and registration date.  
    """  
    database \= {  
        "USR-901": {"clearance": "L3", "joined": "2024-05-12"},  
        "USR-102": {"clearance": "L1", "joined": "2025-01-09"}  
    }  
    return json.dumps(database.get(user\_id, {"clearance": "None", "joined": "None"}))

def run\_openai\_compatible\_agent():  
    \# Configure the client pointing to the local server  
    client \= OpenAI(  
        base\_url="http://localhost:8000/v1",  
        api\_key="none"  
    )  
      
    model\_id \= "Qwen/Qwen3.6-35B-A3B"  
    tools \=  
                }  
            }  
        }  
    \]  
      
    \# Initialize messages  
    messages \=  
      
    step \= 1  
    while True:  
        print(f"\\n--- \[API Iteration {step}\] \---")  
          
        \# Call the server, passing preserve\_thinking in the request body  
        response \= client.chat.completions.create(  
            model=model\_id,  
            messages=messages,  
            tools=tools,  
            extra\_body={  
                "chat\_template\_kwargs": {  
                    "preserve\_thinking": True  \# Enable thinking preservation   
                }  
            }  
        )  
          
        assistant\_message \= response.choices.message  
          
        \# Log the isolated thinking trace  
        if hasattr(assistant\_message, "reasoning\_content") and assistant\_message.reasoning\_content:  
            print(f"\[\*\] Thinking Trace:\\n{assistant\_message.reasoning\_content.strip()}")  
          
        \# Prepare the assistant message dictionary for history  
        history\_message \= {  
            "role": "assistant",  
            "content": assistant\_message.content or ""  
        }  
          
        \# Retain the thinking trace in the history dictionary  
        if hasattr(assistant\_message, "reasoning\_content") and assistant\_message.reasoning\_content:  
            history\_message\["reasoning\_content"\] \= assistant\_message.reasoning\_content  
              
        if assistant\_message.tool\_calls:  
            history\_message\["tool\_calls"\] \= \[  
                {  
                    "id": tc.id,  
                    "type": tc.type,  
                    "function": {  
                        "name": tc.function.name,  
                        "arguments": tc.function.arguments  
                    }  
                } for tc in assistant\_message.tool\_calls  
            \]  
              
        messages.append(history\_message)  
          
        \# Process tool calls if requested  
        if assistant\_message.tool\_calls:  
            for tool\_call in assistant\_message.tool\_calls:  
                func\_name \= tool\_call.function.name  
                func\_args \= json.loads(tool\_call.function.arguments)  
                  
                print(f"\[\!\] Calling Local Database: {func\_name}({func\_args})")  
                  
                if func\_name \== "mock\_db\_lookup":  
                    tool\_output \= mock\_db\_lookup(\*\*func\_args)  
                    print(f"\[=\] Database Response: {tool\_output}")  
                      
                    \# Append the tool output to the conversation history  
                    messages.append({  
                        "role": "tool",  
                        "tool\_call\_id": tool\_call.id,  
                        "name": func\_name,  
                        "content": tool\_output  
                    })  
            step \+= 1  
        else:  
            \# If no tool calls are returned, the task is complete  
            print(f"\\n\[+\] Final System Answer:\\n{assistant\_message.content}")  
            break

if \_\_name\_\_ \== "\_\_main\_\_":  
    run\_openai\_compatible\_agent()

## **Technical Conclusions and Best Practices**

To successfully deploy deliberative models in agentic tool-calling pipelines, technical teams should follow these best practices:

* **Isolate Boundaries with State-Aware Samplers**: Do not apply global grammar constraints to the entire output stream of deliberative models.6 Use runtime engines like vLLM with xgrammar that bypass grammar enforcement during the thinking phase and apply constraints only after the deliberative end token is detected.2  
* **Preserve Thinking Across Conversation Turns**: In multi-turn workflows, configure your engine with preserve\_thinking: true and ensure the client application captures and returns the reasoning\_content in the conversation history.18 This preserves the model's planning context and maintains KV cache alignment, reducing overall latency and token usage.18  
* **Enforce Field-Level Tool Parsing**: Ensure your tool-calling parser extracts functions and arguments exclusively from the designated content field, completely ignoring the thinking or reasoning\_content field.2 This isolates the deliberative trace and prevents draft code or formatted examples in the thinking block from triggering accidental tool calls.2

#### **Works cited**

1. Qwen/Qwen3.6-35B-A3B \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/Qwen/Qwen3.6-35B-A3B](https://huggingface.co/Qwen/Qwen3.6-35B-A3B)  
2. Reasoning Outputs \- vLLM, accessed May 20, 2026, [https://docs.vllm.ai/en/v0.9.1/features/reasoning\_outputs.html](https://docs.vllm.ai/en/v0.9.1/features/reasoning_outputs.html)  
3. Tools models \- Ollama, accessed May 20, 2026, [https://ollama.com/search?c=tools](https://ollama.com/search?c=tools)  
4. Agent Reasoning: The Thinking Layer | developers \- Oracle Blogs, accessed May 20, 2026, [https://blogs.oracle.com/developers/agent-reasoning-the-thinking-layer](https://blogs.oracle.com/developers/agent-reasoning-the-thinking-layer)  
5. Documentation: \`preserve\_thinking=true\` preserves reasoning ..., accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/22615](https://github.com/ggml-org/llama.cpp/issues/22615)  
6. Grammar enforcement not applied when thinking is enabled (response\_format \+ enable\_thinking) · Issue \#20345 · ggml-org/llama.cpp \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/20345](https://github.com/ggml-org/llama.cpp/issues/20345)  
7. Thinking \- Ollama's documentation, accessed May 20, 2026, [https://docs.ollama.com/capabilities/thinking](https://docs.ollama.com/capabilities/thinking)  
8. Feature Request: grammar / json schema with reasoning format. Allow model free to think but strict to answer. · Issue \#12276 · ggml-org/llama.cpp \- GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/issues/12276](https://github.com/ggml-org/llama.cpp/issues/12276)  
9. llama.cpp/grammars/README.md at master · ggml-org/llama.cpp · GitHub, accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/blob/master/grammars/README.md](https://github.com/ggml-org/llama.cpp/blob/master/grammars/README.md)  
10. Ollama tool calling | IBM, accessed May 20, 2026, [https://www.ibm.com/think/tutorials/local-tool-calling-ollama-granite](https://www.ibm.com/think/tutorials/local-tool-calling-ollama-granite)  
11. Structured Decoding in vLLM: a gentle introduction, accessed May 20, 2026, [https://vllm.ai/blog/2025-01-14-struct-decode-intro](https://vllm.ai/blog/2025-01-14-struct-decode-intro)  
12. llama.cpp/grammars/README.md · Steven10429/apply\_lora\_and\_quantize at main, accessed May 20, 2026, [https://huggingface.co/spaces/Steven10429/apply\_lora\_and\_quantize/blob/main/llama.cpp/grammars/README.md](https://huggingface.co/spaces/Steven10429/apply_lora_and_quantize/blob/main/llama.cpp/grammars/README.md)  
13. unsloth/gemma-4-31B-it-GGUF \- Hugging Face, accessed May 20, 2026, [https://huggingface.co/unsloth/gemma-4-31B-it-GGUF](https://huggingface.co/unsloth/gemma-4-31B-it-GGUF)  
14. DeepSeek-R1-Distill-Llama-70B: how to disable these  
15. Think Tool Boosts Accuracy by 54%\! (+ Ollama integration) : r/LocalLLaMA \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1jiwadm/think\_tool\_boosts\_accuracy\_by\_54\_ollama/](https://www.reddit.com/r/LocalLLaMA/comments/1jiwadm/think_tool_boosts_accuracy_by_54_ollama/)  
16. Benchmarking System Dynamics AI Assistants: Cloud Versus Local Large Language Models on CLD Extraction and Discussion \- arXiv, accessed May 20, 2026, [https://arxiv.org/html/2604.18566v1](https://arxiv.org/html/2604.18566v1)  
17. How to use lazy grammars? · ggml-org llama.cpp · Discussion ..., accessed May 20, 2026, [https://github.com/ggml-org/llama.cpp/discussions/12110](https://github.com/ggml-org/llama.cpp/discussions/12110)  
18. PSA: Qwen3.6 ships with preserve\_thinking. Make sure you have it on. \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/LocalLLaMA/comments/1sne4gh/psa\_qwen36\_ships\_with\_preserve\_thinking\_make\_sure/](https://www.reddit.com/r/LocalLLaMA/comments/1sne4gh/psa_qwen36_ships_with_preserve_thinking_make_sure/)  
19. Reasoning Outputs \- vLLM, accessed May 20, 2026, [https://docs.vllm.ai/en/stable/features/reasoning\_outputs/](https://docs.vllm.ai/en/stable/features/reasoning_outputs/)  
20. backend\_xgrammar \- vLLM, accessed May 20, 2026, [https://docs.vllm.ai/en/stable/api/vllm/v1/structured\_output/backend\_xgrammar/](https://docs.vllm.ai/en/stable/api/vllm/v1/structured_output/backend_xgrammar/)  
21. \[Bug\]: \`--reasoning-parser gemma4\` silently disables structured ..., accessed May 20, 2026, [https://github.com/vllm-project/vllm/issues/39130](https://github.com/vllm-project/vllm/issues/39130)  
22. Qwen3.6 preserve\_thinking still fails · Issue \#3479 · earendil-works/pi \- GitHub, accessed May 20, 2026, [https://github.com/earendil-works/pi/issues/3479](https://github.com/earendil-works/pi/issues/3479)  
23. Qwen3.6-Max-Preview: Smarter, Sharper, Still Evolving, accessed May 20, 2026, [https://qwen.ai/blog?id=qwen3.6-max-preview](https://qwen.ai/blog?id=qwen3.6-max-preview)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABEAAAAZCAYAAADXPsWXAAAAo0lEQVR4XmNgGAXYACMQfwDi/0j4LYoKCPjLgJAHsbGC+QwQBQ5o4sgAJI8XJDBAFFWjicPARiA2RhdEB8oMEEO2oUsAARcQP0MXxAVAhnxEFwSCX+gC+AAs4JBBMhDXoInhBdgMQecTBOiGXANiUSQ+UeA7A8IQUEAfQZIjGsxjgBjiB8T30OSIBgkMmF4iGSgyQAxIR5cgFZxGFxgFo4AaAADynCc6DrNJsgAAAABJRU5ErkJggg==>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAsAAAAbCAYAAACqenW9AAAAo0lEQVR4XmNgGNrgKhD/AeL/QMyJJocV7GWAKCYKgBSSpHg6uiA2IMUAUSyBLoENzGJAdUIbED9FE4MDZPceAmI+IF6FJIYCQIJzgfgSELNCxeYB8T24CiiQZECYnIMmhwEiGCAKQREDovegSqOC6wyobgOxpyDxUQBI8hoafyWU/RFJHAxAkmFo/GwgZgTiY0jiDGJQSWTgBxX7gCY+CgYbAADqfCrdk3T3XwAAAABJRU5ErkJggg==>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAE8AAAAaCAYAAAD2dwHCAAACjklEQVR4Xu2YS8hNURiGP6SYkAwopVDKwAQTTJUBUS65ZaAYoAwUBlKiXAcSE0yYyAhTU5cJYuCS24SIgbuIXN+39a3///Z39vn32rvf6XT+9dTb+va7Vvvs3r322msfkd7nlDcSeeyNochpU5+B/kI3oOnGv6z+FeM9MfWQxYZHGNIM522CRjkvhyfl4W103gV3THomvNXQJegctA26Bl2EjkE/oU/Qs77RRXx4v6Cz5viBqS09E17kgLZvC67IH21fQSNth7SGdx16qvUUaLbps3RleKMlPDabjVI5ou2HghtmZeSqqYkP75CER5cw7HZ0VXhrJVw0w5oJTYYmQuPtoApieB8LrsheU783NfHhLZZwHSec76kd3lzoPDRcj9lu6e9uzB1oqzcb0C48rn+E13vSdkhreGMlhLfD+Z5a4W2QcNKoRdCPwohmcE3Z582GxPD4gogwMIa5EPpq/IgPj/z2Rgm1wvNvK77FxjmvCTzPYGHDizf5C3QXmhYHOcrCS6FWeJbb0CTnHZf+hbYODG9OhVLxM4/bDvJO2zI6Gt4LaIw3labhLalQKj68qdAere9r6+lYeFw7RnhTWQk98mYCKetLKoe1tWveLQnrHjfSC4zfUb55Q4ozjTvxNVpzjfls+gZiObTdmw05qK0Nj3DdI9z8trv5/42XEsJgWK8lPAKs15sxPOYd3g3N1+NUODuWerMmw6BdWnNpscyD1mk9mDO9EgbEbQphOPxxBrOsb0SA3k1p/fxJhaF/l/D4T5DWfzEG4rmETzC7leJ1HjVj7rm+rmEF9FDC3a8z48qYBe2U8FXA/d9+CeftWbjGrdI6hvdG20wFdrZxtnAf2PTxzWQymUwmk0nkH78AmBGsQLzOAAAAAElFTkSuQmCC>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAbCAYAAAB1NA+iAAAAxUlEQVR4XmNgGAVUB45AbIsuSAr4D8Rr0QWJBUwMEAMM0SWIBdkMEANIBiBN2DDJgCL/MzJADNBDlwCCWUB8Cogt0SWQQQ4DfmfjkwODNQz4FeGTAwOQgnfoglAgxkCkASVI/CNI7KlAPBPKPgvE95Dk4ABkgAqU/RNZggEiJw3E64GYBcrHAD0MEIkfDBBFyAAk/hqIBdHEiQKiDAgbQTQ7khxRoB+Ip0PZIANArjuPkCYMQDEDc3ogEJ8GYlmE9CgYHAAA6sYtEmIsEl8AAAAASUVORK5CYII=>

[image5]: <data:image/png;base64,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>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAH0AAAAaCAYAAACacVPHAAAET0lEQVR4Xu2ZWagURxiF/4jEfUNFQZIrLiCIPrgvuIL7gw8qaB5yFRUURQXFJXEJSBIIgkhQFAT3B3EBQUXUJxURcQc1EsTrEkGJiTGJWyL6H6qLqTnT+7R3rtofHKb7/NXVNVXV1VXVIjk5OTl1mdFsZMhUNnIqzxzVcjaJeqrvVItUbTyvkaqpTRBCJ9VVNj8mPlM9Vb119KQoheGNFOI4rhRfqB6ySZxS/aPqo6pSXVDtFVP2uGxQbWEzBZ+rGpPQ+eJQI8Xt8p9qiZtAWevFrM4Xh8PZJuai4eS7JKm09wXK0JBNhzOq+2yK6chJy580vWW/mGs3qr5XrVKtUC1VLVYtLCSNhW3QIHAPNH5ipovJ+FvyLYdUvdmsZQapXrHpgFEL/wFDO7NMdZzNCHZJ8mEe969is0yiGj31yNtZTMZHOSBmSIoaUmsDDG9h73I8RfgPaHxmvqovmxG0kPDKZmrEDOdZE9boNarmbCYBGf/FpvKajQqB8oW9D/E0Iw3e535PexqQX9xK3cNGRuCB82v07qpjbCbFr0fNVK0krxI0k9Ky+WH/g9VtMSNVWpAH3sdRTFQ1YTMj9okpRw/y49RHJH6NzueVYoTEK0t7KV2NQH5Dfhxw7Q42ffiBjQzBxA/lmOt428XUSdlwo99QtXXO04CJIfIc5Z13U/1fCBexU4IbdoYEx4LAsPxSzHXzHB/H8CY5XhAvVOfY9GGwvL9NHSw9Ud7d3jnmDX7L6lTgD9qKxcQOy58suC6FRscQeMSJMUENWy3BMRC2ZMF1W328OPyrushmAHHzTAPyvusdP5f0I1cJJ8Vk/qX3mxVY9thGjyLovgMkOAaiYgN9vDgg3UE2A+ipesBmRqAc0DTVjxQrizViMsbmxjiKAXtjW2H2uIMnPNGY9GDEcJcubqO71wP0WKy98SReophLKwmOtZTgWLWUxqaobnnHJ1SnnRiDa79hMwTbOcdyoEy47jPDTpb+5IAD4h2941mOjz+JkQKgx7ubKPykuwXHcQM6DwIxv3XwOjGxzeR38XxM7lzQOb9SnfXOo+6JDpeUoWLq446YYRm656mmkCw2tsH5v5RNfTEZh70vvpZCof9wfIACYa36mxRXJBrd/SpmY3YHzYXPXRDDBgyDiSE6A/bLkQY7VBDex36dxKbBh5UwMP8IK09tgo0pfFOoGKgIbH70crzDYmb7AF+yuNHHOOc2xukAn7tg7xobL+Vi7/FYNcQNEBg5DrD5qXJN9Yw8VGSVdzzZO7dfqdAZ3DmC27A4djdPwhodII4RKS3YRLnpHV9WDVOtL4SLiCoLMyGmxtsLPiRaS+mWI4Z2vMexvBmpeqT6WXVFzGsA8wQ0/N+q38UMvQANCA8Tqk1iKjqssjF3+JXNBGAmjoYGXVW/iP+kdbWYr1dJ6B9T/ewFOfH5STWbzQxpJ6Yz5NQxqtnIkAVs5OTk5OTk5OTUNd4BanIePHXkdAYAAAAASUVORK5CYII=>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABYAAAAaCAYAAACzdqxAAAABA0lEQVR4XmNgGAVEADUgngnEvkhiJUhskgErEP8D4tlAzAfEdkD8H4hrgPgzkjqSAcgQG3RBBoh4FbogsWABA8QAbAAkDvINWQCkGZ/BZAOYwb3oEpSCbgaE4TA8A0UFBSCPAdPwWygqqABcGLCH+zQsYjhBMLoAFCxmwDREBIsYVuAHxAXoglBQyoBpyFQGIsP+LBCvQxeEgr8MmIaALJIEYk4gPg7ED1GlEQAWjjxo4msZsGdjkFphIHYD4mogfo0qjQBPgJgJiD8wQDS9h9ILkNTAgCgDRA4U9lQFUxggqQJUQKGHPUUAVPJJQNkwgzdDaYoAsiu3AvENJP4oGAV4AACdfEA/mlvUsgAAAABJRU5ErkJggg==>

[image8]: <data:image/png;base64,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>

[image9]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAFEAAAAaCAYAAADPELCZAAADOUlEQVR4Xu2YW6gNURjHP5I7EaJcDqE8ybXwIJTbgxd5tgmleFByi1zyJg8kt1JKefCokPAkT26heJFSIsr9fo3vb63V/vb/zMyaffacs2s3v/p3Zv2/WTPffHtdZo5ISUlJYyxioyCmsNGqrFdtZ5Portqr2qQa6r0+qv7hhAz+shGjm+q9uI5Bb2rOcPyRahzHzWK06gWbxHXVZ9UMVZvqtuqc5C/OONU7NvNwWtxN5pFvyZtEZ4IcerNpuKF6xqa4gVFP/l9Uy9iMsUrcTXaSHzivms5mFzNH9YNNA2YVngFTmdmmusJmBhOlvqL/Z7y4Tpc4oPSV+BTqCn5J9lq4WdwzoJjMRtVMNiPgWnj2ukCnD2wqP9loEsgPm0MaGG04B+th0misF1zrAJsx0ImH8BrVLvKawQBpn1sS4RmCnkgHRpMH0z9r+UgkqYjcbhbzJV8uI6T92waUNMVjnJB896yBi/hINcy0OwI2KlxzoW9PUv2uhms4I+lJr5b0WBoDVd/F9dtAsamqm+QxYXmoi29S7YSNBq8LRfBQqkXsp7poYkxa0hVJj4H9bBjQ75Rp71YdlXgRt0j2PRO5Jq7TGP+3KO5LtYgx0u47S9JjIBabTd4+iRfxuGRfN5E94jrhZXUpxQBiQbY90gsjboe4Ed3TnwNsEW1/gLUKizdGyl2KWQZLemyQpMcqkhzLU8TLqq9sxgiLd9YnD+Jj/fFa4y8RN5LBZKnd1Xgk2ofCcS9qp4GY/XECB8XFsBFYJngfmw2DIt5ik0DfQ2zG6CGuY9ZOtlL11B+/NT5AsmdVz6W2GCii/a9LiIUvDAu3LYjhhZrBRoXiHhZ3Dr7tIXy6JRUdoIh32CRwLcyATgEXx8vsNONdELebA/ynhIu42LRDjM8D3LZsFfciXQQoIpaPNEZJdi4N80D1kTzcsM0fr/Dtk76N4to11iaHY/syHEscccyYRkER77FpwJK2nM0iGSJu2lowlbEOYoosUL1SHRGXKKY9kkIhP6lei5tqAAWBd1V1TFyRsgqJtfcxm3WCeyMn5IHPXP5Rhqtektdy4Ht2HZsFkvUjthQVNgpiLhslJSUlJfn5BwiC13nKx+8YAAAAAElFTkSuQmCC>