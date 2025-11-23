package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"constellation-overwatch/api/middleware"
	"constellation-overwatch/api/services"
	"constellation-overwatch/db"
	"constellation-overwatch/pkg/ontology"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
	"constellation-overwatch/pkg/services/logger"
	"constellation-overwatch/pkg/services/web/datastar"
	"constellation-overwatch/pkg/services/web/templates"
	"constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

type Server struct {
	db           *db.Service
	nc           *nats.Conn
	natsEmbedded *embeddednats.EmbeddedNATS
	orgSvc       *services.OrganizationService
	entitySvc    *services.EntityService
	sseHandler   *SSEHandler
	mux          *http.ServeMux
	server       *http.Server
	bindAddr     string
}

func NewServer(dbService *db.Service, nc *nats.Conn, natsEmbedded *embeddednats.EmbeddedNATS) (*Server, error) {
	s := &Server{
		db:           dbService,
		nc:           nc,
		natsEmbedded: natsEmbedded,
		orgSvc:       services.NewOrganizationService(dbService.GetDB()),
		entitySvc:    services.NewEntityService(dbService.GetDB(), natsEmbedded),
		sseHandler:   NewSSEHandler(natsEmbedded.Connection(), natsEmbedded.JetStream()),
		mux:          http.NewServeMux(),
	}

	s.setupRoutes()
	return s, nil
}

// NewWebService creates a new web service with environment-based configuration
func NewWebService(dbService *db.Service, nc *nats.Conn, natsEmbedded *embeddednats.EmbeddedNATS) (*Server, error) {
	server, err := NewServer(dbService, nc, natsEmbedded)
	if err != nil {
		return nil, err
	}

	// Configure bind address from environment
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")
	server.bindAddr = fmt.Sprintf("%s:%s", host, port)

	return server, nil
}

// getEnv gets environment variable with fallback
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Name returns the service name (implements Service interface)
func (s *Server) Name() string {
	return "web-server"
}

// Start initializes and starts the web service (implements Service interface)
func (s *Server) Start(ctx context.Context) error {
	s.server = &http.Server{
		Addr:    s.bindAddr,
		Handler: s.mux,
	}

	go func() {
		logger.Infow("Starting web server", "bind_addr", s.bindAddr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorw("Web server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the web service (implements Service interface)
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		logger.Info("Shutting down web server...")
		return s.server.Shutdown(ctx)
	}
	return nil
}

// HealthCheck returns the health status of the web service (implements Service interface)
func (s *Server) HealthCheck() error {
	// Simple check that server is configured
	if s.server == nil {
		return fmt.Errorf("web server not initialized")
	}
	return nil
}

func (s *Server) setupRoutes() {
	// Serve static files
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("pkg/services/web/static"))))

	// Pages
	s.mux.HandleFunc("/", s.handleEntitiesPage)
	s.mux.HandleFunc("/organizations", s.handleEntitiesPage)
	s.mux.HandleFunc("/organizations/entities/new", s.handleEntityForm)
	s.mux.HandleFunc("/organizations/entities/edit", s.handleEntityForm)
	s.mux.HandleFunc("/organizations/new", s.handleOrganizationForm)
	s.mux.HandleFunc("/organizations/edit/", s.handleOrganizationEdit)
	s.mux.HandleFunc("/organizations/cancel/", s.handleOrganizationCancel)
	s.mux.HandleFunc("/fleet", s.handleFleetPage)
	s.mux.HandleFunc("/fleet/edit/", s.handleFleetEdit)
	s.mux.HandleFunc("/fleet/cancel/", s.handleFleetCancel)
	s.mux.HandleFunc("/streams", s.handleStreamsPage)
	s.mux.HandleFunc("/overwatch", s.handleOverwatchPage)

	// Web API endpoints (for Datastar/SSE)
	s.mux.HandleFunc("/api/organizations", s.handleAPIOrganizations)
	s.mux.HandleFunc("/api/organizations/update", s.handleAPIOrganizationUpdate) // Update org (org_id in form data)
	s.mux.HandleFunc("/api/organizations/", s.handleAPIOrganization)             // For specific organization operations
	s.mux.HandleFunc("/api/entities", s.handleAPIEntities)
	s.mux.HandleFunc("/api/entities/", s.handleAPIEntity)         // For specific entity operations
	s.mux.HandleFunc("/api/fleet", s.handleAPIFleet)              // Create and list fleet entities
	s.mux.HandleFunc("/api/fleet/update", s.handleAPIFleetUpdate) // Update fleet entity
	s.mux.HandleFunc("/api/fleet/", s.handleAPIFleetEntity)       // Delete fleet entity
	s.mux.HandleFunc("/api/overwatch/kv", s.handleAPIOverwatchKV)
	s.mux.HandleFunc("/api/overwatch/kv/watch", s.handleAPIOverwatchKVWatch)
	s.mux.HandleFunc("/api/overwatch/kv/debug", s.handleAPIOverwatchKVDebug)

	// SSE endpoint for streams
	s.mux.HandleFunc("/api/streams/sse", s.handleStreamSSE)

	// REST API v1 endpoints
	s.mux.HandleFunc("/api/v1/health", s.handleHealthCheck)
	s.mux.HandleFunc("/api/v1/organizations", s.handleAPIV1Organizations)
	s.mux.HandleFunc("/api/v1/entities", s.handleAPIV1Entities)
}

// Page handlers

