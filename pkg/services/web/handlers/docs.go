package handlers

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	docs_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/docs/pages"
)

type DocsHandler struct{}

func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

func (h *DocsHandler) HandleDocsPage(w http.ResponseWriter, r *http.Request) {
	component := docs_pages.DocsPage()
	if err := component.Render(r.Context(), w); err != nil {
		logger.Errorf("Failed to render docs page: %v", err)
	}
}
