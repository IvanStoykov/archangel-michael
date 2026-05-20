package endpoints

import (
	"archangel/config"

	"github.com/gin-gonic/gin"
)

// SetupRouter initializes routes and CORS middlewares for the web server daemon.
func SetupRouter(cfg *config.Config, h *Handlers) *gin.Engine {
	// Respect system configuration's LogLevel for Gin modes
	switch cfg.Server.LogLevel {
	case "debug", "info":
		gin.SetMode(gin.DebugMode)
	default:
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// Apply CORS middleware
	r.Use(CORSMiddleware())

	// Route mapping decoupled directly to Handlers methods
	r.GET("/api", h.GetAPIIndex)
	r.GET("/api/status", h.GetDaemonStatus)
	r.GET("/api/logs/stream", h.StreamLogsSSE)
	r.GET("/api/config", h.GetConfig)
	r.PUT("/api/config", h.UpdateConfig)

	// Curated model profile configurations (CRUD)
	r.GET("/api/models", h.ListProfiles)
	r.GET("/api/models/:id", h.GetProfile)
	r.POST("/api/models", h.CreateProfile)
	r.PUT("/api/models/:id", h.UpdateProfile)
	r.DELETE("/api/models/:id", h.DeleteProfile)

	// Process Controls (Start, Stop, Restart)
	r.POST("/api/models/start", h.StartModelProcess)
	r.POST("/api/models/stop", h.StopModelProcess)
	r.POST("/api/models/restart", h.RestartModelProcess)

	// Task Auditing controls
	r.GET("/api/tasks", h.ListAuditTasks)
	r.POST("/api/tasks/run", h.RunAuditTask)
	r.POST("/api/tasks/cancel", h.CancelAuditTask)

	return r
}

// CORSMiddleware enables cross-origin resource requests for client dashboard tools.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
