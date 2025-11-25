package handlers

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/templates"
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
		component := templates.EntitiesPage(orgs, orgID, entities)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching entities page: %v", err)
		}
		return
	}

	component := templates.EntitiesPage(orgs, orgID, entities)
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
		component := templates.EntityForm(orgID, entity, isEdit)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#entity-form-modal"),
			datastar.WithMode(datastar.ElementPatchModeInner))
		if err != nil {
			logger.Infof("Error patching entity form: %v", err)
		}
		return
	}

	component := templates.EntityForm(orgID, entity, isEdit)
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleStreamsPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.StreamsPage()
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching streams page: %v", err)
		}
		return
	}

	component := templates.StreamsPage()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleOrganizationForm(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.OrganizationForm()
		// Try using fragment/morph mode instead of inner
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#org-form-modal"),
			datastar.WithMode(datastar.ElementPatchModeMorph))
		if err != nil {
			logger.Infof("Error patching organization form: %v", err)
		}
		return
	}

	component := templates.OrganizationForm()
	component.Render(r.Context(), w)
}

func (h *PageHandler) HandleOverwatchPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.OverwatchPage()
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching overwatch page: %v", err)
		}
		return
	}

	component := templates.OverwatchPage()
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
		component := templates.FleetPage(orgs, entities)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching fleet page: %v", err)
		}
		return
	}

	component := templates.FleetPage(orgs, entities)
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

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.VideoPage(entityIDs)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("body"),
			datastar.WithMode(datastar.ElementPatchModeOuter))
		if err != nil {
			logger.Infof("Error patching video page: %v", err)
		}
		return
	}

	component := templates.VideoPage(entityIDs)
	component.Render(r.Context(), w)
}
