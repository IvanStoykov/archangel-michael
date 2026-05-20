package llm

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"archangel/config"

	"github.com/rs/zerolog/log"
)

// ProcessState captures the runtime state of a managed llama-server process.
type ProcessState struct {
	ProfileID string    `json:"profile_id"`
	Running   bool      `json:"running"`
	Pid       int       `json:"pid"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Cmd       *exec.Cmd `json:"-"`
}

// Runner manages the lifecycle of a local GGUF model serving process.
type Runner struct {
	mu       sync.Mutex
	state    ProcessState
	profiles *config.ModelsProfilesConfig
}

// NewRunner creates a new llama-server lifecycle manager.
func NewRunner(profiles *config.ModelsProfilesConfig) *Runner {
	return &Runner{
		profiles: profiles,
	}
}

// State returns a copy of the current process state.
func (r *Runner) State() ProcessState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Start launches a llama-server process using the given profile's parameters.
// The process is wrapped in taskset to pin threads to Performance Cores.
func (r *Runner) Start(profileID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state.Running {
		return fmt.Errorf("model process already running (profile=%s, pid=%d); stop it first", r.state.ProfileID, r.state.Pid)
	}

	profile, found := r.profiles.GetProfileByID(profileID)
	if !found {
		return fmt.Errorf("model profile %q not found in models.yml", profileID)
	}

	args := buildLlamaArgs(profile)

	log.Info().
		Str("profile", profileID).
		Str("model_path", profile.ModelPath).
		Strs("args", args).
		Msg("Constructing llama-server launch command.")

	// Wrap in taskset for P-Core affinity binding if taskset is available (Linux)
	var cmd *exec.Cmd
	coreAffinity := getStringParam(profile.Parameters, "core_affinity", "0-15")

	if _, err := exec.LookPath("taskset"); err == nil {
		cmdArgs := append([]string{"-c", coreAffinity, "llama-server"}, args...)
		cmd = exec.Command("taskset", cmdArgs...)
		log.Info().
			Str("core_affinity", coreAffinity).
			Msg("[Hardware Optimization] llama-server wrapped in taskset for Performance Cores.")
	} else {
		log.Warn().Msg("taskset binary not found in path (non-Linux or macOS system). Launching llama-server directly without core binding.")
		cmd = exec.Command("llama-server", args...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	r.state = ProcessState{
		ProfileID: profileID,
		Running:   true,
		Pid:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Cmd:       cmd,
	}

	log.Info().
		Int("pid", r.state.Pid).
		Msg("llama-server launched successfully.")

	// Monitor process in background
	go r.monitor(cmd)

	return nil
}

// Stop terminates the running llama-server process.
func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.state.Running || r.state.Cmd == nil {
		return fmt.Errorf("no active model process to stop")
	}

	log.Info().Int("pid", r.state.Pid).Str("profile", r.state.ProfileID).Msg("Sending SIGTERM to llama-server process group.")

	// Kill the entire process group
	pgid, err := syscall.Getpgid(r.state.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		r.state.Cmd.Process.Signal(syscall.SIGTERM)
	}

	// Give it 5 seconds to shut down gracefully
	done := make(chan error, 1)
	go func() {
		done <- r.state.Cmd.Wait()
	}()

	select {
	case <-done:
		log.Info().Msg("llama-server process exited gracefully.")
	case <-time.After(5 * time.Second):
		log.Warn().Msg("llama-server did not exit in 5s, sending SIGKILL.")
		if pgid, err := syscall.Getpgid(r.state.Pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}

	r.state = ProcessState{Running: false}
	return nil
}

// Restart stops and restarts with the given profile.
func (r *Runner) Restart(profileID string) error {
	if r.State().Running {
		if err := r.Stop(); err != nil {
			log.Warn().Err(err).Msg("Error during stop phase of restart; proceeding with start.")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return r.Start(profileID)
}

// monitor waits for the process to exit and updates state.
func (r *Runner) monitor(cmd *exec.Cmd) {
	err := cmd.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()

	if err != nil {
		log.Warn().Err(err).Int("pid", r.state.Pid).Msg("llama-server process exited with error.")
	} else {
		log.Info().Int("pid", r.state.Pid).Msg("llama-server process exited normally.")
	}

	r.state.Running = false
	r.state.Cmd = nil
}

// buildLlamaArgs constructs the command-line arguments for llama-server
// based on the curated model profile parameters.
func buildLlamaArgs(profile config.ModelProfile) []string {
	var args []string

	// Model path
	args = append(args, "-m", profile.ModelPath)

	// Thread count
	threads := getIntParam(profile.Parameters, "threads", 8)
	args = append(args, "-t", strconv.Itoa(threads))

	// Asymmetric KV cache quantization (Doc 08)
	ctk := getStringParam(profile.Parameters, "ctk", "q8_0")
	ctv := getStringParam(profile.Parameters, "ctv", "q4_0")
	args = append(args, "-ctk", ctk, "-ctv", ctv)

	// Speculative decoding (Doc 08)
	specType := getStringParam(profile.Parameters, "spec_type", "none")
	if specType != "none" && specType != "" {
		args = append(args, "--spec-type", specType)

		specNMax := getIntParam(profile.Parameters, "spec_draft_n_max", 2)
		args = append(args, "--spec-draft-n-max", strconv.Itoa(specNMax))

		specPMin := getFloatParam(profile.Parameters, "spec_draft_p_min", 0.75)
		args = append(args, "--spec-draft-p-min", fmt.Sprintf("%.2f", specPMin))
	}

	// Context checkpoints for DeltaNet recurrence
	ctxCheckpoints := getIntParam(profile.Parameters, "ctx_checkpoints", 4)
	args = append(args, "--ctx-checkpoints", strconv.Itoa(ctxCheckpoints))

	// Sliding Window Attention
	if getBoolParam(profile.Parameters, "swa_full", false) {
		args = append(args, "--swa-full")
	}

	// Dynamic VRAM fitting
	if getBoolParam(profile.Parameters, "fit", false) {
		args = append(args, "--fit", "on")
		fitCtx := getIntParam(profile.Parameters, "fit_ctx", 16384)
		args = append(args, "-fitc", strconv.Itoa(fitCtx))
		fitTarget := getIntParam(profile.Parameters, "fit_target", 1536)
		args = append(args, "-fitt", strconv.Itoa(fitTarget))
	}

	// Cache reuse
	cacheReuse := getIntParam(profile.Parameters, "cache_reuse", 256)
	if cacheReuse > 0 {
		args = append(args, "--cache-reuse", strconv.Itoa(cacheReuse))
	}

	// Temperature override for server-side sampling
	temp := getFloatParam(profile.Parameters, "temperature", 0.20)
	args = append(args, "--temp", fmt.Sprintf("%.2f", temp))

	// Memory mapping override
	if getBoolParam(profile.Parameters, "no_mmap", false) {
		args = append(args, "--no-mmap")
	}

	// Projector offload override
	if getBoolParam(profile.Parameters, "no_mmproj_offload", false) {
		args = append(args, "--no-mmproj-offload")
	}

	// Tool call parser override
	toolCallParser := getStringParam(profile.Parameters, "tool_call_parser", "")
	if toolCallParser != "" {
		args = append(args, "--tool-call-parser", toolCallParser)
	}

	// Chat template override
	chatTemplate := getStringParam(profile.Parameters, "chat_template", "")
	if chatTemplate != "" {
		args = append(args, "--chat-template", chatTemplate)
	}

	// Port mapping
	port := getIntParam(profile.Parameters, "port", 8081)
	args = append(args, "--port", strconv.Itoa(port))

	return args
}

// --- Parameter extraction helpers for map[string]interface{} ---

func getStringParam(params map[string]interface{}, key, fallback string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func getIntParam(params map[string]interface{}, key string, fallback int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				return i
			}
		}
	}
	return fallback
}

func getFloatParam(params map[string]interface{}, key string, fallback float64) float64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				return f
			}
		}
	}
	return fallback
}

func getBoolParam(params map[string]interface{}, key string, fallback bool) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return fallback
}
