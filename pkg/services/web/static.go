package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/css/* static/images/* static/lib/*
var staticFS embed.FS

// StaticFileServer returns an http.Handler that serves embedded static files
func StaticFileServer() http.Handler {
	// Get the "static" subdirectory from the embedded filesystem
	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("failed to get static subdirectory: " + err.Error())
	}
	return http.FileServer(http.FS(staticContent))
}
