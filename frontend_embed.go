package main

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/rs/zerolog/log"
)

//go:embed frontend/dist
var frontendFS embed.FS

// InitEmbeddedFS checks if the embedded filesystem can load.
func InitEmbeddedFS() {
	subFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize sub-filesystem for frontend assets.")
		return
	}

	// Verify we can read the embedded index.html file
	_, err = subFS.Open("index.html")
	if err != nil {
		log.Error().Err(err).Msg("Embedded index.html not found in frontend asset bundle.")
		return
	}

	log.Info().Msg("Embedded static frontend assets validated successfully.")
}

// GetAssetsHandler returns an http.Handler serving the embedded frontend code.
func GetAssetsHandler() http.Handler {
	subFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Fatal error sub-routing embedded files.")
	}
	return http.FileServer(http.FS(subFS))
}
