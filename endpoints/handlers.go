package endpoints

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"archangel/config"
	"archangel/cpg"
	"archangel/llm"
	"archangel/scout"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// ProcessState describes the process run state of a local GGUF server.
type ProcessState struct {
	ActiveProfile string    `json:"active_profile"`
	Running       bool      `json:"running"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	Pid           int       `json:"pid,omitempty"`
}

// TaskState describes an active or completed code audit run.
type TaskState struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"` // "running", "completed", "cancelled", "failed"
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
	FileCount     int       `json:"file_count"`
	NodesInserted int       `json:"nodes_inserted,omitempty"`
	EdgesInserted int       `json:"edges_inserted,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// StartControlRequest binds start process triggers.
type StartControlRequest struct {
	ProfileID string `json:"profile_id"`
}

// Handlers encapsulates daemon dependencies for REST endpoint logic.
type Handlers struct {
	cfg      *config.Config
	cfgPath  string
	profiles *config.ModelsProfilesConfig
	runner   *llm.Runner
	db       *cpg.DB
	indexer  *scout.Indexer

	stateMu     sync.RWMutex
	activeTasks []TaskState
}

// NewHandlers creates a new handlers injector instance.
func NewHandlers(cfg *config.Config, cfgPath string, profiles *config.ModelsProfilesConfig, runner *llm.Runner, db *cpg.DB, indexer *scout.Indexer) *Handlers {
	return &Handlers{
		cfg:         cfg,
		cfgPath:     cfgPath,
		profiles:    profiles,
		runner:      runner,
		db:          db,
		indexer:     indexer,
		activeTasks: make([]TaskState, 0),
	}
}

func (h *Handlers) GetAPIIndex(c *gin.Context) {
	acceptHeader := c.GetHeader("Accept")

	// If a browser is requesting (Accept contains text/html), render the nice page
	if strings.Contains(acceptHeader, "text/html") || acceptHeader == "" || acceptHeader == "*/*" {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(htmlIndexPage))
		return
	}

	// Programmatic requests get a structured JSON index
	c.JSON(http.StatusOK, gin.H{
		"daemon":      "archangel-michael",
		"version":     "0.1.0",
		"description": "Archangel Orchestration Daemon API Endpoints Index",
		"endpoints": []gin.H{
			{"method": "GET", "path": "/api", "description": "Interactive browser API index / documentation"},
			{"method": "GET", "path": "/api/status", "description": "Fetch daemon running logs and processes status"},
			{"method": "GET", "path": "/api/logs/stream", "description": "Stream server logs over Server-Sent Events (SSE)"},
			{"method": "GET", "path": "/api/config", "description": "Retrieve current daemon YAML configuration"},
			{"method": "PUT", "path": "/api/config", "description": "Update daemon YAML configuration dynamically"},
			{"method": "GET", "path": "/api/models", "description": "List all registered model profiles"},
			{"method": "GET", "path": "/api/models/:id", "description": "Retrieve details of a specific profile"},
			{"method": "POST", "path": "/api/models", "description": "Register a new curated serving profile"},
			{"method": "PUT", "path": "/api/models/:id", "description": "Modify parameters of an existing profile"},
			{"method": "DELETE", "path": "/api/models/:id", "description": "Remove a profile from curated settings"},
			{"method": "POST", "path": "/api/models/start", "description": "Deploy serving engine on Performance cores"},
			{"method": "POST", "path": "/api/models/stop", "description": "Halt serve execution of serving engine"},
			{"method": "POST", "path": "/api/models/restart", "description": "Cleanly restart local serving process"},
			{"method": "GET", "path": "/api/tasks", "description": "Retrieve state of active and historic audits"},
			{"method": "POST", "path": "/api/tasks/run", "description": "Initiate static codebase indexing pass"},
			{"method": "POST", "path": "/api/tasks/cancel", "description": "Terminate currently running audit process"},
		},
	})
}

// GetDaemonStatus returns the state of the orchestrator, CPG DB, and active server processes.
func (h *Handlers) GetDaemonStatus(c *gin.Context) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()

	rs := h.runner.State()
	currentModel := ProcessState{
		ActiveProfile: rs.ProfileID,
		Running:       rs.Running,
		StartedAt:     rs.StartedAt,
		Pid:           rs.Pid,
	}

	nodesCount, edgesCount, _ := h.db.Stats()

	c.JSON(http.StatusOK, gin.H{
		"daemon":       "archangel-michael",
		"version":      "0.1.0",
		"status":       "nominal",
		"active_model": currentModel,
		"task_count":   len(h.activeTasks),
		"cpg_nodes":    nodesCount,
		"cpg_edges":    edgesCount,
	})
}

// StreamLogsSSE streams real-time logs over a Server-Sent Events channel.
func (h *Handlers) StreamLogsSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// Register listener to global zerolog log broadcaster
	ch := config.LogBroadcaster.Register()
	defer config.LogBroadcaster.Deregister(ch)

	log.Info().Msg("Live HTTP log streaming subscriber established connection.")

	c.Stream(func(w io.Writer) bool {
		select {
		case <-c.Request.Context().Done():
			log.Info().Msg("Live HTTP log streaming subscriber closed connection.")
			return false
		case msg, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent("log", msg)
			return true
		}
	})
}

// GetConfig returns the active configuration.
func (h *Handlers) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.cfg)
}

// UpdateConfig updates the active configuration on disk and memory.
func (h *Handlers) UpdateConfig(c *gin.Context) {
	var newCfg config.Config
	if err := c.ShouldBindJSON(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update memory settings
	*h.cfg = newCfg

	// Save back to archangel.yml
	if err := h.cfg.Save(h.cfgPath); err != nil {
		log.Error().Err(err).Msg("Failed to persist updated configuration to disk.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save configuration: " + err.Error()})
		return
	}

	log.Info().Msg("System configuration updated dynamically.")
	c.JSON(http.StatusOK, h.cfg)
}

// ListProfiles retrieves all curated profiles from models.yml.
func (h *Handlers) ListProfiles(c *gin.Context) {
	c.JSON(http.StatusOK, h.profiles.Profiles)
}

// GetProfile retrieves a single curated profile.
func (h *Handlers) GetProfile(c *gin.Context) {
	id := c.Param("id")
	p, found := h.profiles.GetProfileByID(id)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "model profile not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// CreateProfile binds and adds a model profile to models.yml.
func (h *Handlers) CreateProfile(c *gin.Context) {
	var p config.ModelProfile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if p.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile id must be specified"})
		return
	}

	h.profiles.AddOrUpdateProfile(p)

	if err := h.profiles.Save("models.yml"); err != nil {
		log.Error().Err(err).Msg("Failed to persist models.yml profile creation.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save profile to disk"})
		return
	}

	log.Info().Str("profile_id", p.ID).Msg("Curated model profile created successfully.")
	c.JSON(http.StatusCreated, p)
}

// UpdateProfile modifies an existing profile inside models.yml.
func (h *Handlers) UpdateProfile(c *gin.Context) {
	id := c.Param("id")
	_, found := h.profiles.GetProfileByID(id)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "model profile not found"})
		return
	}

	var p config.ModelProfile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = id
	h.profiles.AddOrUpdateProfile(p)

	if err := h.profiles.Save("models.yml"); err != nil {
		log.Error().Err(err).Msg("Failed to persist models.yml profile update.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save profile to disk"})
		return
	}

	log.Info().Str("profile_id", id).Msg("Curated model profile updated successfully.")
	c.JSON(http.StatusOK, p)
}

// DeleteProfile removes a profile from models.yml.
func (h *Handlers) DeleteProfile(c *gin.Context) {
	id := c.Param("id")
	if !h.profiles.DeleteProfile(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "model profile not found"})
		return
	}

	if err := h.profiles.Save("models.yml"); err != nil {
		log.Error().Err(err).Msg("Failed to persist models.yml profile deletion.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save changes to disk"})
		return
	}

	log.Info().Str("profile_id", id).Msg("Curated model profile deleted successfully.")
	c.JSON(http.StatusOK, gin.H{"message": "profile deleted"})
}

// StartModelProcess launches a llama-server serv engine pinned to CPU P-cores.
func (h *Handlers) StartModelProcess(c *gin.Context) {
	var req StartControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.runner.Start(req.ProfileID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rs := h.runner.State()
	c.JSON(http.StatusOK, gin.H{
		"message": "model started successfully",
		"state": ProcessState{
			ActiveProfile: rs.ProfileID,
			Running:       rs.Running,
			StartedAt:     rs.StartedAt,
			Pid:           rs.Pid,
		},
	})
}

// StopModelProcess halts the running GGUF serving engine process.
func (h *Handlers) StopModelProcess(c *gin.Context) {
	if err := h.runner.Stop(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "model stopped successfully",
	})
}

// RestartModelProcess halts and launches the GGUF serving engine process.
func (h *Handlers) RestartModelProcess(c *gin.Context) {
	var req StartControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.runner.Restart(req.ProfileID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rs := h.runner.State()
	c.JSON(http.StatusOK, gin.H{
		"message": "model restarted successfully",
		"state": ProcessState{
			ActiveProfile: rs.ProfileID,
			Running:       rs.Running,
			StartedAt:     rs.StartedAt,
			Pid:           rs.Pid,
		},
	})
}

// ListAuditTasks lists all completed or running tasks.
func (h *Handlers) ListAuditTasks(c *gin.Context) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()

	c.JSON(http.StatusOK, h.activeTasks)
}

// RunAuditTask dispatches a real background CPG indexing scan and code analysis sweep.
func (h *Handlers) RunAuditTask(c *gin.Context) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()

	task := TaskState{
		ID:        "task-run-" + time.Now().Format("20060102-150405"),
		Status:    "running",
		StartedAt: time.Now(),
	}

	h.activeTasks = append(h.activeTasks, task)

	log.Info().Str("task_id", task.ID).Msg("Dispatched autonomous static audit execution loop.")

	// Perform the real indexing pass in the background
	go func(id string) {
		res, err := h.indexer.Pass1MapWorkspace()
		h.stateMu.Lock()
		defer h.stateMu.Unlock()

		for i, t := range h.activeTasks {
			if t.ID == id {
				h.activeTasks[i].EndedAt = time.Now()
				if err != nil {
					h.activeTasks[i].Status = "failed"
					h.activeTasks[i].Error = err.Error()
					log.Error().Err(err).Str("task_id", id).Msg("Autonomous static audit failed.")
				} else {
					h.activeTasks[i].Status = "completed"
					h.activeTasks[i].FileCount = res.FilesScanned
					h.activeTasks[i].NodesInserted = res.NodesInserted
					h.activeTasks[i].EdgesInserted = res.EdgesInserted
					log.Info().
						Str("task_id", id).
						Int("files", res.FilesScanned).
						Int("nodes", res.NodesInserted).
						Int("edges", res.EdgesInserted).
						Msg("Autonomous static audit completed successfully.")
				}
				return
			}
		}
	}(task.ID)

	c.JSON(http.StatusAccepted, task)
}

// CancelAuditTask cancels the currently active audit.
func (h *Handlers) CancelAuditTask(c *gin.Context) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()

	cancelledAny := false
	for i, t := range h.activeTasks {
		if t.Status == "running" {
			h.activeTasks[i].Status = "cancelled"
			h.activeTasks[i].EndedAt = time.Now()
			log.Warn().Str("task_id", t.ID).Msg("Audit task run manually cancelled by user request.")
			cancelledAny = true
		}
	}

	if !cancelledAny {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no active audit tasks are currently running"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "active tasks cancelled"})
}

// Beautiful Premium Glassmorphism API Index HTML Page
const htmlIndexPage = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Archangel API Browser</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0f172a;
            --panel-bg: rgba(30, 41, 59, 0.7);
            --border-color: rgba(255, 255, 255, 0.08);
            --text-color: #f1f5f9;
            --text-muted: #94a3b8;
            --primary: #6366f1;
            --primary-hover: #4f46e5;
            
            --get-color: #10b981;
            --post-color: #3b82f6;
            --put-color: #f59e0b;
            --delete-color: #ef4444;
        }
        
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        
        body {
            font-family: 'Inter', sans-serif;
            background-color: var(--bg-color);
            color: var(--text-color);
            min-height: 100vh;
            background-image: radial-gradient(circle at 10% 20%, rgba(99, 102, 241, 0.15) 0%, transparent 40%),
                              radial-gradient(circle at 90% 80%, rgba(139, 92, 246, 0.15) 0%, transparent 40%);
            padding: 2rem;
            display: flex;
            flex-direction: column;
            align-items: center;
        }

        .container {
            max-width: 1000px;
            width: 100%;
        }

        header {
            margin-bottom: 2rem;
            text-align: center;
        }

        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            background: linear-gradient(to right, #818cf8, #c084fc);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 0.5rem;
        }

        p.subtitle {
            color: var(--text-muted);
            font-size: 1.1rem;
        }

        .search-container {
            margin-bottom: 2rem;
            position: relative;
        }

        .search-input {
            width: 100%;
            padding: 1rem 1.5rem;
            font-size: 1.1rem;
            border-radius: 9999px;
            border: 1px solid var(--border-color);
            background-color: var(--panel-bg);
            backdrop-filter: blur(12px);
            color: var(--text-color);
            outline: none;
            transition: all 0.3s ease;
        }

        .search-input:focus {
            border-color: var(--primary);
            box-shadow: 0 0 15px rgba(99, 102, 241, 0.2);
        }

        .grid {
            display: grid;
            grid-template-columns: 1fr;
            gap: 1rem;
        }

        .card {
            background-color: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 1.25rem;
            backdrop-filter: blur(12px);
            display: flex;
            align-items: center;
            justify-content: space-between;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            text-decoration: none;
            color: inherit;
        }

        .card:hover {
            transform: translateY(-2px);
            border-color: rgba(99, 102, 241, 0.4);
            box-shadow: 0 10px 20px rgba(0, 0, 0, 0.2);
        }

        .endpoint-info {
            display: flex;
            align-items: center;
            gap: 1rem;
            flex-grow: 1;
        }

        .badge {
            font-size: 0.85rem;
            font-weight: 700;
            padding: 0.4rem 0.8rem;
            border-radius: 6px;
            min-width: 80px;
            text-align: center;
            text-transform: uppercase;
        }

        .badge.get { background-color: rgba(16, 185, 129, 0.15); color: var(--get-color); border: 1px solid rgba(16, 185, 129, 0.3); }
        .badge.post { background-color: rgba(59, 130, 246, 0.15); color: var(--post-color); border: 1px solid rgba(59, 130, 246, 0.3); }
        .badge.put { background-color: rgba(245, 158, 11, 0.15); color: var(--put-color); border: 1px solid rgba(245, 158, 11, 0.3); }
        .badge.delete { background-color: rgba(239, 68, 68, 0.15); color: var(--delete-color); border: 1px solid rgba(239, 68, 68, 0.3); }

        .path {
            font-family: monospace;
            font-size: 1.1rem;
            font-weight: 500;
            color: #e2e8f0;
        }

        .desc {
            color: var(--text-muted);
            font-size: 0.95rem;
            margin-top: 0.25rem;
        }

        .action-btn {
            background-color: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--border-color);
            color: var(--text-muted);
            padding: 0.5rem 1rem;
            border-radius: 8px;
            font-size: 0.9rem;
            font-weight: 500;
            transition: all 0.2s ease;
            white-space: nowrap;
        }

        .card:hover .action-btn {
            background-color: var(--primary);
            color: white;
            border-color: var(--primary);
        }

        footer {
            margin-top: 4rem;
            text-align: center;
            color: var(--text-muted);
            font-size: 0.9rem;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>Archangel Orchestrator API Browser</h1>
            <p class="subtitle">Interactive API Endpoints for model lifecycle and static code audits</p>
        </header>

        <div class="search-container">
            <input type="text" id="search" class="search-input" placeholder="Search API endpoints (e.g. status, models, task)..." autocomplete="off">
        </div>

        <div class="grid" id="endpoints-grid">
            <!-- Cards will be populated here -->
        </div>
    </div>

    <footer>
        Archangel Daemon v0.1.0 &bull; Running on RTX 4070 (12GB VRAM) & Intel Core i7-13700KF
    </footer>

    <script>
        const endpoints = [
            { method: "GET", path: "/api/status", desc: "Retrieve active serving model, engine version, and diagnostics status.", link: "/api/status" },
            { method: "GET", path: "/api/logs/stream", desc: "Establish Server-Sent Events (SSE) live logging stream.", link: "/api/logs/stream" },
            { method: "GET", path: "/api/config", desc: "Read global system configuration (archangel.yml).", link: "/api/config" },
            { method: "PUT", path: "/api/config", desc: "Modify global system configuration (archangel.yml).", link: "#" },
            { method: "GET", path: "/api/models", desc: "List all curated local GGUF serving model profiles.", link: "/api/models" },
            { method: "GET", path: "/api/models/:id", desc: "Retrieve a specific model profile metadata.", link: "/api/models/qwen3.6-35b-mtp" },
            { method: "POST", path: "/api/models", desc: "Register a new curated model profile inside models.yml.", link: "#" },
            { method: "PUT", path: "/api/models/:id", desc: "Update configuration fields for a curated model profile.", link: "#" },
            { method: "DELETE", path: "/api/models/:id", desc: "Delete a curated model profile from registry.", link: "#" },
            { method: "POST", path: "/api/models/start", desc: "Initialize llama-server serve instance for a model profile.", link: "#" },
            { method: "POST", path: "/api/models/stop", desc: "Halt serve execution of the active GGUF local model.", link: "#" },
            { method: "POST", path: "/api/models/restart", desc: "Cleanly restart local serving process.", link: "#" },
            { method: "GET", path: "/api/tasks", desc: "List all running or completed static audit tasks.", link: "/api/tasks" },
            { method: "POST", path: "/api/tasks/run", desc: "Trigger static CPG analysis and audit sweep on local workspace.", link: "#" },
            { method: "POST", path: "/api/tasks/cancel", desc: "Force termination and rollback on currently executing audit.", link: "#" }
        ];

        const grid = document.getElementById("endpoints-grid");
        const searchInput = document.getElementById("search");

        function render(filter = "") {
            grid.innerHTML = "";
            const lowerFilter = filter.toLowerCase();
            
            endpoints.forEach(ep => {
                if (ep.path.toLowerCase().includes(lowerFilter) || 
                    ep.desc.toLowerCase().includes(lowerFilter) || 
                    ep.method.toLowerCase().includes(lowerFilter)) {
                    
                    const card = document.createElement("a");
                    card.className = "card";
                    card.href = ep.link;
                    card.target = ep.link !== "#" ? "_blank" : "_self";
                    
                    card.innerHTML = 
                        '<div class="endpoint-info">' +
                            '<span class="badge ' + ep.method.toLowerCase() + '">' + ep.method + '</span>' +
                            '<div>' +
                                '<div class="path">' + ep.path + '</div>' +
                                '<div class="desc">' + ep.desc + '</div>' +
                            '</div>' +
                        '</div>' +
                        '<div class="action-btn">' + (ep.link !== '#' ? 'Open' : 'Test Endpoint') + '</div>';
                    grid.appendChild(card);
                }
            });
        }

        searchInput.addEventListener("input", (e) => {
            render(e.target.value);
        });

        render();
    </script>
</body>
</html>
`
