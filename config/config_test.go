package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Model.Temperature != 0.20 {
		t.Errorf("expected default temperature 0.20, got %f", cfg.Model.Temperature)
	}
	if cfg.Model.CtxCheckpoints != 4 {
		t.Errorf("expected default ctx_checkpoints 4, got %d", cfg.Model.CtxCheckpoints)
	}
}

func TestLoadConfig(t *testing.T) {
	// Write a temp config file containing environment variable references
	os.Setenv("ARCHANGEL_PORT", "9999")
	defer os.Unsetenv("ARCHANGEL_PORT")

	tempYAML := `
server:
  port: ${ARCHANGEL_PORT}
  log_level: "debug"
model:
  temperature: 0.25
`
	tmpFile, err := os.CreateTemp("", "archangel_test_*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(tempYAML)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.Port != 9999 {
		t.Errorf("expected environment variable expanded port 9999, got %d", cfg.Server.Port)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Errorf("expected log_level 'debug', got %s", cfg.Server.LogLevel)
	}
	if cfg.Model.Temperature != 0.25 {
		t.Errorf("expected temperature 0.25, got %f", cfg.Model.Temperature)
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		url      string
		expected bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:8000/v1", true},
		{"http://ollama:11434/v1", true}, // Unqualified domain
		{"http://hermes-litellm/v1", true}, // Unqualified domain
		{"http://google.com/v1", false}, // External
		{"http://192.168.0.1:11434", true}, // RFC 1918 Private IP
		{"http://10.25.0.44:8080/v1", true}, // RFC 1918 Private IP
		{"http://172.22.1.25/v1", true}, // RFC 1918 Private IP
	}

	for _, tc := range tests {
		result := cfg.IsLocalEndpoint(tc.url)
		if result != tc.expected {
			t.Errorf("IsLocalEndpoint(%q) = %t; expected %t", tc.url, result, tc.expected)
		}
	}
}

func TestCalculateCompressionThreshold(t *testing.T) {
	// Standard threshold check (70% of 32768)
	val1 := CalculateCompressionThreshold(32768, 0.70)
	if val1 != 22937 {
		t.Errorf("expected 22937 for 32K context, got %d", val1)
	}

	// Safety floor check for exactly 64,000 threshold locks
	val2 := CalculateCompressionThreshold(64000, 0.70)
	if val2 != 44800 {
		t.Errorf("expected 44800 for 64K context, got %d", val2)
	}

	// Sane minimum boundary check (should not go below 4096)
	val3 := CalculateCompressionThreshold(8192, 0.10)
	if val3 != 4096 {
		t.Errorf("expected floor minimum 4096, got %d", val3)
	}
}

func TestModelsProfiles(t *testing.T) {
	tempYAML := `
profiles:
  - id: "test-model"
    name: "Test Model Coder"
    description: "Testing profiles integration"
    model_path: "models/test.gguf"
    parameters:
      temperature: 0.15
`
	tmpFile, err := os.CreateTemp("", "models_test_*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(tempYAML)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	profiles, err := LoadModelsProfiles(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to load model profiles: %v", err)
	}

	if len(profiles.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles.Profiles))
	}

	p, found := profiles.GetProfileByID("test-model")
	if !found {
		t.Fatalf("expected to find profile 'test-model'")
	}

	if p.Name != "Test Model Coder" {
		t.Errorf("expected name 'Test Model Coder', got %s", p.Name)
	}

	// Test AddOrUpdate
	newProfile := ModelProfile{
		ID:          "test-model",
		Name:        "Test Model Updated",
		Description: "Testing update",
		ModelPath:   "models/test-updated.gguf",
		Parameters:  map[string]interface{}{"temperature": 0.20},
	}
	profiles.AddOrUpdateProfile(newProfile)

	pUpdated, found := profiles.GetProfileByID("test-model")
	if !found {
		t.Fatalf("expected to find updated profile 'test-model'")
	}
	if pUpdated.Name != "Test Model Updated" {
		t.Errorf("expected updated name, got %s", pUpdated.Name)
	}

	// Test Delete
	deleted := profiles.DeleteProfile("test-model")
	if !deleted {
		t.Errorf("expected deletion to succeed")
	}

	_, foundAfterDelete := profiles.GetProfileByID("test-model")
	if foundAfterDelete {
		t.Errorf("expected profile to be deleted")
	}
}

