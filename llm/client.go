package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"archangel/config"

	"github.com/rs/zerolog/log"
)

var thinkRegexp = regexp.MustCompile(`(?s)<think>.*?</think>`)

// ToolCall represents a parsed tool invocation from the model's response.
type ToolCall struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Args     map[string]interface{} `json:"arguments"`
	RawJSON  string                 `json:"-"`
}

// ChatMessage represents a single message in the conversation history.
type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	Thinking  string     `json:"-"` // Preserved reasoning block, excluded from JSON marshaling to API
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// CompletionResponse wraps the parsed output from a model completion.
type CompletionResponse struct {
	Content     string     `json:"content"`
	Thinking    string     `json:"thinking,omitempty"`
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed  int        `json:"tokens_used"`
	FinishReason string   `json:"finish_reason"`
}

// DoomLoopState tracks repetitive tool invocations for loop mitigation.
type DoomLoopState struct {
	mu           sync.Mutex
	history      []string // serialized tool call signatures
	windowSize   int
	warningCount int
}

// Client manages HTTP API interactions with a local or remote LLM backend.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cfg        *config.Config
	doomLoop   *DoomLoopState
}

// NewClient creates a new LLM API client from the daemon configuration.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.Model.APIHealthURL, "/api/tags"),
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.Timeouts.TaskTimeoutSec) * time.Second,
		},
		cfg: cfg,
		doomLoop: &DoomLoopState{
			history:    make([]string, 0),
			windowSize: cfg.DoomLoop.ToolHistoryWindow,
		},
	}
}

// chatCompletionRequest is the OpenAI-compatible request payload.
type chatCompletionRequest struct {
	Model              string                 `json:"model"`
	Messages           []ChatMessage          `json:"messages"`
	Temperature        float64                `json:"temperature"`
	TopP               float64                `json:"top_p"`
	ParallelToolCalls  bool                   `json:"parallel_tool_calls,omitempty"`
	Stream             bool                   `json:"stream"`
	ChatTemplateKwargs map[string]interface{} `json:"chat_template_kwargs,omitempty"`
}

// chatCompletionResponse is the OpenAI-compatible response payload.
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete sends a chat completion request to the LLM backend.
// It handles thinking block extraction and doom-loop detection on the response.
func (c *Client) Complete(messages []ChatMessage) (*CompletionResponse, error) {
	endpoint := c.baseURL + "/v1/chat/completions"

	reqBody := chatCompletionRequest{
		Model:             c.cfg.Model.DefaultModel,
		Messages:          messages,
		Temperature:       c.cfg.Model.Temperature,
		TopP:              c.cfg.Model.TopP,
		ParallelToolCalls: c.cfg.Model.ParallelToolCalls,
		Stream:            false,
	}

	// Reconstruct Assistant thinking history if preserve_thinking is enabled (Doc 03 / Doc 05)
	if c.cfg.Agent.PreserveThinking {
		localMessages := make([]ChatMessage, len(messages))
		copy(localMessages, messages)
		for i, msg := range localMessages {
			if msg.Role == "assistant" && msg.Thinking != "" {
				localMessages[i].Content = fmt.Sprintf("<think>\n%s\n</think>\n%s", msg.Thinking, msg.Content)
			}
		}
		reqBody.Messages = localMessages
		reqBody.ChatTemplateKwargs = map[string]interface{}{
			"preserve_thinking": true,
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal request: %w", err)
	}

	log.Debug().
		Str("endpoint", endpoint).
		Int("message_count", len(messages)).
		Msg("Sending chat completion request to LLM backend.")

	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm: backend returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("llm: failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty response from backend")
	}

	choice := apiResp.Choices[0]
	rawContent := choice.Message.Content
	thinking := choice.Message.ReasoningContent

	if thinking == "" {
		thinking = extractThinking(rawContent)
	}
	executableContent := StripThinking(rawContent)

	// Parse native tool calls from the API response
	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		toolCalls = append(toolCalls, ToolCall{
			ID:      tc.ID,
			Name:    tc.Function.Name,
			Args:    args,
			RawJSON: tc.Function.Arguments,
		})
	}

	result := &CompletionResponse{
		Content:      executableContent,
		Thinking:     thinking,
		ToolCalls:    toolCalls,
		TokensUsed:   apiResp.Usage.TotalTokens,
		FinishReason: choice.FinishReason,
	}

	// --- Doom-Loop Detection ---
	if warning := c.checkDoomLoop(toolCalls); warning != "" {
		log.Warn().Str("warning", warning).Msg("[Doom-Loop Detected] Injecting ephemeral break reminder.")
		result.Content = result.Content + "\n\n[SYSTEM WARNING: " + warning + "]"
	}

	return result, nil
}

// StripThinking removes <think>...</think> blocks from raw model output,
// returning only the executable/tool-calling content.
func StripThinking(raw string) string {
	return strings.TrimSpace(thinkRegexp.ReplaceAllString(raw, ""))
}

// extractThinking pulls the content inside <think>...</think> blocks.
func extractThinking(raw string) string {
	matches := thinkRegexp.FindAllString(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	var parts []string
	for _, m := range matches {
		inner := strings.TrimPrefix(m, "<think>")
		inner = strings.TrimSuffix(inner, "</think>")
		parts = append(parts, strings.TrimSpace(inner))
	}
	return strings.Join(parts, "\n---\n")
}

// checkDoomLoop evaluates the sliding tool call history window for repetitive patterns.
// Returns a warning string if a loop is detected, empty string otherwise.
func (c *Client) checkDoomLoop(toolCalls []ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	c.doomLoop.mu.Lock()
	defer c.doomLoop.mu.Unlock()

	for _, tc := range toolCalls {
		sig := tc.Name + ":" + tc.RawJSON
		c.doomLoop.history = append(c.doomLoop.history, sig)
	}

	// Trim history to window size
	for len(c.doomLoop.history) > c.doomLoop.windowSize {
		c.doomLoop.history = c.doomLoop.history[1:]
	}

	// Check if all entries in the window are identical
	if len(c.doomLoop.history) == c.doomLoop.windowSize {
		allIdentical := true
		for i := 1; i < len(c.doomLoop.history); i++ {
			if c.doomLoop.history[i] != c.doomLoop.history[0] {
				allIdentical = false
				break
			}
		}
		if allIdentical {
			c.doomLoop.warningCount++
			if c.doomLoop.warningCount >= c.cfg.DoomLoop.HardIterationLimit {
				return fmt.Sprintf("HARD STOP: %d identical tool calls detected across %d consecutive warnings. Execution halted.",
					c.doomLoop.windowSize, c.doomLoop.warningCount)
			}
			return fmt.Sprintf("Repetitive tool call detected (%s) %d/%d times. Try an alternative approach.",
				c.doomLoop.history[0], c.doomLoop.warningCount, c.cfg.DoomLoop.HardIterationLimit)
		}
	}

	// Reset warning count on non-repetitive calls
	c.doomLoop.warningCount = 0
	return ""
}

// ResetDoomLoop clears the tool call history buffer.
func (c *Client) ResetDoomLoop() {
	c.doomLoop.mu.Lock()
	defer c.doomLoop.mu.Unlock()
	c.doomLoop.history = make([]string, 0)
	c.doomLoop.warningCount = 0
}
