package config

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// ServerConfig maps HTTP server daemon settings.
type ServerConfig struct {
	Port        int    `yaml:"port"`
	LogFile     string `yaml:"log_file"`
	LogToStdout bool   `yaml:"log_to_stdout"`
	JSONLogs    bool   `yaml:"json_logs"`
	LogLevel    string `yaml:"log_level"`
}

// ModelConfig maps local and remote LLM parameters.
type ModelConfig struct {
	DefaultProvider   string   `yaml:"default_provider"`
	DefaultModel      string   `yaml:"default_model"`
	APIHealthURL      string   `yaml:"api_health_url"`
	LocalEndpoints    []string `yaml:"local_endpoints"`
	Temperature       float64  `yaml:"temperature"`
	TopP              float64  `yaml:"top_p"`
	TopK              int      `yaml:"top_k"`
	ParallelToolCalls bool     `yaml:"parallel_tool_calls"`
	APIMaxRetries     int      `yaml:"api_max_retries"`
	SpecType          string   `yaml:"spec_type"`
	SpecDraftNMax     int      `yaml:"spec_draft_n_max"`
	SpecDraftPMin     float64  `yaml:"spec_draft_p_min"`
	CtxCheckpoints    int      `yaml:"ctx_checkpoints"`
	CacheReuse        int      `yaml:"cache_reuse"`
	SwaFull           bool     `yaml:"swa_full"`
	ToolCallParser    string   `yaml:"tool_call_parser"`
	ChatTemplate      string   `yaml:"chat_template"`
}

// AgentConfig maps iteration and token limits.
type AgentConfig struct {
	MaxIterations    int  `yaml:"max_iterations"`
	MaxAutoContinues int  `yaml:"max_auto_continues"`
	PreserveThinking bool `yaml:"preserve_thinking"`
	PreserveEnvRefs  bool `yaml:"preserve_env_refs"`
}

// CompressionConfig maps context compression mechanics.
type CompressionConfig struct {
	Enabled                 bool    `yaml:"enabled"`
	Threshold               float64 `yaml:"threshold"`
	TargetRatio             float64 `yaml:"target_ratio"`
	ProtectLastN            int     `yaml:"protect_last_n"`
	HygieneHardMessageLimit int     `yaml:"hygiene_hard_message_limit"`
}

// ContextConfig wraps context engines.
type ContextConfig struct {
	Engine      string            `yaml:"engine"`
	Compression CompressionConfig `yaml:"compression"`
}

// DoomLoopConfig maps safety detector thresholds.
type DoomLoopConfig struct {
	ToolHistoryWindow  int     `yaml:"tool_history_window"`
	ASTSimilarityLimit float64 `yaml:"ast_similarity_limit"`
	MaxCompileFailures int     `yaml:"max_compile_failures"`
	HardIterationLimit int     `yaml:"hard_iteration_limit"`
}

// TimeoutsConfig maps execution timeouts.
type TimeoutsConfig struct {
	TaskTimeoutSec     int `yaml:"task_timeout_sec"`
	TerminalTimeoutSec int `yaml:"terminal_timeout_sec"`
	StaleTimeoutSec    int `yaml:"stale_timeout_sec"`
}

// Config represents the unified application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Model    ModelConfig    `yaml:"model"`
	Agent    AgentConfig    `yaml:"agent"`
	Context  ContextConfig  `yaml:"context"`
	DoomLoop DoomLoopConfig `yaml:"doom_loop"`
	Timeouts TimeoutsConfig `yaml:"timeouts"`
}

// ModelProfile maps specific GGUF model properties.
type ModelProfile struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	ModelPath   string                 `yaml:"model_path" json:"model_path"`
	Parameters  map[string]interface{} `yaml:"parameters" json:"parameters"`
}

// ModelsProfilesConfig wraps a list of profiles.
type ModelsProfilesConfig struct {
	Profiles []ModelProfile `yaml:"profiles" json:"profiles"`
	mu       sync.RWMutex   `yaml:"-" json:"-"`
}

