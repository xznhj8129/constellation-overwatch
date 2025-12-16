package handlers

import (
	"net/http"
	"os"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	docs_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/docs/pages"
	fleet_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/fleet/pages"
	map_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/map/pages"
	org_components "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/organizations/components"
	org_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/organizations/pages"
	overwatch_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/overwatch/pages"
	streams_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/streams/pages"
	video_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/video/pages"
)

type PageHandler struct {
	orgSvc    *services.OrganizationService
	entitySvc *services.EntityService
}

func NewPageHandler(orgSvc *services.OrganizationService, entitySvc *services.EntityService) *PageHandler {
	return &PageHandler{
		orgSvc:    orgSvc,
		entitySvc: entitySvc,
	}
}

func (h *PageHandler) HandleEntitiesPage(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")

	orgs, err := h.orgSvc.ListOrganizations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Load all entities by default
	var entities []ontology.Entity
	if orgID != "" {
		// If org_id is provided, filter by that org
		entities, err = h.entitySvc.ListEntities(orgID)
	} else {
		// Otherwise load all entities
		entities, err = h.entitySvc.ListAllEntities()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := org_pages.OrganizationsPage(orgs, orgID, entities)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching entities page: %v", err)
		}
		return
	}

	component := org_pages.OrganizationsPage(orgs, orgID, entities)
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleEntityForm(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	var entity *ontology.Entity
	isEdit := false

	if entityID != "" {
		isEdit = true
		e, err := h.entitySvc.GetEntity(orgID, entityID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entity = e
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := org_components.EntityForm(orgID, entity, isEdit)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#entity-form-modal"),
			datastar.WithMode(datastar.ElementPatchModeInner))
		if err != nil {
			logger.Infof("Error patching entity form: %v", err)
		}
		return
	}

	component := org_components.EntityForm(orgID, entity, isEdit)
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleStreamsPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := streams_pages.StreamsPage()
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching streams page: %v", err)
		}
		return
	}

	component := streams_pages.StreamsPage()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleOrganizationForm(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := org_components.OrganizationForm()
		// Try using fragment/morph mode instead of inner
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#org-form-modal"),
			datastar.WithMode(datastar.ElementPatchModeMorph))
		if err != nil {
			logger.Infof("Error patching organization form: %v", err)
		}
		return
	}

	component := org_components.OrganizationForm()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleOverwatchPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := overwatch_pages.OverwatchPage()
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching overwatch page: %v", err)
		}
		return
	}

	component := overwatch_pages.OverwatchPage()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleFleetPage(w http.ResponseWriter, r *http.Request) {
	// Fetch all organizations for the dropdown
	orgs, err := h.orgSvc.ListOrganizations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch all entities
	entities, err := h.entitySvc.ListAllEntities()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := fleet_pages.FleetPage(orgs, entities)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching fleet page: %v", err)
		}
		return
	}

	component := fleet_pages.FleetPage(orgs, entities)
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleVideoPage(w http.ResponseWriter, r *http.Request) {
	// Fetch all entities to populate the dropdown
	entities, err := h.entitySvc.ListAllEntities()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract entity IDs for the template
	var entityIDs []string
	for _, entity := range entities {
		entityIDs = append(entityIDs, entity.EntityID)
	}

	// Get NATS auth token for WebSocket connection
	natsAuthToken := os.Getenv("NATS_AUTH_TOKEN")

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := video_pages.VideoPage(entityIDs, natsAuthToken)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching video page: %v", err)
		}
		return
	}

	component := video_pages.VideoPage(entityIDs, natsAuthToken)
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleDocsPage(w http.ResponseWriter, r *http.Request) {
	component := docs_pages.DocsPage()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleMapPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := map_pages.MapPage()
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching map page: %v", err)
		}
		return
	}

	component := map_pages.MapPage()
	component.Render(r.Context(), w)
}
