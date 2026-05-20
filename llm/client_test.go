package llm

import (
	"testing"

	"archangel/config"
)

func TestStripThinking(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple thinking block",
			input:    `<think>Let me analyze this code.</think> {"tool": "read_file", "path": "main.go"}`,
			expected: `{"tool": "read_file", "path": "main.go"}`,
		},
		{
			name:     "multiline thinking block",
			input:    "<think>\nI need to check the config.\nLet me look at the imports.\n</think>\nHere is the result.",
			expected: "Here is the result.",
		},
		{
			name:     "no thinking block",
			input:    `{"tool": "read_file", "path": "go.mod"}`,
			expected: `{"tool": "read_file", "path": "go.mod"}`,
		},
		{
			name:     "multiple thinking blocks",
			input:    "<think>First thought.</think> action_1 <think>Second thought.</think> action_2",
			expected: "action_1  action_2",
		},
		{
			name:     "empty thinking block",
			input:    "<think></think> do_something",
			expected: "do_something",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripThinking(tc.input)
			if got != tc.expected {
				t.Errorf("StripThinking(%q)\ngot:  %q\nwant: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestExtractThinking(t *testing.T) {
	raw := "<think>Step 1: analyze dependencies.</think> Call read_file <think>Step 2: check tests.</think> Call run_tests"
	thinking := extractThinking(raw)

	if thinking == "" {
		t.Fatal("expected non-empty thinking content")
	}

	if len(thinking) < 20 {
		t.Errorf("expected substantial thinking content, got %q", thinking)
	}
}

func TestDoomLoopDetection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DoomLoop.ToolHistoryWindow = 3
	cfg.DoomLoop.HardIterationLimit = 5

	client := NewClient(cfg)

	// First call — no warning
	w1 := client.checkDoomLoop([]ToolCall{{Name: "read_file", RawJSON: `{"path":"a.go"}`}})
	if w1 != "" {
		t.Errorf("expected no warning on first call, got %q", w1)
	}

	// Second identical call — no warning yet (window not full)
	w2 := client.checkDoomLoop([]ToolCall{{Name: "read_file", RawJSON: `{"path":"a.go"}`}})
	if w2 != "" {
		t.Errorf("expected no warning on second call, got %q", w2)
	}

	// Third identical call — window is full, should trigger warning
	w3 := client.checkDoomLoop([]ToolCall{{Name: "read_file", RawJSON: `{"path":"a.go"}`}})
	if w3 == "" {
		t.Error("expected doom-loop warning on third identical call, got empty string")
	}

	// Different call should reset
	w4 := client.checkDoomLoop([]ToolCall{{Name: "write_file", RawJSON: `{"path":"b.go"}`}})
	if w4 != "" {
		t.Errorf("expected no warning after different call, got %q", w4)
	}
}