// LogBroadcaster streams structured logger outputs to HTTP SSE and gRPC clients.
var LogBroadcaster = NewBroadcaster()

type Broadcaster struct {
	listeners  map[chan string]bool
	register   chan chan string
	deregister chan chan string
	messages   chan string
}

func NewBroadcaster() *Broadcaster {
	b := &Broadcaster{
		listeners:  make(map[chan string]bool),
		register:   make(chan chan string),
		deregister: make(chan chan string),
		messages:   make(chan string, 100),
	}
	go b.run()
	return b
}

func (b *Broadcaster) run() {
	for {
		select {
		case ch := <-b.register:
			b.listeners[ch] = true
		case ch := <-b.deregister:
			delete(b.listeners, ch)
			close(ch)
		case msg := <-b.messages:
			for ch := range b.listeners {
				select {
				case ch <- msg:
				default:
				}
			}
		}
	}
}

func (b *Broadcaster) Write(p []byte) (n int, err error) {
	msg := string(p)
	select {
	case b.messages <- msg:
	default:
	}
	return len(p), nil
}

func (b *Broadcaster) Register() chan string {
	ch := make(chan string, 50)
	b.register <- ch
	return ch
}

func (b *Broadcaster) Deregister(ch chan string) {
	b.deregister <- ch
}

// DefaultConfig defines fallback parameter values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:        8080,
			LogFile:     "archangel.log",
			LogToStdout: true,
			JSONLogs:    false,
			LogLevel:    "info",
		},
		Model: ModelConfig{
			DefaultProvider:   "custom",
			DefaultModel:      "qwen3.6-35b-mtp",
			APIHealthURL:      "http://ollama:11434/api/tags",
			LocalEndpoints:    []string{"ollama", "hermes-litellm", "localhost", "127.0.0.1"},
			Temperature:       0.20,
			TopP:              0.90,
			TopK:              40,
			ParallelToolCalls: true,
			APIMaxRetries:     1,
			SpecType:          "draft-mtp",
			SpecDraftNMax:     2,
			SpecDraftPMin:     0.75,
			CtxCheckpoints:    4,
			CacheReuse:        256,
			SwaFull:           true,
		},
		Agent: AgentConfig{
			MaxIterations:    90,
			MaxAutoContinues: 3,
			PreserveThinking: true,
			PreserveEnvRefs:  true,
		},
		Context: ContextConfig{
			Engine: "compressor",
			Compression: CompressionConfig{
				Enabled:                 true,
				Threshold:               0.70,
				TargetRatio:             0.20,
				ProtectLastN:            20,
				HygieneHardMessageLimit: 400,
			},
		},
		DoomLoop: DoomLoopConfig{
			ToolHistoryWindow:  3,
			ASTSimilarityLimit: 0.90,
			MaxCompileFailures: 3,
			HardIterationLimit: 5,
		},
		Timeouts: TimeoutsConfig{
			TaskTimeoutSec:     3600,
			TerminalTimeoutSec: 600,
			StaleTimeoutSec:    600,
		},
	}
}

// LoadConfig reads the configuration file at the path, expanding environment variables in memory.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	fileBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Msgf("Config file %s not found. Using default parameters.", path)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Determine if we should preserve env variable references
	var rawCfg Config
	if err := yaml.Unmarshal(fileBytes, &rawCfg); err == nil {
		if rawCfg.Agent.PreserveEnvRefs {
			err = yaml.Unmarshal(fileBytes, cfg)
			if err != nil {
				return nil, fmt.Errorf("failed to parse yaml configuration raw: %w", err)
			}
			return cfg, nil
		}
	}

	expandedContent := os.ExpandEnv(string(fileBytes))

	err = yaml.Unmarshal([]byte(expandedContent), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse yaml configuration: %w", err)
	}

	return cfg, nil
}

// Save writes the global configuration back to disk.
func (cfg *Config) Save(path string) error {
	fileBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(path, fileBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config to file: %w", err)
	}

	return nil
}