func (s *Server) handleEntitiesPage(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")

	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Load all entities by default
	var entities []ontology.Entity
	if orgID != "" {
		// If org_id is provided, filter by that org
		entities, err = s.entitySvc.ListEntities(orgID)
	} else {
		// Otherwise load all entities
		entities, err = s.entitySvc.ListAllEntities()
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

func (s *Server) handleEntityForm(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	var entity *ontology.Entity
	isEdit := false

	if entityID != "" {
		isEdit = true
		e, err := s.entitySvc.GetEntity(orgID, entityID)
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

func (s *Server) handleStreamsPage(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleOrganizationForm(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleOverwatchPage(w http.ResponseWriter, r *http.Request) {
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

// API handlers

func (s *Server) handleAPIOrganizations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		orgs, err := s.orgSvc.ListOrganizations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := templates.OrganizationsTable(orgs, "")
			err := sse.PatchComponent(r.Context(), component,
				datastar.WithSelector("#org-table"),
				datastar.WithMode(datastar.ElementPatchModeInner))
			if err != nil {
				logger.Infof("Error patching organizations: %v", err)
			}
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organizations": orgs,
		})

	case "POST":
		// Log request details for debugging
		logger.Infof("[API] POST /api/organizations - Content-Type: %s, Accept: %s",
			r.Header.Get("Content-Type"), r.Header.Get("Accept"))

		// Parse form data
		if err := r.ParseForm(); err != nil {
			logger.Infow("[API] Error parsing form: %v", err)
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		// Create organization request
		req := &ontology.CreateOrganizationRequest{
			Name:        r.FormValue("name"),
			OrgType:     r.FormValue("org_type"),
			Description: r.FormValue("description"),
		}

		logger.Infof("[API] Creating organization: name=%s, type=%s", req.Name, req.OrgType)

		// Create the organization
		org, err := s.orgSvc.CreateOrganization(req)
		if err != nil {
			logger.Infof("[API] Error creating organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infof("[API] Organization created: %s (ID: %s)", org.Name, org.OrgID)

		// Always send SSE response (Datastar forms always expect SSE)
		logger.Infow("[API] Creating SSE connection for response")
		sse := datastar.NewServerSentEventGenerator(w, r)
		logger.Infow("[API] SSE generator created successfully")

		// Insert the new organization row before the form row
		logger.Infow("[API] Rendering organization row component")
		component := templates.OrganizationRow(*org, org.OrgID)

		logger.Infow("[API] Patching component with selector '#new-org-form-row', mode: before")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#new-org-form-row"),
			datastar.WithMode(datastar.ElementPatchModeBefore)); err != nil {
			logger.Infof("[API] ERROR inserting org row: %v", err)
			return
		}

		// Reset the form after successful submission
		logger.Infow("[API] Resetting form via ExecuteScript")
		if err := sse.ExecuteScript("document.getElementById('new-org-form').reset()"); err != nil {
			logger.Infof("[API] ERROR resetting form: %v", err)
		}

		logger.Infow("[API] ✓ SSE patch sent successfully - new row appended and form reset")
	}
}

func (s *Server) handleOrganizationEdit(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from path: /organizations/edit/{orgID}
	orgID := r.URL.Path[len("/organizations/edit/"):]
	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	// Fetch the organization
	org, err := s.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[EDIT] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Infof("[EDIT] Returning edit row for organization: %s", org.Name)

	// Return the edit row component via SSE using Replace mode to force re-initialization of event listeners
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationEditRow(*org)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		logger.Infof("[EDIT] Error patching edit row: %v", err)
	}
}

func (s *Server) handleOrganizationCancel(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from path: /organizations/cancel/{orgID}
	orgID := r.URL.Path[len("/organizations/cancel/"):]
	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	// Fetch the organization
	org, err := s.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[CANCEL] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Infof("[CANCEL] Returning normal row for organization: %s", org.Name)

	// Return the normal row component via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[CANCEL] Error patching normal row: %v", err)
	}
}

// Fleet handlers

func (s *Server) handleFleetPage(w http.ResponseWriter, r *http.Request) {
	// Fetch all organizations for the dropdown
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch all entities
	entities, err := s.entitySvc.ListAllEntities()
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

func (s *Server) handleFleetEdit(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path: /fleet/edit/{entityID}
	entityID := r.URL.Path[len("/fleet/edit/"):]
	if entityID == "" {
		http.Error(w, "Entity ID required", http.StatusBadRequest)
		return
	}

	// Fetch all organizations for the dropdown
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infof("[FLEET-EDIT] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the entity - we need to find its org_id first
	// Try all organizations to find the entity
	var entity *ontology.Entity
	for _, org := range orgs {
		e, err := s.entitySvc.GetEntity(org.OrgID, entityID)
		if err == nil {
			entity = e
			break
		}
	}

	if entity == nil {
		logger.Infof("[FLEET-EDIT] Entity %s not found", entityID)
		http.Error(w, "Entity not found", http.StatusNotFound)
		return
	}

	logger.Infof("[FLEET-EDIT] Returning edit row for entity: %s", entity.EntityID)

	// Return the edit row component via SSE using Replace mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetEditRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		logger.Infof("[FLEET-EDIT] Error patching edit row: %v", err)
	}
}

func (s *Server) handleFleetCancel(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path: /fleet/cancel/{entityID}
	entityID := r.URL.Path[len("/fleet/cancel/"):]
	if entityID == "" {
		http.Error(w, "Entity ID required", http.StatusBadRequest)
		return
	}

	// Fetch all organizations for the dropdown
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infof("[FLEET-CANCEL] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the entity
	var entity *ontology.Entity
	for _, org := range orgs {
		e, err := s.entitySvc.GetEntity(org.OrgID, entityID)
		if err == nil {
			entity = e
			break
		}
	}

	if entity == nil {
		logger.Infof("[FLEET-CANCEL] Entity %s not found", entityID)
		http.Error(w, "Entity not found", http.StatusNotFound)
		return
	}

	logger.Infof("[FLEET-CANCEL] Returning normal row for entity: %s", entity.EntityID)

	// Return the normal row component via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[FLEET-CANCEL] Error patching normal row: %v", err)
	}
}

func (s *Server) handleAPIOrganizationUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var orgID string
	updates := make(map[string]interface{})

	// Try to parse as form data first (for form-based submissions)
	if err := r.ParseForm(); err == nil && r.Form.Get("org_id") != "" {
		// Form data submission
		orgID = r.FormValue("org_id")
		updates["name"] = r.FormValue("name")
		updates["org_type"] = r.FormValue("org_type")
		updates["description"] = r.FormValue("description")

		updates["description"] = r.FormValue("description")

		logger.Infof("[API] PUT /api/organizations/update (form data, org_id=%s)", orgID)
	} else {
		// Read JSON body (Datastar sends signals as JSON)
		signals := make(map[string]interface{})
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&signals); err != nil {
			logger.Infof("[API] Error reading JSON: %v", err)
			http.Error(w, "Invalid request data", http.StatusBadRequest)
			return
		}

		// Extract org_id from signals
		if id, ok := signals["org_id"].(string); ok && id != "" {
			orgID = id
		}

		if orgID == "" {
			http.Error(w, "Organization ID required", http.StatusBadRequest)
			return
		}

		// Extract update fields from signals (using simplified signal names)
		if name, ok := signals["edit_name"]; ok {
			updates["name"] = name
		}
		if orgType, ok := signals["edit_org_type"]; ok {
			updates["org_type"] = orgType
		}
		if description, ok := signals["edit_description"]; ok {
			updates["description"] = description
		}

		logger.Infof("[API] PUT /api/organizations/update (JSON, org_id=%s)", orgID)
	}

	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	logger.Infof("[API] Updating organization with: name=%v, org_type=%v, description=%v",
		updates["name"], updates["org_type"], updates["description"])

	// Update the organization
	if err := s.orgSvc.UpdateOrganization(orgID, updates); err != nil {
		logger.Infof("[API] Error updating organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the updated organization
	org, err := s.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[API] Error fetching updated organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

	// Return the updated row via SSE using Morph mode for intelligent DOM diffing
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[API] Error patching updated row: %v", err)
		return
	}

	logger.Infow("[API] ✓ Organization row updated via SSE with Morph mode")
}

func (s *Server) handleAPIOrganization(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from path: /api/organizations/{orgID}
	orgID := r.URL.Path[len("/api/organizations/"):]
	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "PUT":
		logger.Infof("[API] PUT /api/organizations/%s", orgID)

		// Parse form data
		if err := r.ParseForm(); err != nil {
			logger.Infof("[API] Error parsing form: %v", err)
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		// Build updates map - always include required fields
		updates := make(map[string]interface{})

		// Required fields - always include
		updates["name"] = r.FormValue("name")
		updates["org_type"] = r.FormValue("org_type")

		// Optional field - include even if empty to allow clearing
		updates["description"] = r.FormValue("description")

		logger.Infof("[API] Updating organization with: name=%s, org_type=%s, description=%s",
			updates["name"], updates["org_type"], updates["description"])

		// Update the organization
		if err := s.orgSvc.UpdateOrganization(orgID, updates); err != nil {
			logger.Infof("[API] Error updating organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch the updated organization
		org, err := s.orgSvc.GetOrganization(orgID)
		if err != nil {
			logger.Infof("[API] Error fetching updated organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infof("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

		// Return the updated row via SSE using Morph mode for intelligent DOM diffing
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.OrganizationRow(*org, "")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#org-row-"+orgID),
			datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
			logger.Infof("[API] Error patching updated row: %v", err)
			return
		}

		logger.Infow("[API] ✓ Organization row updated via SSE with Morph mode")

	case "DELETE":
		logger.Infof("[API] DELETE /api/organizations/%s", orgID)
		logger.Infof("[API] Accept header: %s", r.Header.Get("Accept"))

		// Delete the organization
		if err := s.orgSvc.DeleteOrganization(orgID); err != nil {
			logger.Infof("[API] Error deleting organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infof("[API] Organization deleted: %s", orgID)

		// If Datastar, remove the row from the UI
		acceptHeader := r.Header.Get("Accept")
		if acceptHeader != "" && (acceptHeader == "text/event-stream" || strings.Contains(acceptHeader, "text/event-stream")) {
			logger.Infow("[API] Sending SSE response to remove row")
			sse := datastar.NewServerSentEventGenerator(w, r)
			err := sse.PatchElements("",
				datastar.WithSelector("#org-row-"+orgID),
				datastar.WithMode(datastar.ElementPatchModeRemove))
			if err != nil {
				logger.Infof("[API] Error removing organization row: %v", err)
			} else {
				logger.Infow("[API] ✓ SSE response sent successfully")
			}
			return
		}

		logger.Infow("[API] No SSE request detected, returning JSON")
		// Otherwise return JSON success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIEntities(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")

	switch r.Method {
	case "GET":
		var entities []ontology.Entity
		var err error

		if orgID != "" {
			// If org_id is provided, filter by that org
			entities, err = s.entitySvc.ListEntities(orgID)
		} else {
			// Otherwise load all entities
			entities, err = s.entitySvc.ListAllEntities()
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := templates.EntitiesTable(orgID, entities)
			err := sse.PatchComponent(r.Context(), component,
				datastar.WithSelector("#entities-content"),
				datastar.WithMode(datastar.ElementPatchModeInner))
			if err != nil {
				logger.Infof("Error patching entities: %v", err)
			}
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entities": entities,
		})

	case "POST":
		if orgID == "" {
			http.Error(w, "org_id required for creating entities", http.StatusBadRequest)
			return
		}

		// Parse form data
		r.ParseForm()

		// Create entity request
		req := &ontology.CreateEntityRequest{
			EntityType: r.FormValue("entity_type"),
			Status:     r.FormValue("status"),
			Priority:   r.FormValue("priority"),
		}

		// Handle name
		req.Name = r.FormValue("name")

		// Handle position data
		if lat := r.FormValue("latitude"); lat != "" {
			if lon := r.FormValue("longitude"); lon != "" {
				latVal, _ := strconv.ParseFloat(lat, 64)
				lonVal, _ := strconv.ParseFloat(lon, 64)
				req.Position = &ontology.Position{
					Latitude:  latVal,
					Longitude: lonVal,
				}
				if alt := r.FormValue("altitude"); alt != "" {
					altVal, _ := strconv.ParseFloat(alt, 64)
					req.Position.Altitude = altVal
				}
			}
		}

		// Handle metadata
		metadata := make(map[string]interface{})

		// Add velocity if provided
		if vel := r.FormValue("velocity"); vel != "" {
			metadata["velocity"], _ = strconv.ParseFloat(vel, 64)
		}

		// Add heading if provided
		if heading := r.FormValue("heading"); heading != "" {
			metadata["heading"], _ = strconv.ParseFloat(heading, 64)
		}

		// Add is_live
		metadata["is_live"] = r.FormValue("is_live") == "true"

		req.Metadata = metadata

		// Create the entity
		entity, err := s.entitySvc.CreateEntity(orgID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Datastar, return SSE format with new row
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := templates.EntityRow(orgID, *entity)
			err := sse.PatchComponent(r.Context(), component,
				datastar.WithSelector("#entity-table tbody"),
				datastar.WithMode(datastar.ElementPatchModeAppend))
			if err != nil {
				logger.Infow("Error patching new entity: %v", err)
			}

			// Close the modal
			sse.PatchElements("",
				datastar.WithSelector("#entity-form-modal"),
				datastar.WithMode(datastar.ElementPatchModeInner))
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entity)
	}
}

func (s *Server) handleAPIEntity(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path
	entityID := r.URL.Path[len("/api/entities/"):]
	orgID := r.URL.Query().Get("org_id")

	if orgID == "" || entityID == "" {
		http.Error(w, "org_id and entity_id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "PUT":
		// Parse form data
		r.ParseForm()

		// Create updates map for UpdateEntity
		updates := make(map[string]interface{})

		// Add name if provided
		if name := r.FormValue("name"); name != "" {
			updates["name"] = name
		}

		// Add status if provided
		if status := r.FormValue("status"); status != "" {
			updates["status"] = status
		}

		// Add priority if provided
		if priority := r.FormValue("priority"); priority != "" {
			updates["priority"] = priority
		}

		// Handle position data
		if lat := r.FormValue("latitude"); lat != "" {
			if latVal, err := strconv.ParseFloat(lat, 64); err == nil {
				updates["latitude"] = latVal
			}
		}
		if lon := r.FormValue("longitude"); lon != "" {
			if lonVal, err := strconv.ParseFloat(lon, 64); err == nil {
				updates["longitude"] = lonVal
			}
		}
		if alt := r.FormValue("altitude"); alt != "" {
			if altVal, err := strconv.ParseFloat(alt, 64); err == nil {
				updates["altitude"] = altVal
			}
		}

		// Add velocity if provided
		if vel := r.FormValue("velocity"); vel != "" {
			if velVal, err := strconv.ParseFloat(vel, 64); err == nil {
				updates["velocity"] = velVal
			}
		}

		// Add heading if provided
		if heading := r.FormValue("heading"); heading != "" {
			if headingVal, err := strconv.ParseFloat(heading, 64); err == nil {
				updates["heading"] = headingVal
			}
		}

		// Add is_live
		updates["is_live"] = r.FormValue("is_live") == "true"

		// Handle metadata - only add if there are actual metadata fields
		metadata := make(map[string]interface{})
		// Add any additional metadata fields here if needed
		if len(metadata) > 0 {
			updates["metadata"] = metadata
		}

		// Update the entity
		entity, err := s.entitySvc.UpdateEntity(orgID, entityID, updates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Datastar, return the updated row
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := templates.EntityRow(orgID, *entity)
			err := sse.PatchComponent(r.Context(), component,
				datastar.WithSelector(fmt.Sprintf("#entity-%s", entityID)),
				datastar.WithMode(datastar.ElementPatchModeOuter))
			if err != nil {
				logger.Infow("Error patching updated entity: %v", err)
			}

			// Close the modal
			sse.PatchElements("",
				datastar.WithSelector("#entity-form-modal"),
				datastar.WithMode(datastar.ElementPatchModeInner))
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entity)

	case "DELETE":
		err := s.entitySvc.DeleteEntity(orgID, entityID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Datastar, remove the element
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			err := sse.PatchElements("",
				datastar.WithSelector(fmt.Sprintf("#entity-%s", entityID)),
				datastar.WithMode(datastar.ElementPatchModeRemove))
			if err != nil {
				logger.Infow("Error removing entity: %v", err)
			}
			return
		}

		// Otherwise return success JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// Fleet API handler
func (s *Server) handleAPIFleetUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var entityID string
	var orgID string
	updates := make(map[string]interface{})

	// Read JSON body (Datastar sends signals as JSON)
	signals := make(map[string]interface{})
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&signals); err != nil {
		logger.Infow("[FLEET-API] Error reading JSON: %v", err)
		http.Error(w, "Invalid request data", http.StatusBadRequest)
		return
	}

	// Extract entity_id and org_id from signals
	if id, ok := signals["entity_id"].(string); ok && id != "" {
		entityID = id
	}
	if oid, ok := signals["edit_org_id"].(string); ok && oid != "" {
		orgID = oid
	}

	if entityID == "" || orgID == "" {
		http.Error(w, "Entity ID and Organization ID required", http.StatusBadRequest)
		return
	}

	// Extract update fields from signals
	if name, ok := signals["edit_name"]; ok {
		if str, ok := name.(string); ok {
			updates["name"] = str
		}
	}
	if entityType, ok := signals["edit_entity_type"]; ok {
		updates["entity_type"] = entityType
	}
	if status, ok := signals["edit_status"]; ok {
		updates["status"] = status
	}
	if priority, ok := signals["edit_priority"]; ok {
		updates["priority"] = priority
	}
	if isLive, ok := signals["edit_is_live"]; ok {
		// Convert string "true"/"false" to boolean
		if str, ok := isLive.(string); ok {
			updates["is_live"] = str == "true"
		} else {
			updates["is_live"] = isLive
		}
	}

	logger.Infow("[FLEET-API] PUT /api/fleet/update (entity_id=%s, org_id=%s)", entityID, orgID)
	logger.Infow("[FLEET-API] Updating entity with: %v", updates)

	// Update the entity
	entity, err := s.entitySvc.UpdateEntity(orgID, entityID, updates)
	if err != nil {
		logger.Infow("[FLEET-API] Error updating entity: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infow("[FLEET-API] Entity updated: %s (ID: %s)", entity.EntityType, entity.EntityID)

	// Fetch all organizations for the dropdown in the returned row
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infow("[FLEET-API] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the updated row via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infow("[FLEET-API] Error patching updated row: %v", err)
		return
	}

	logger.Infow("[FLEET-API] ✓ Fleet entity row updated via SSE with Morph mode")
}

// Fleet API handlers for create and list
func (s *Server) handleAPIFleet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Fetch all entities
		entities, err := s.entitySvc.ListAllEntities()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch all organizations for rendering
		orgs, err := s.orgSvc.ListOrganizations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := templates.FleetTable(orgs, entities)
			err := sse.PatchComponent(r.Context(), component,
				datastar.WithSelector("#fleet-table"),
				datastar.WithMode(datastar.ElementPatchModeInner))
			if err != nil {
				logger.Infow("Error patching fleet: %v", err)
			}
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entities": entities,
		})

	case "POST":
		// Log request details for debugging
		logger.Infow("[FLEET-API] POST /api/fleet - Content-Type: %s, Accept: %s",
			r.Header.Get("Content-Type"), r.Header.Get("Accept"))

		// Parse form data
		if err := r.ParseForm(); err != nil {
			logger.Infow("[FLEET-API] Error parsing form: %v", err)
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		// Get org_id from form
		orgID := r.FormValue("org_id")
		if orgID == "" {
			http.Error(w, "org_id required", http.StatusBadRequest)
			return
		}

		// Create entity request
		req := &ontology.CreateEntityRequest{
			Name:       r.FormValue("name"),
			EntityType: r.FormValue("entity_type"),
			Status:     r.FormValue("status"),
			Priority:   r.FormValue("priority"),
		}

		// Handle position data if provided
		if lat := r.FormValue("latitude"); lat != "" {
			if lon := r.FormValue("longitude"); lon != "" {
				latVal, _ := strconv.ParseFloat(lat, 64)
				lonVal, _ := strconv.ParseFloat(lon, 64)
				req.Position = &ontology.Position{
					Latitude:  latVal,
					Longitude: lonVal,
				}
				if alt := r.FormValue("altitude"); alt != "" {
					altVal, _ := strconv.ParseFloat(alt, 64)
					req.Position.Altitude = altVal
				}
			}
		}

		// Handle is_live
		metadata := make(map[string]interface{})
		metadata["is_live"] = r.FormValue("is_live") == "true"
		req.Metadata = metadata

		logger.Infow("[FLEET-API] Creating fleet entity: type=%s, org_id=%s", req.EntityType, orgID)

		// Create the entity
		entity, err := s.entitySvc.CreateEntity(orgID, req)
		if err != nil {
			logger.Infow("[FLEET-API] Error creating entity: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infow("[FLEET-API] Entity created: %s (ID: %s)", entity.EntityType, entity.EntityID)

		// Fetch all organizations for rendering the row
		orgs, err := s.orgSvc.ListOrganizations()
		if err != nil {
			logger.Infow("[FLEET-API] Error fetching organizations: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Always send SSE response (Datastar forms always expect SSE)
		logger.Infow("[FLEET-API] Creating SSE connection for response")
		sse := datastar.NewServerSentEventGenerator(w, r)

		// Insert the new fleet row before the form row
		logger.Infow("[FLEET-API] Rendering fleet row component")
		component := templates.FleetRow(orgs, *entity)

		logger.Infow("[FLEET-API] Patching component with selector '#new-fleet-form-row', mode: before")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#new-fleet-form-row"),
			datastar.WithMode(datastar.ElementPatchModeBefore)); err != nil {
			logger.Infow("[FLEET-API] ERROR inserting fleet row: %v", err)
			return
		}

		// Reset the form after successful submission
		logger.Infow("[FLEET-API] Resetting form via ExecuteScript")
		if err := sse.ExecuteScript("document.getElementById('new-fleet-form').reset()"); err != nil {
			logger.Infow("[FLEET-API] ERROR resetting form: %v", err)
		}

		logger.Infow("[FLEET-API] ✓ SSE patch sent successfully - new row appended and form reset")
	}
}

// Fleet API handler for specific entity operations (delete)
func (s *Server) handleAPIFleetEntity(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path: /api/fleet/{entityID}
	entityID := r.URL.Path[len("/api/fleet/"):]
	if entityID == "" || entityID == "update" {
		http.Error(w, "Entity ID required", http.StatusBadRequest)
		return
	}

	// Get org_id from query
	orgID := r.URL.Query().Get("org_id")

	switch r.Method {
	case "DELETE":
		logger.Infow("[FLEET-API] DELETE /api/fleet/%s?org_id=%s", entityID, orgID)
		logger.Infow("[FLEET-API] Accept header: %s", r.Header.Get("Accept"))

		// If org_id not provided, try to find it
		if orgID == "" {
			orgs, err := s.orgSvc.ListOrganizations()
			if err != nil {
				logger.Infow("[FLEET-API] Error fetching organizations: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Find the entity's org_id
			for _, org := range orgs {
				if _, err := s.entitySvc.GetEntity(org.OrgID, entityID); err == nil {
					orgID = org.OrgID
					break
				}
			}
		}

		if orgID == "" {
			http.Error(w, "Could not find entity", http.StatusNotFound)
			return
		}

		// Delete the entity
		if err := s.entitySvc.DeleteEntity(orgID, entityID); err != nil {
			logger.Infow("[FLEET-API] Error deleting entity: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infow("[FLEET-API] Entity deleted: %s", entityID)

		// If Datastar, remove the row from the UI
		acceptHeader := r.Header.Get("Accept")
		if acceptHeader != "" && (acceptHeader == "text/event-stream" || strings.Contains(acceptHeader, "text/event-stream")) {
			logger.Infow("[FLEET-API] Sending SSE response to remove row")
			sse := datastar.NewServerSentEventGenerator(w, r)
			err := sse.PatchElements("",
				datastar.WithSelector("#fleet-row-"+entityID),
				datastar.WithMode(datastar.ElementPatchModeRemove))
			if err != nil {
				logger.Infow("[FLEET-API] Error removing fleet row: %v", err)
			} else {
				logger.Infow("[FLEET-API] ✓ SSE response sent successfully")
			}
			return
		}

		logger.Infow("[FLEET-API] No SSE request detected, returning JSON")
		// Otherwise return JSON success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// SSE handler for real-time streams
func (s *Server) handleStreamSSE(w http.ResponseWriter, r *http.Request) {
	// Delegate to SSE handler
	s.sseHandler.StreamMessages(w, r)
}

// API handler for Overwatch KV store
func (s *Server) handleAPIOverwatchKV(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all keys from the KV store
	kv := s.natsEmbedded.KeyValue()
	if kv == nil {
		http.Error(w, "KV store not initialized", http.StatusInternalServerError)
		return
	}

	// Get all keys using Keys() method
	keys, err := kv.Keys()
	if err != nil {
		logger.Infow("Error fetching KV keys: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch all entries
	var kvEntries []templates.KVEntry
	for _, key := range keys {
		entry, err := kv.Get(key)
		if err != nil {
			logger.Infow("Error getting key %s: %v", key, err)
			continue
		}

		kvEntries = append(kvEntries, templates.KVEntry{
			Key:      key,
			Value:    string(entry.Value()),
			Revision: fmt.Sprintf("%d", entry.Revision()),
			Updated:  entry.Created().Format("15:04:05"),
		})
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.KVStateTable(kvEntries)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#kv-content"),
			datastar.WithMode(datastar.ElementPatchModeInner))
		if err != nil {
			logger.Infow("Error patching KV content: %v", err)
		}
		return
	}

	// Otherwise return JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": kvEntries,
	})
}

// API handler for real-time KV watching via SSE
func (s *Server) handleAPIOverwatchKVWatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify we have a flusher (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Infow("[Overwatch] ERROR: ResponseWriter does not support flushing (SSE won't work)")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// CRITICAL: Set SSE headers BEFORE creating SSE generator or writing anything
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	logger.Infow("[Overwatch] ✓ SSE headers set, establishing connection", "remote_addr", r.RemoteAddr)

	// Create SSE generator AFTER setting headers
	sse := datastar.NewServerSentEventGenerator(w, r)

	// Mutex to synchronize writes to ResponseWriter from multiple goroutines
	var writeMutex sync.Mutex

	// Send an immediate comment to establish the SSE stream in the browser
	writeMutex.Lock()
	fmt.Fprintf(w, ": SSE connection established\n\n")
	flusher.Flush()
	writeMutex.Unlock()
	logger.Infow("SSE client connected", "component", "Overwatch", "remote_addr", r.RemoteAddr)

	// Local cache of all KV data: entityID -> key -> data
	// This allows us to reconstruct a single entity's state without fetching everything
	localEntityCache := make(map[string]map[string][]byte)

	// Track known entities and orgs to determine patch strategy
	knownEntities := make(map[string]bool)
	knownOrgs := make(map[string]bool)

	// Struct to pass data to renderer
	type RenderPayload struct {
		Snapshot      []shared.EntityState
		TotalEntities int
	}

	// Channel to buffer updates from NATS to SSE
	// Increased buffer size to handle initial state dump and high throughput
	updateChan := make(chan nats.KeyValueEntry, 10000)

	// Channel to send snapshots to the renderer
	// Buffer of 1 allows us to have one snapshot pending while the renderer is busy.
	// If the renderer is too slow, we drop intermediate snapshots (conflation).
	renderChan := make(chan RenderPayload, 1)

	// Start watching for changes in a goroutine
	ctx := r.Context()
	go func() {
		defer close(updateChan)

		// Retry loop for the watcher
		for {
			if ctx.Err() != nil {
				return
			}

			logger.Infow("[Overwatch] KV watcher goroutine started, waiting for changes...")

			watchErr := s.natsEmbedded.WatchKV(ctx, func(key string, entry nats.KeyValueEntry) error {
				select {
				case updateChan <- entry:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})

			if ctx.Err() != nil {
				return
			}

			if watchErr != nil {
				logger.Warnw("[Overwatch] KV watcher stopped unexpectedly, restarting...", "error", watchErr)
			} else {
				logger.Warnw("[Overwatch] KV watcher channel closed unexpectedly, restarting...")
			}

			select {
			case <-time.After(1 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start Renderer Goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-renderChan:
				if !ok {
					return
				}

				// Render and Flush
				s.renderAndFlushSnapshot(w, flusher, &writeMutex, sse, payload.Snapshot, payload.TotalEntities, knownEntities, knownOrgs)
			}
		}
	}()

	// Keep the connection alive with heartbeats
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Flush ticker for batching
	flushTicker := time.NewTicker(50 * time.Millisecond)
	defer flushTicker.Stop()

	dirtyEntities := make(map[string]bool)

	// State Manager Loop (Main Goroutine)
	// This loop MUST be fast to keep up with NATS.
	// It does NO rendering.
	for {
		select {
		case <-ctx.Done():
			logger.Infow("[Overwatch] Client disconnected", "remote_addr", r.RemoteAddr)
			return

		case <-ticker.C:
			writeMutex.Lock()
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
			writeMutex.Unlock()

		case <-flushTicker.C:
			// Periodic flush of dirty entities
			if len(dirtyEntities) > 0 {
				// Create snapshot
				var snapshot []shared.EntityState
				for entityID := range dirtyEntities {
					entityData, exists := localEntityCache[entityID]
					if !exists {
						continue
					}
					// Reconstruct state (fast, just map lookups and struct creation)
					entityState := s.mergeEntityData(entityID, entityData)
					snapshot = append(snapshot, entityState)
				}

				// Try to send to renderer (non-blocking)
				payload := RenderPayload{
					Snapshot:      snapshot,
					TotalEntities: len(localEntityCache),
				}

				select {
				case renderChan <- payload:
					// Success, renderer will handle it
					dirtyEntities = make(map[string]bool)
				default:
					// Renderer is busy, skip this frame (conflation)
					// We keep the entities dirty so they are included in the next snapshot
					logger.Debugw("[Overwatch] Renderer busy, skipping frame (conflation)", "pending_entities", len(dirtyEntities))
				}
			}

		case entry, ok := <-updateChan:
			if !ok {
				logger.Infow("[Overwatch] Update channel closed, stopping SSE stream")
				return
			}

			// Update Cache
			key := entry.Key()
			parts := strings.Split(key, ".")
			entityID := parts[0]

			if localEntityCache[entityID] == nil {
				localEntityCache[entityID] = make(map[string][]byte)
			}

			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				delete(localEntityCache[entityID], key)
				if len(localEntityCache[entityID]) == 0 {
					delete(localEntityCache, entityID)
				}
			} else {
				localEntityCache[entityID][key] = entry.Value()
			}

			// Mark dirty
			dirtyEntities[entityID] = true
		}
	}
}

// Helper to render and flush a snapshot
func (s *Server) renderAndFlushSnapshot(w http.ResponseWriter, flusher http.Flusher, writeMutex *sync.Mutex, sse *datastar.ServerSentEventGenerator, snapshot []shared.EntityState, totalEntities int, knownEntities map[string]bool, knownOrgs map[string]bool) {
	writeMutex.Lock()
	defer writeMutex.Unlock()

	updatesSent := 0

	for _, entityState := range snapshot {
		// Render card
		var cardHTML strings.Builder
		if err := templates.EntityCard(entityState).Render(context.Background(), &cardHTML); err != nil {
			logger.Errorw("Error rendering entity card", "error", err)
			continue
		}

		// Determine patch mode
		var patchMode datastar.PatchElementMode
		var selector string
		entityID := entityState.EntityID

		if !knownEntities[entityID] {
			// New entity
			if !knownOrgs[entityState.OrgID] {
				// Create Org Container
				if len(knownOrgs) == 0 {
					sse.PatchElements("", datastar.WithSelector(".empty-state"), datastar.WithMode(datastar.ElementPatchModeRemove))
				}

				var orgHTML strings.Builder
				orgName := entityState.OrgID
				if entityState.OrgName != "" {
					orgName = entityState.OrgName
				}
				orgHTML.WriteString(fmt.Sprintf(`<div style="margin-bottom: 30px;"><h3 style="color: #0ff; border-bottom: 2px solid #444; padding-bottom: 10px; margin-bottom: 15px;">Organization: %s</h3><div id="org-cards-%s" class="entity-cards-container" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(420px, 1fr)); gap: 15px;"></div></div>`, orgName, entityState.OrgID))

				sse.PatchElements(orgHTML.String(), datastar.WithSelector("#entities-container"), datastar.WithMode(datastar.ElementPatchModeAppend))
				knownOrgs[entityState.OrgID] = true

				// Initialize signal
				sse.PatchSignals(map[string]interface{}{
					fmt.Sprintf("entityStatesByOrg.%s", entityState.OrgID): map[string]interface{}{},
				})
			}

			patchMode = datastar.ElementPatchModeAppend
			selector = fmt.Sprintf("#org-cards-%s", entityState.OrgID)
			knownEntities[entityID] = true
		} else {
			patchMode = datastar.ElementPatchModeMorph
			selector = fmt.Sprintf("#entity-%s", entityID)
		}

		// Patch Element
		if err := sse.PatchElements(cardHTML.String(), datastar.WithSelector(selector), datastar.WithMode(patchMode)); err != nil {
			logger.Debugw("Failed to patch entity", "entity_id", entityID, "error", err)
		}

		// Patch Signal
		sse.PatchSignals(map[string]interface{}{
			fmt.Sprintf("entityStatesByOrg.%s.%s", entityState.OrgID, entityID): entityState,
		})

		updatesSent++
	}

	if updatesSent > 0 {
		// Get total orgs from DB for accurate count
		orgs, _ := s.orgSvc.ListOrganizations()
		totalOrgs := len(orgs)

		sse.PatchSignals(map[string]interface{}{
			"lastUpdate":    time.Now().Format("15:04:05"),
			"totalEntities": totalEntities,
			"totalOrgs":     totalOrgs,
			"_isConnected":  true,
		})
		flusher.Flush()
	}
}

// parseKVEntriesToEntityStates parses KV entries and aggregates them by entity_id
func (s *Server) parseKVEntriesToEntityStates(entries []nats.KeyValueEntry) map[string][]shared.EntityState {
	// First, group entries by entity_id
	entitiesByID := make(map[string]map[string][]byte)

	for _, entry := range entries {
		key := entry.Key()

		// Extract entity_id from key (before first dot or entire key if no dot)
		parts := strings.Split(key, ".")
		entityID := parts[0]

		if entitiesByID[entityID] == nil {
			entitiesByID[entityID] = make(map[string][]byte)
		}

		// Store raw data keyed by full key for later processing
		entitiesByID[entityID][key] = entry.Value()
	}

	// Now build consolidated EntityState objects
	entityStatesByOrg := make(map[string][]shared.EntityState)

	logger.Infow("[Overwatch] Aggregating entities from KV entries", "entity_count", len(entitiesByID), "kv_entry_count", len(entries))

	for entityID, dataMap := range entitiesByID {
		logger.Infow("[Overwatch] Processing entity", "entity_id", entityID, "kv_entry_count", len(dataMap))
		entityState := s.mergeEntityData(entityID, dataMap)

		// Group by org_id
		orgID := entityState.OrgID
		if orgID == "" {
			orgID = "unknown"
		}

		entityStatesByOrg[orgID] = append(entityStatesByOrg[orgID], entityState)
	}

	logger.Infow("[Overwatch] Built entities", "total_entities", len(entitiesByID), "org_count", len(entityStatesByOrg))
	return entityStatesByOrg
}

// mergeEntityData merges separate KV entries into a single EntityState
func (s *Server) mergeEntityData(entityID string, dataMap map[string][]byte) shared.EntityState {
	state := shared.EntityState{
		EntityID:   entityID,
		EntityType: "sensor", // Default type for detection entities
		Status:     "active",
		Priority:   "normal",
		IsLive:     true,
		Components: make(map[string]interface{}),
		Aliases:    make(map[string]string),
		Tags:       []string{},
		Metadata:   make(map[string]interface{}),
		UpdatedAt:  time.Now(),
	}

	// Process each key and merge data
	for key, data := range dataMap {
		// Skip empty data
		if len(data) == 0 {
			continue
		}

		var rawData map[string]interface{}
		if err := json.Unmarshal(data, &rawData); err != nil {
			logger.Warnf("[Overwatch] Failed to unmarshal key %s: %v", key, err)
			continue
		}

		// Extract org_id (check both org_id and organization_id)
		if orgID, ok := rawData["org_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
		}
		if orgID, ok := rawData["organization_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
		}

		// Extract device_id if present
		if deviceID, ok := rawData["device_id"].(string); ok && deviceID != "" {
			state.DeviceID = deviceID
		}

		// Determine data type from key suffix and merge accordingly
		if strings.Contains(key, ".detections.objects") {
			s.mergeDetections(&state, rawData)
		} else if strings.Contains(key, ".analytics.summary") || strings.Contains(key, ".analytics.c4isr_summary") {
			s.mergeAnalytics(&state, rawData)
		} else if strings.Contains(key, ".c4isr.threat_intelligence") {
			s.mergeThreatIntel(&state, rawData)
		} else if !strings.Contains(key, ".") {
			// Single-key entity state (like device.1.1 or full EntityState)
			s.mergeFullState(&state, rawData)
		}
	}

	return state
}

// mergeDetections merges detection data into EntityState
func (s *Server) mergeDetections(state *shared.EntityState, data map[string]interface{}) {
	if trackedObjects, ok := data["tracked_objects"].(map[string]interface{}); ok {
		detectionState := &shared.DetectionState{
			TrackedObjects: make(map[string]shared.TrackedObject),
			Timestamp:      time.Now(),
		}

		for trackID, objData := range trackedObjects {
			if objMap, ok := objData.(map[string]interface{}); ok {
				trackedObj := shared.TrackedObject{
					TrackID:  trackID,
					IsActive: false,
				}

				if label, ok := objMap["label"].(string); ok {
					trackedObj.Label = label
				}
				if conf, ok := objMap["avg_confidence"].(float64); ok {
					trackedObj.AvgConfidence = conf
				}
				if active, ok := objMap["is_active"].(bool); ok {
					trackedObj.IsActive = active
				}
				if threat, ok := objMap["threat_level"].(string); ok {
					trackedObj.ThreatLevel = threat
				}
				if frames, ok := objMap["frame_count"].(float64); ok {
					trackedObj.FrameCount = int(frames)
				}

				detectionState.TrackedObjects[trackID] = trackedObj
			}
		}

		state.Detections = detectionState
	}
}

// mergeAnalytics merges analytics data into EntityState
func (s *Server) mergeAnalytics(state *shared.EntityState, data map[string]interface{}) {
	analyticsState := &shared.AnalyticsState{
		Timestamp: time.Now(),
	}

	if val, ok := data["total_unique_objects"].(float64); ok {
		analyticsState.TotalUniqueObjects = int(val)
	}
	if val, ok := data["total_frames_processed"].(float64); ok {
		analyticsState.TotalFramesProcessed = int(val)
	}
	if val, ok := data["active_objects_count"].(float64); ok {
		analyticsState.ActiveObjectsCount = int(val)
	}
	if val, ok := data["tracked_objects_count"].(float64); ok {
		analyticsState.TrackedObjectsCount = int(val)
	}
	if val, ok := data["active_threat_count"].(float64); ok {
		analyticsState.ActiveThreatCount = int(val)
	}
	if labels, ok := data["label_distribution"].(map[string]interface{}); ok {
		analyticsState.LabelDistribution = make(map[string]int)
		for k, v := range labels {
			if num, ok := v.(float64); ok {
				analyticsState.LabelDistribution[k] = int(num)
			}
		}
	}
	if threats, ok := data["threat_distribution"].(map[string]interface{}); ok {
		analyticsState.ThreatDistribution = make(map[string]int)
		for k, v := range threats {
			if num, ok := v.(float64); ok {
				analyticsState.ThreatDistribution[k] = int(num)
			}
		}
	}
	if ids, ok := data["active_track_ids"].([]interface{}); ok {
		for _, id := range ids {
			if str, ok := id.(string); ok {
				analyticsState.ActiveTrackIDs = append(analyticsState.ActiveTrackIDs, str)
			}
		}
	}

	state.Analytics = analyticsState
}

// mergeThreatIntel merges threat intelligence data into EntityState
func (s *Server) mergeThreatIntel(state *shared.EntityState, data map[string]interface{}) {
	threatIntel := &shared.ThreatIntelState{
		Timestamp: time.Now(),
	}

	if mission, ok := data["mission"].(string); ok {
		threatIntel.Mission = mission
	}

	if summary, ok := data["threat_summary"].(map[string]interface{}); ok {
		threatSummary := &shared.ThreatSummary{}

		if total, ok := summary["total_threats"].(float64); ok {
			threatSummary.TotalThreats = int(total)
		}
		if alert, ok := summary["alert_level"].(string); ok {
			threatSummary.AlertLevel = alert
		}
		if dist, ok := summary["threat_distribution"].(map[string]interface{}); ok {
			threatSummary.ThreatDistribution = make(map[string]int)
			for k, v := range dist {
				if num, ok := v.(float64); ok {
					threatSummary.ThreatDistribution[k] = int(num)
				}
			}
		}

		threatIntel.ThreatSummary = threatSummary
	}

	state.ThreatIntel = threatIntel
}

// mergeFullState merges full entity state data (Python + TelemetryWorker consolidated format)
func (s *Server) mergeFullState(state *shared.EntityState, data map[string]interface{}) {
	// Extract core fields (check both org_id and organization_id)
	if orgID, ok := data["org_id"].(string); ok && orgID != "" {
		state.OrgID = orgID
	}
	if orgID, ok := data["organization_id"].(string); ok && orgID != "" {
		state.OrgID = orgID
	}

	// Extract Name and OrgName if present
	if name, ok := data["name"].(string); ok && name != "" {
		state.Name = name
	}
	if orgName, ok := data["org_name"].(string); ok && orgName != "" {
		state.OrgName = orgName
	}

	if state.OrgID == "" {
		logger.Infow("[Overwatch] mergeFullState: WARNING - No org_id or organization_id in data")
	}

	// Python detection service format (NEW): detections.tracked_objects
	if detectionsData, ok := data["detections"].(map[string]interface{}); ok {
		// Check for tracked_objects (new format)
		if trackedObjects, ok := detectionsData["tracked_objects"].(map[string]interface{}); ok {
			s.mergeDetections(state, map[string]interface{}{"tracked_objects": trackedObjects})
			logger.Infof("[Overwatch] Merged detections.tracked_objects with %d tracked objects", len(trackedObjects))
		} else if objectsData, ok := detectionsData["objects"].(map[string]interface{}); ok {
			// Fallback to old format: detections.objects
			s.mergeDetections(state, map[string]interface{}{"tracked_objects": objectsData})
			logger.Infof("[Overwatch] Merged detections.objects with %d tracked objects", len(objectsData))
		}

		// Check for analytics nested inside detections (new format)
		if analyticsData, ok := detectionsData["analytics"].(map[string]interface{}); ok {
			s.mergeAnalytics(state, analyticsData)
			logger.Infow("[Overwatch] Merged detections.analytics")
		}
	}

	// Python analytics format (OLD): top-level analytics.summary
	if analyticsData, ok := data["analytics"].(map[string]interface{}); ok {
		if summaryData, ok := analyticsData["summary"].(map[string]interface{}); ok {
			s.mergeAnalytics(state, summaryData)
			logger.Infow("[Overwatch] Merged analytics.summary")
		}
	}

	// Python threat intelligence format (NEW): top-level threat_intelligence
	if threatData, ok := data["threat_intelligence"].(map[string]interface{}); ok {
		s.mergeThreatIntel(state, threatData)
		logger.Infow("[Overwatch] Merged threat_intelligence")
	}

	// Python C4ISR format (OLD): c4isr.threat_intelligence
	if c4isrData, ok := data["c4isr"].(map[string]interface{}); ok {
		if threatData, ok := c4isrData["threat_intelligence"].(map[string]interface{}); ok {
			s.mergeThreatIntel(state, threatData)
			logger.Infow("[Overwatch] Merged c4isr.threat_intelligence")
		}
	}

	// Try to unmarshal entire object for telemetry fields (from TelemetryWorker)
	jsonData, _ := json.Marshal(data)
	var fullState shared.EntityState
	if err := json.Unmarshal(jsonData, &fullState); err == nil {
		// Merge telemetry fields
		if fullState.Position != nil {
			state.Position = fullState.Position
		}
		if fullState.Attitude != nil {
			state.Attitude = fullState.Attitude
		}
		if fullState.Power != nil {
			state.Power = fullState.Power
		}
		if fullState.VFR != nil {
			state.VFR = fullState.VFR
		}
		if fullState.VehicleStatus != nil {
			state.VehicleStatus = fullState.VehicleStatus
		}
		if fullState.Mission != nil {
			state.Mission = fullState.Mission
		}
	}
}

// sendEntityStatesUpdate sends an SSE update with entity states using server-side HTML rendering
func (s *Server) sendEntityStatesUpdate(sse *datastar.ServerSentEventGenerator, entityStatesByOrg map[string][]shared.EntityState) error {
	// Calculate totals
	totalEntities := 0
	for _, entities := range entityStatesByOrg {
		totalEntities += len(entities)
	}

	// Get total orgs from DB for accurate count
	orgs, _ := s.orgSvc.ListOrganizations()
	totalOrgs := len(orgs)

	logger.Infow("[Overwatch] Total entities", "total_entities", totalEntities, "total_orgs", totalOrgs)

	// STEP 1: Send metadata as signals (for stats display and debugging)
	signals := map[string]interface{}{
		"entityStatesByOrg": entityStatesByOrg, // For debug panel JSON display
		"_isConnected":      true,
		"lastUpdate":        time.Now().Format("15:04:05"),
		"totalEntities":     totalEntities,
		"totalOrgs":         totalOrgs,
	}

	// Log signal structure before sending (for debugging)
	logger.Infow("[Overwatch] Patching signals", "totalOrgs", totalOrgs, "totalEntities", totalEntities, "lastUpdate", signals["lastUpdate"])

	// Verify entityStatesByOrg structure
	for orgID, entities := range entityStatesByOrg {
		logger.Infow("[Overwatch] Signal data", "org_id", orgID, "entity_count", len(entities))
	}

	if err := sse.PatchSignals(signals); err != nil {
		logger.Infow("[Overwatch] ERROR in PatchSignals", "error", err)
		return err
	}

	logger.Infow("[Overwatch] ✓ Signals patched successfully")

	// STEP 2: Render complete HTML structure (org sections + entity cards) on server
	var htmlBuilder strings.Builder

	if totalEntities == 0 {
		// Empty state
		htmlBuilder.WriteString(`<div class="empty-state" style="color: #888; padding: 40px; text-align: center;">`)
		htmlBuilder.WriteString(`<p>No entity states in global store. Waiting for telemetry data...</p>`)
		htmlBuilder.WriteString(`<p style="font-size: 10px; margin-top: 10px;">Server-side rendering via SSE</p>`)
		htmlBuilder.WriteString(`</div>`)
	} else {
		// Render organization sections with entity cards
		for orgID, entities := range entityStatesByOrg {
			// Org section
			orgName := orgID
			if len(entities) > 0 && entities[0].OrgName != "" {
				orgName = entities[0].OrgName
			}

			htmlBuilder.WriteString(`<div style="margin-bottom: 30px;">`)
			htmlBuilder.WriteString(fmt.Sprintf(`<h3 style="color: #0ff; border-bottom: 2px solid #444; padding-bottom: 10px; margin-bottom: 15px; display: flex; justify-content: space-between; align-items: center;"><span>Organization: %s</span>`, orgName))
			htmlBuilder.WriteString(fmt.Sprintf(`<span style="font-size: 14px; color: #888;">%d entities</span>`, len(entities)))
			htmlBuilder.WriteString(`</h3>`)

			// Entity cards grid
			htmlBuilder.WriteString(fmt.Sprintf(`<div id="org-cards-%s" class="entity-cards-container" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(420px, 1fr)); gap: 15px;">`, orgID))

			// Render each entity card
			for _, entity := range entities {
				// Ensure orgID is set
				if entity.OrgID == "" {
					entity.OrgID = orgID
				}

				if err := templates.EntityCard(entity).Render(context.Background(), &htmlBuilder); err != nil {
					logger.Infof("[Overwatch] Error rendering entity card %s: %v", entity.EntityID, err)
					continue
				}
			}

			htmlBuilder.WriteString(`</div>`) // Close entity-cards-container
			htmlBuilder.WriteString(`</div>`) // Close org section
		}
	}

	html := htmlBuilder.String()
	logger.Infow("[Overwatch] Rendered HTML", "bytes", len(html), "totalEntities", totalEntities)

	// STEP 3: Patch the entire entities container with inner mode
	err := sse.PatchElements(html,
		datastar.WithSelector("#entities-container"),
		datastar.WithMode(datastar.ElementPatchModeInner))

	if err != nil {
		logger.Infow("[Overwatch] ERROR in PatchElements", "error", err)
		return err
	}

	logger.Infow("[Overwatch] ✓ SSE update complete: signals + HTML replaced")
	return nil
}

// API handler for debugging KV data structure
func (s *Server) handleAPIOverwatchKVDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all KV entries
	entries, err := s.natsEmbedded.GetAllKVEntries()
	if err != nil {
		logger.Infof("[Overwatch Debug] Error fetching KV entries: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse into entity states
	entityStatesByOrg := s.parseKVEntriesToEntityStates(entries)

	// Create the same structure we send via SSE
	response := map[string]interface{}{
		"entityStatesByOrg": entityStatesByOrg,
		"lastUpdate":        time.Now().Format("15:04:05"),
		"_isConnected":      true,
		"totalOrgs":         len(entityStatesByOrg),
		"totalEntities":     0,
	}

	for _, entities := range entityStatesByOrg {
		response["totalEntities"] = response["totalEntities"].(int) + len(entities)
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		logger.Infof("[Overwatch Debug] Error encoding JSON: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// REST API v1 handlers (with authentication)

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	health := shared.HealthStatus{
		Status:    "healthy",
		Service:   "constellation-overwatch",
		Timestamp: time.Now(),
		Details:   make(map[string]string),
	}

	// Check database
	if err := s.db.GetDB().Ping(); err != nil {
		health.Status = "unhealthy"
		health.Details["database"] = "unhealthy: " + err.Error()
	} else {
		health.Details["database"] = "healthy"
	}

	// Check NATS
	if err := s.natsEmbedded.HealthCheck(); err != nil {
		health.Status = "unhealthy"
		health.Details["nats"] = "unhealthy: " + err.Error()
	} else {
		health.Details["nats"] = "healthy"
	}

	statusCode := http.StatusOK
	if health.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	sendSuccess(w, statusCode, health)
}

func (s *Server) handleAPIV1Organizations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		middleware.BearerAuth(s.createOrganization)(w, r)
	case http.MethodGet:
		if r.URL.Query().Get("org_id") != "" {
			middleware.BearerAuth(s.getOrganization)(w, r)
		} else {
			middleware.BearerAuth(s.listOrganizations)(w, r)
		}
	case http.MethodDelete:
		middleware.BearerAuth(s.deleteOrganization)(w, r)
	default:
		sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (s *Server) handleAPIV1Entities(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		middleware.BearerAuth(s.createEntity)(w, r)
	case http.MethodGet:
		if r.URL.Query().Get("entity_id") != "" {
			middleware.BearerAuth(s.getEntity)(w, r)
		} else {
			middleware.BearerAuth(s.listEntities)(w, r)
		}
	case http.MethodPut:
		middleware.BearerAuth(s.updateEntity)(w, r)
	case http.MethodDelete:
		middleware.BearerAuth(s.deleteEntity)(w, r)
	default:
		sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// REST API v1 handler implementations

func (s *Server) createOrganization(w http.ResponseWriter, r *http.Request) {
	var req ontology.CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	org, err := s.orgSvc.CreateOrganization(&req)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	sendSuccess(w, http.StatusCreated, org)
}

func (s *Server) listOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, orgs)
}

func (s *Server) getOrganization(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	org, err := s.orgSvc.GetOrganization(orgID)
	if err != nil {
		if err.Error() == "organization not found" {
			sendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		}
		return
	}

	sendSuccess(w, http.StatusOK, org)
}

func (s *Server) deleteOrganization(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	err := s.orgSvc.DeleteOrganization(orgID)
	if err != nil {
		if err.Error() == "organization not found" {
			sendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		}
		return
	}

	// If this is a Datastar request, remove the row from the UI
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		err := sse.PatchElements("",
			datastar.WithSelector("#org-row-"+orgID),
			datastar.WithMode(datastar.ElementPatchModeRemove))
		if err != nil {
			logger.Infof("Error removing organization row: %v", err)
		}
		return
	}

	sendSuccess(w, http.StatusOK, map[string]string{"message": "Organization deleted successfully"})
}

func (s *Server) createEntity(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	var req ontology.CreateEntityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	entity, err := s.entitySvc.CreateEntity(orgID, &req)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	sendSuccess(w, http.StatusCreated, entity)
}

func (s *Server) listEntities(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	entities, err := s.entitySvc.ListEntities(orgID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, entities)
}

func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

	entity, err := s.entitySvc.GetEntity(orgID, entityID)
	if err != nil {
		if err.Error() == "entity not found" {
			sendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		}
		return
	}

	sendSuccess(w, http.StatusOK, entity)
}

func (s *Server) updateEntity(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	entity, err := s.entitySvc.UpdateEntity(orgID, entityID, updates)
	if err != nil {
		if err.Error() == "entity not found" {
			sendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		}
		return
	}

	sendSuccess(w, http.StatusOK, entity)
}

func (s *Server) deleteEntity(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		sendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

	err := s.entitySvc.DeleteEntity(orgID, entityID)
	if err != nil {
		if err.Error() == "entity not found" {
			sendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		}
		return
	}

	sendSuccess(w, http.StatusOK, map[string]string{"message": "Entity deleted successfully"})
}

// Helper functions for REST API v1

func sendSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := shared.Response{
		Success: true,
		Data:    data,
	}

	json.NewEncoder(w).Encode(response)
}

func sendError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := shared.Response{
		Success: false,
		Error: &shared.Error{
			Code:    code,
			Message: message,
		},
	}

	json.NewEncoder(w).Encode(response)
}
