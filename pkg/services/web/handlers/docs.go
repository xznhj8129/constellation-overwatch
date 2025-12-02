package handlers

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/templates"
)

type DocsHandler struct{}

func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

func (h *DocsHandler) HandleDocsPage(w http.ResponseWriter, r *http.Request) {
	component := templates.DocsPage()
	component.Render(r.Context(), w)
}