// LoadModelsProfiles parses the curated model settings.
func LoadModelsProfiles(path string) (*ModelsProfilesConfig, error) {
	cfg := &ModelsProfilesConfig{
		Profiles: make([]ModelProfile, 0),
	}

	fileBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read models profiles file: %w", err)
	}

	err = yaml.Unmarshal(fileBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse models profiles: %w", err)
	}

	return cfg, nil
}

// Save writes curated model settings back to disk.
func (mp *ModelsProfilesConfig) Save(path string) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	fileBytes, err := yaml.Marshal(mp)
	if err != nil {
		return fmt.Errorf("failed to marshal model profiles: %w", err)
	}

	err = os.WriteFile(path, fileBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write model profiles: %w", err)
	}

	return nil
}

// GetProfileByID safe retrieves profile entries.
func (mp *ModelsProfilesConfig) GetProfileByID(id string) (ModelProfile, bool) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	for _, p := range mp.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return ModelProfile{}, false
}

// AddOrUpdateProfile updates model profiles list.
func (mp *ModelsProfilesConfig) AddOrUpdateProfile(newProfile ModelProfile) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for i, p := range mp.Profiles {
		if p.ID == newProfile.ID {
			mp.Profiles[i] = newProfile
			return
		}
	}
	mp.Profiles = append(mp.Profiles, newProfile)
}

// DeleteProfile deletes a profile by ID.
func (mp *ModelsProfilesConfig) DeleteProfile(id string) bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for i, p := range mp.Profiles {
		if p.ID == id {
			mp.Profiles = append(mp.Profiles[:i], mp.Profiles[i+1:]...)
			return true
		}
	}
	return false
}

// IsLocalEndpoint checks whether a Base URL target represents a local network endpoint.
// It bypasses the 180s stale watchdog timeout on local prefill tasks.
func (cfg *Config) IsLocalEndpoint(rawURL string) bool {
	expandedURL := os.ExpandEnv(rawURL)
	parsed, err := url.Parse(expandedURL)
	if err != nil {
		return false
	}

	host := parsed.Hostname()
	if host == "" {
		host = parsed.Path
	}
	host = strings.TrimSpace(host)

	for _, le := range cfg.Model.LocalEndpoints {
		if strings.EqualFold(host, le) {
			return true
		}
	}

	if !strings.Contains(host, ".") {
		return true
	}

	if strings.EqualFold(host, "localhost") {
		return true
	}

	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() || isPrivateIP(ip) {
				return true
			}
		}
	}

	return false
}

// isPrivateIP checks if the IP address belongs to RFC 1918 or Link-local private ranges.
func isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 10 {
			return true
		}
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

// CalculateCompressionThreshold determines the token checkpoint capacity.
// It avoids the 64K compression floor locking bug by falling back to percentage limits.
func CalculateCompressionThreshold(contextLength int, thresholdPercent float64) int {
	thresholdTokens := int(float64(contextLength) * thresholdPercent)

	if thresholdTokens >= contextLength {
		thresholdTokens = int(float64(contextLength) * thresholdPercent)
	}

	if thresholdTokens < 4096 && contextLength > 4096 {
		return 4096
	}

	return thresholdTokens
}

// SetupLog initializes the zerolog system based on server configuration options.
func (cfg *Config) SetupLog() error {
	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Server.LogLevel))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	var consoleOutput io.Writer = os.Stdout
	if !cfg.Server.JSONLogs {
		consoleOutput = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"}
	}

	var output io.Writer = io.MultiWriter(consoleOutput, LogBroadcaster)

	if cfg.Server.LogFile != "" {
		expandedLogFile := os.ExpandEnv(cfg.Server.LogFile)
		logDir := filepath.Dir(expandedLogFile)
		if logDir != "." && logDir != "/" {
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return fmt.Errorf("failed to create log directory: %w", err)
			}
		}

		file, err := os.OpenFile(expandedLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		if cfg.Server.LogToStdout {
			output = io.MultiWriter(consoleOutput, file, LogBroadcaster)
		} else {
			output = io.MultiWriter(file, LogBroadcaster)
		}
	}

	log.Logger = zerolog.New(output).With().Timestamp().Logger()
	return nil
}
