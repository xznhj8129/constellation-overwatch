package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed static/css/* static/images/* static/js/* static/lib/*
var staticFS embed.FS

// StaticFileServer returns an http.Handler that serves embedded static files.
// Returns an error instead of panicking if the embedded filesystem is invalid.
func StaticFileServer() (http.Handler, error) {
	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to get static subdirectory: %w", err)
	}
	return http.FileServer(http.FS(staticContent)), nil
}
