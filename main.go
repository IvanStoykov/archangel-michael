package main

import (
	"flag"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"archangel/config"
	"archangel/cpg"
	"archangel/endpoints"
	agegrpc "archangel/grpc"
	"archangel/llm"
	pb "archangel/proto"
	"archangel/scout"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

var (
	thinkRegexp = regexp.MustCompile(`(?s)<think>.*?</think>`)
)

func main() {
	// 1. Parse Command Line Arguments
	configPath := flag.String("config", "archangel.yml", "Path to daemon configuration file")
	modelsPath := flag.String("models", "models.yml", "Path to curated model profiles config")
	flag.Parse()

	// 2. Load Daemon Configurations
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		return
	}

	// 3. Load Curated Model Profiles
	profiles, err := config.LoadModelsProfiles(*modelsPath)
	if err != nil {
		fmt.Printf("Error loading model profiles: %v\n", err)
		return
	}

	// 4. Initialize Multi-writer Logging Framework
	if err := cfg.SetupLog(); err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}

	log.Info().Msg("==================================================")
	log.Info().Msg("       Archangel Orchestration Daemon Booted       ")
	log.Info().Msg("==================================================")
	log.Info().Str("config_file", *configPath).Msg("Loaded configuration file successfully.")
	log.Info().Str("models_file", *modelsPath).Int("loaded_profiles", len(profiles.Profiles)).Msg("Loaded curated model profiles successfully.")

	// 5. Verify local endpoints and prefill stale watchdog
	apiBase := cfg.Model.APIHealthURL
	isLocal := cfg.IsLocalEndpoint(apiBase)
	log.Info().Str("api_url", apiBase).Bool("is_local", isLocal).Msg("Endpoint geolocation check completed.")
	if isLocal {
		log.Warn().Msg("Local endpoint detected. Prefill stale watchdog (180s timeout) has been disabled to prevent prefill infinite loops.")
	}

	// 6. Test Context Threshold Compression Safety Floor
	effectiveThreshold := config.CalculateCompressionThreshold(32768, cfg.Context.Compression.Threshold)
	safe64KThreshold := config.CalculateCompressionThreshold(64000, cfg.Context.Compression.Threshold)
	log.Info().
		Int("context_32K_threshold", effectiveThreshold).
		Int("context_64K_threshold", safe64KThreshold).
		Msg("Context compressor parameters compiled.")

	// 7. Initialize Embedded Assets Check
	InitEmbeddedFS()

	// 8. Open CPG relational SQLite database
	db, err := cpg.Open("cpg.db")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize CPG SQLite database.")
	}
	defer db.Close()

	// 9. Initialize CPG Indexer and Real Model process runner
	indexer := scout.NewIndexer(db, ".")
	runner := llm.NewRunner(profiles)

	// 10. Instantiate API Handlers dependencies
	handlers := endpoints.NewHandlers(cfg, *configPath, profiles, runner, db, indexer)

	// 11. Start gRPC Logging Stream Server (Port = Gin Port + 1)
	grpcPort := cfg.Server.Port + 1
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to bind TCP port for gRPC logging service.")
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLogServiceServer(grpcServer, &agegrpc.LogServer{})

	go func() {
		log.Info().Int("grpc_port", grpcPort).Msg("Starting gRPC Logging Server...")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC logging server failed to run.")
		}
	}()

	// 12. Start HTTP Gin Web Server
	router := endpoints.SetupRouter(cfg, handlers)
	httpAddr := fmt.Sprintf(":%d", cfg.Server.Port)

	go func() {
		log.Info().Int("http_port", cfg.Server.Port).Msg("Starting HTTP Gin Server...")
		if err := router.Run(httpAddr); err != nil {
			log.Fatal().Err(err).Msg("HTTP Gin server failed to run.")
		}
	}()

	// Keep simulation running to trace startup & logs
	log.Info().Msg("Initializing agent runtime safety simulation...")
	simulateAgentLoop(cfg)

	// Block main routine to keep servers alive
	select {}
}

// ToolCall represents a serialized agent action invocation.
type ToolCall struct {
	Name string
	Args string
}

// simulateAgentLoop simulates loop execution demonstrating decoupled parsing,
// iteration budget warnings, and doom-loop detection.
func simulateAgentLoop(cfg *config.Config) {
	// Initialize history buffer for Doom-Loop detection
	history := make([]ToolCall, 0)

	// Simulated execution of task steps
	for step := 1; step <= 90; step++ {
		// Activity Watchdog reset tick
		lastActivity := time.Now()
		log.Debug().Time("last_activity", lastActivity).Msg("Watchdog activity timer tick.")

		// Step A: Ingest Model Output with Thought blocks
		modelOutput := getSampleModelOutput(step)

		// Step B: Parser-Level Decoupling
		cleanOutput := ExtractExecutableContent(modelOutput)

		// Step C: Budget Pressure Warnings
		if step == 75 {
			log.Warn().
				Int("iteration", step).
				Int("max_limit", cfg.Agent.MaxIterations).
				Msg("[Iteration Budget: Caution Tier] Injecting summary command prompt to model.")
		} else if step == 85 {
			log.Error().
				Int("iteration", step).
				Int("max_limit", cfg.Agent.MaxIterations).
				Msg("[Iteration Budget: Warning Tier] Injecting forced completion response to model.")
		}

		// Step D: Doom-Loop repetition checks
		currentCall := getSimulatedToolCall(step)
		history = append(history, currentCall)

		// Check window size limit
		if len(history) > cfg.DoomLoop.ToolHistoryWindow {
			history = history[1:]
		}

		// Evaluate identical repetition violation
		if len(history) == cfg.DoomLoop.ToolHistoryWindow {
			allIdentical := true
			for i := 1; i < len(history); i++ {
				if history[i].Name != history[0].Name || history[i].Args != history[0].Args {
					allIdentical = false
					break
				}
			}
			if allIdentical {
				log.Warn().
					Str("tool", currentCall.Name).
					Str("args", currentCall.Args).
					Msg("[Doom-Loop Detected] Identical tool call repetition detected. Pausing and injecting warning reminder.")
				break
			}
		}

		// Trace output snippet
		if step == 1 {
			log.Info().
				Int("step", step).
				Str("extracted_instruction", strings.TrimSpace(cleanOutput)).
				Msg("Agent loop parsing verified.")
		}
	}
}

// ExtractExecutableContent strips thinking tags and returns only the finalized tool-calling or response content.
func ExtractExecutableContent(rawOutput string) string {
	return thinkRegexp.ReplaceAllString(rawOutput, "")
}

// getSampleModelOutput mocks model output content.
func getSampleModelOutput(step int) string {
	if step == 3 {
		return "<think>Let me query the workspace files again. I must check config.go.</think> {\"tool\": \"read_file\", \"path\": \"config/config.go\"}"
	}
	return "<think>I need to parse go.mod first to analyze dependencies.</think> {\"tool\": \"read_file\", \"path\": \"go.mod\"}"
}

// getSimulatedToolCall mocks tool serialization.
func getSimulatedToolCall(step int) ToolCall {
	if step >= 3 {
		return ToolCall{Name: "read_file", Args: "config/config.go"}
	}
	return ToolCall{Name: "read_file", Args: "go.mod"}
}
