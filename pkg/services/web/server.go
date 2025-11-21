package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"constellation-overwatch/api/middleware"
	"constellation-overwatch/api/services"
	"constellation-overwatch/db"
	"constellation-overwatch/pkg/ontology"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
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
	s.mux.HandleFunc("/api/organizations/", s.handleAPIOrganization)              // For specific organization operations
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

func (s *Server) Start(bindAddr string) error {
	log.Printf("Starting Constellation Overwatch Edge Awareness Plane on %s", bindAddr)
	return http.ListenAndServe(bindAddr, s.mux)
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
			log.Printf("Error patching entities page: %v", err)
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
			log.Printf("Error patching entity form: %v", err)
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
			log.Printf("Error patching streams page: %v", err)
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
			log.Printf("Error patching organization form: %v", err)
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
			log.Printf("Error patching overwatch page: %v", err)
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
				log.Printf("Error patching organizations: %v", err)
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
		log.Printf("[API] POST /api/organizations - Content-Type: %s, Accept: %s",
			r.Header.Get("Content-Type"), r.Header.Get("Accept"))

		// Parse form data
		if err := r.ParseForm(); err != nil {
			log.Printf("[API] Error parsing form: %v", err)
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		// Create organization request
		req := &ontology.CreateOrganizationRequest{
			Name:        r.FormValue("name"),
			OrgType:     r.FormValue("org_type"),
			Description: r.FormValue("description"),
		}

		log.Printf("[API] Creating organization: name=%s, type=%s", req.Name, req.OrgType)

		// Create the organization
		org, err := s.orgSvc.CreateOrganization(req)
		if err != nil {
			log.Printf("[API] Error creating organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[API] Organization created: %s (ID: %s)", org.Name, org.OrgID)

		// Always send SSE response (Datastar forms always expect SSE)
		log.Printf("[API] Creating SSE connection for response")
		sse := datastar.NewServerSentEventGenerator(w, r)
		log.Printf("[API] SSE generator created successfully")

		// Insert the new organization row before the form row
		log.Printf("[API] Rendering organization row component")
		component := templates.OrganizationRow(*org, org.OrgID)

		log.Printf("[API] Patching component with selector '#new-org-form-row', mode: before")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#new-org-form-row"),
			datastar.WithMode(datastar.ElementPatchModeBefore)); err != nil {
			log.Printf("[API] ERROR inserting org row: %v", err)
			return
		}

		// Reset the form after successful submission
		log.Printf("[API] Resetting form via ExecuteScript")
		if err := sse.ExecuteScript("document.getElementById('new-org-form').reset()"); err != nil {
			log.Printf("[API] ERROR resetting form: %v", err)
		}

		log.Printf("[API] ✓ SSE patch sent successfully - new row appended and form reset")
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
		log.Printf("[EDIT] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Printf("[EDIT] Returning edit row for organization: %s", org.Name)

	// Return the edit row component via SSE using Replace mode to force re-initialization of event listeners
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationEditRow(*org)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		log.Printf("[EDIT] Error patching edit row: %v", err)
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
		log.Printf("[CANCEL] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Printf("[CANCEL] Returning normal row for organization: %s", org.Name)

	// Return the normal row component via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		log.Printf("[CANCEL] Error patching normal row: %v", err)
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
			log.Printf("Error patching fleet page: %v", err)
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
		log.Printf("[FLEET-EDIT] Error fetching organizations: %v", err)
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
		log.Printf("[FLEET-EDIT] Entity %s not found", entityID)
		http.Error(w, "Entity not found", http.StatusNotFound)
		return
	}

	log.Printf("[FLEET-EDIT] Returning edit row for entity: %s", entity.EntityID)

	// Return the edit row component via SSE using Replace mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetEditRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		log.Printf("[FLEET-EDIT] Error patching edit row: %v", err)
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
		log.Printf("[FLEET-CANCEL] Error fetching organizations: %v", err)
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
		log.Printf("[FLEET-CANCEL] Entity %s not found", entityID)
		http.Error(w, "Entity not found", http.StatusNotFound)
		return
	}

	log.Printf("[FLEET-CANCEL] Returning normal row for entity: %s", entity.EntityID)

	// Return the normal row component via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		log.Printf("[FLEET-CANCEL] Error patching normal row: %v", err)
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

		log.Printf("[API] PUT /api/organizations/update (form data, org_id=%s)", orgID)
	} else {
		// Read JSON body (Datastar sends signals as JSON)
		signals := make(map[string]interface{})
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&signals); err != nil {
			log.Printf("[API] Error reading JSON: %v", err)
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

		log.Printf("[API] PUT /api/organizations/update (JSON, org_id=%s)", orgID)
	}

	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	log.Printf("[API] Updating organization with: name=%v, org_type=%v, description=%v",
		updates["name"], updates["org_type"], updates["description"])

	// Update the organization
	if err := s.orgSvc.UpdateOrganization(orgID, updates); err != nil {
		log.Printf("[API] Error updating organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the updated organization
	org, err := s.orgSvc.GetOrganization(orgID)
	if err != nil {
		log.Printf("[API] Error fetching updated organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

	// Return the updated row via SSE using Morph mode for intelligent DOM diffing
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		log.Printf("[API] Error patching updated row: %v", err)
		return
	}

	log.Printf("[API] ✓ Organization row updated via SSE with Morph mode")
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
		log.Printf("[API] PUT /api/organizations/%s", orgID)

		// Parse form data
		if err := r.ParseForm(); err != nil {
			log.Printf("[API] Error parsing form: %v", err)
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

		log.Printf("[API] Updating organization with: name=%s, org_type=%s, description=%s",
			updates["name"], updates["org_type"], updates["description"])

		// Update the organization
		if err := s.orgSvc.UpdateOrganization(orgID, updates); err != nil {
			log.Printf("[API] Error updating organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch the updated organization
		org, err := s.orgSvc.GetOrganization(orgID)
		if err != nil {
			log.Printf("[API] Error fetching updated organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

		// Return the updated row via SSE using Morph mode for intelligent DOM diffing
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.OrganizationRow(*org, "")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#org-row-"+orgID),
			datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
			log.Printf("[API] Error patching updated row: %v", err)
			return
		}

		log.Printf("[API] ✓ Organization row updated via SSE with Morph mode")

	case "DELETE":
		log.Printf("[API] DELETE /api/organizations/%s", orgID)
		log.Printf("[API] Accept header: %s", r.Header.Get("Accept"))

		// Delete the organization
		if err := s.orgSvc.DeleteOrganization(orgID); err != nil {
			log.Printf("[API] Error deleting organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[API] Organization deleted: %s", orgID)

		// If Datastar, remove the row from the UI
		acceptHeader := r.Header.Get("Accept")
		if acceptHeader != "" && (acceptHeader == "text/event-stream" || strings.Contains(acceptHeader, "text/event-stream")) {
			log.Printf("[API] Sending SSE response to remove row")
			sse := datastar.NewServerSentEventGenerator(w, r)
			err := sse.PatchElements("",
				datastar.WithSelector("#org-row-"+orgID),
				datastar.WithMode(datastar.ElementPatchModeRemove))
			if err != nil {
				log.Printf("[API] Error removing organization row: %v", err)
			} else {
				log.Printf("[API] ✓ SSE response sent successfully")
			}
			return
		}

		log.Printf("[API] No SSE request detected, returning JSON")
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
				log.Printf("Error patching entities: %v", err)
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
				log.Printf("Error patching new entity: %v", err)
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
				log.Printf("Error patching updated entity: %v", err)
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
				log.Printf("Error removing entity: %v", err)
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
		log.Printf("[FLEET-API] Error reading JSON: %v", err)
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

	log.Printf("[FLEET-API] PUT /api/fleet/update (entity_id=%s, org_id=%s)", entityID, orgID)
	log.Printf("[FLEET-API] Updating entity with: %v", updates)

	// Update the entity
	entity, err := s.entitySvc.UpdateEntity(orgID, entityID, updates)
	if err != nil {
		log.Printf("[FLEET-API] Error updating entity: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[FLEET-API] Entity updated: %s (ID: %s)", entity.EntityType, entity.EntityID)

	// Fetch all organizations for the dropdown in the returned row
	orgs, err := s.orgSvc.ListOrganizations()
	if err != nil {
		log.Printf("[FLEET-API] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the updated row via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := templates.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		log.Printf("[FLEET-API] Error patching updated row: %v", err)
		return
	}

	log.Printf("[FLEET-API] ✓ Fleet entity row updated via SSE with Morph mode")
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
				log.Printf("Error patching fleet: %v", err)
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
		log.Printf("[FLEET-API] POST /api/fleet - Content-Type: %s, Accept: %s",
			r.Header.Get("Content-Type"), r.Header.Get("Accept"))

		// Parse form data
		if err := r.ParseForm(); err != nil {
			log.Printf("[FLEET-API] Error parsing form: %v", err)
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

		log.Printf("[FLEET-API] Creating fleet entity: type=%s, org_id=%s", req.EntityType, orgID)

		// Create the entity
		entity, err := s.entitySvc.CreateEntity(orgID, req)
		if err != nil {
			log.Printf("[FLEET-API] Error creating entity: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[FLEET-API] Entity created: %s (ID: %s)", entity.EntityType, entity.EntityID)

		// Fetch all organizations for rendering the row
		orgs, err := s.orgSvc.ListOrganizations()
		if err != nil {
			log.Printf("[FLEET-API] Error fetching organizations: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Always send SSE response (Datastar forms always expect SSE)
		log.Printf("[FLEET-API] Creating SSE connection for response")
		sse := datastar.NewServerSentEventGenerator(w, r)

		// Insert the new fleet row before the form row
		log.Printf("[FLEET-API] Rendering fleet row component")
		component := templates.FleetRow(orgs, *entity)

		log.Printf("[FLEET-API] Patching component with selector '#new-fleet-form-row', mode: before")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#new-fleet-form-row"),
			datastar.WithMode(datastar.ElementPatchModeBefore)); err != nil {
			log.Printf("[FLEET-API] ERROR inserting fleet row: %v", err)
			return
		}

		// Reset the form after successful submission
		log.Printf("[FLEET-API] Resetting form via ExecuteScript")
		if err := sse.ExecuteScript("document.getElementById('new-fleet-form').reset()"); err != nil {
			log.Printf("[FLEET-API] ERROR resetting form: %v", err)
		}

		log.Printf("[FLEET-API] ✓ SSE patch sent successfully - new row appended and form reset")
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
		log.Printf("[FLEET-API] DELETE /api/fleet/%s?org_id=%s", entityID, orgID)
		log.Printf("[FLEET-API] Accept header: %s", r.Header.Get("Accept"))

		// If org_id not provided, try to find it
		if orgID == "" {
			orgs, err := s.orgSvc.ListOrganizations()
			if err != nil {
				log.Printf("[FLEET-API] Error fetching organizations: %v", err)
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
			log.Printf("[FLEET-API] Error deleting entity: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[FLEET-API] Entity deleted: %s", entityID)

		// If Datastar, remove the row from the UI
		acceptHeader := r.Header.Get("Accept")
		if acceptHeader != "" && (acceptHeader == "text/event-stream" || strings.Contains(acceptHeader, "text/event-stream")) {
			log.Printf("[FLEET-API] Sending SSE response to remove row")
			sse := datastar.NewServerSentEventGenerator(w, r)
			err := sse.PatchElements("",
				datastar.WithSelector("#fleet-row-"+entityID),
				datastar.WithMode(datastar.ElementPatchModeRemove))
			if err != nil {
				log.Printf("[FLEET-API] Error removing fleet row: %v", err)
			} else {
				log.Printf("[FLEET-API] ✓ SSE response sent successfully")
			}
			return
		}

		log.Printf("[FLEET-API] No SSE request detected, returning JSON")
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
		log.Printf("Error fetching KV keys: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch all entries
	var kvEntries []templates.KVEntry
	for _, key := range keys {
		entry, err := kv.Get(key)
		if err != nil {
			log.Printf("Error getting key %s: %v", key, err)
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
			log.Printf("Error patching KV content: %v", err)
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
		log.Printf("[Overwatch] ERROR: ResponseWriter does not support flushing (SSE won't work)")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Create SSE generator (this sets SSE headers automatically)
	sse := datastar.NewServerSentEventGenerator(w, r)

	log.Printf("[Overwatch] ✓ SSE connection established from %s", r.RemoteAddr)

	// Send initial state - get all current entries
	entries, err := s.natsEmbedded.GetAllKVEntries()
	if err != nil {
		log.Printf("[Overwatch] Error fetching initial KV entries: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse entries into entity states organized by org
	entityStatesByOrg := s.parseKVEntriesToEntityStates(entries)

	log.Printf("[Overwatch] Sending initial state with %d entities...", len(entityStatesByOrg))

	// Send initial state
	if err := s.sendEntityStatesUpdate(sse, entityStatesByOrg); err != nil {
		log.Printf("[Overwatch] Error sending initial state: %v", err)
		return
	}

	// CRITICAL: Flush the response to ensure SSE events reach the browser
	flusher.Flush()
	log.Printf("[Overwatch] ✓ Initial state flushed to client")

	// Start watching for changes in a goroutine
	ctx := r.Context()
	go func() {
		watchErr := s.natsEmbedded.WatchKV(ctx, func(key string, entry nats.KeyValueEntry) error {
			log.Printf("[Overwatch KV Watch] ⚡ Change detected for key: %s, operation: %s", key, entry.Operation())

			// On ANY change, refetch the complete global state from NATS KV
			entries, err := s.natsEmbedded.GetAllKVEntries()
			if err != nil {
				log.Printf("[Overwatch KV Watch] Error refetching global state: %v", err)
				return err
			}

			// Parse complete global state
			entityStatesByOrg := s.parseKVEntriesToEntityStates(entries)

			// Send complete global state to client
			log.Printf("[Overwatch KV Watch] Sending update with %d entities", len(entityStatesByOrg))
			if err := s.sendEntityStatesUpdate(sse, entityStatesByOrg); err != nil {
				log.Printf("[Overwatch KV Watch] ERROR sending update: %v", err)
				return err
			}

			// CRITICAL: Flush after each update to ensure browser receives it
			flusher.Flush()
			log.Printf("[Overwatch KV Watch] ✓ Update flushed to client")

			return nil
		})

		if watchErr != nil && watchErr != context.Canceled {
			log.Printf("[Overwatch] Error watching KV: %v", watchErr)
		}
	}()

	// Keep the connection alive with heartbeats (CRITICAL!)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Overwatch] Client disconnected from %s", r.RemoteAddr)
			return
		case <-ticker.C:
			// Send heartbeat to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
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

	log.Printf("[Overwatch] Aggregating %d entities from %d KV entries", len(entitiesByID), len(entries))

	for entityID, dataMap := range entitiesByID {
		log.Printf("[Overwatch] Processing entity %s with %d KV entries", entityID, len(dataMap))
		entityState := s.mergeEntityData(entityID, dataMap)

		// Group by org_id
		orgID := entityState.OrgID
		if orgID == "" {
			orgID = "unknown"
		}

		entityStatesByOrg[orgID] = append(entityStatesByOrg[orgID], entityState)
	}

	log.Printf("[Overwatch] Built %d entities across %d orgs", len(entitiesByID), len(entityStatesByOrg))
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
			log.Printf("[Overwatch] Skipping empty data for key %s", key)
			continue
		}

		// Log raw data preview for debugging
		preview := string(data)
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		log.Printf("[Overwatch] Key '%s' data preview: %s", key, preview)

		var rawData map[string]interface{}
		if err := json.Unmarshal(data, &rawData); err != nil {
			log.Printf("[Overwatch] Failed to unmarshal key %s: %v", key, err)
			continue
		}

		// Extract org_id (check both org_id and organization_id)
		if orgID, ok := rawData["org_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
			log.Printf("[Overwatch] Found org_id='%s' in key '%s'", orgID, key)
		}
		if orgID, ok := rawData["organization_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
			log.Printf("[Overwatch] Found organization_id='%s' in key '%s'", orgID, key)
		}
		// Log if neither was found
		if state.OrgID == "" {
			log.Printf("[Overwatch] WARNING: No org_id or organization_id found in key '%s'", key)
		}

		// Extract device_id if present
		if deviceID, ok := rawData["device_id"].(string); ok && deviceID != "" {
			state.DeviceID = deviceID
		}

		// Debug: Check what top-level fields exist
		hasAnalytics := rawData["analytics"] != nil
		hasDetections := rawData["detections"] != nil
		hasThreatIntel := rawData["threat_intelligence"] != nil || rawData["threat_intel"] != nil
		log.Printf("[Overwatch] Key '%s' has: analytics=%v, detections=%v, threat_intel=%v",
			key, hasAnalytics, hasDetections, hasThreatIntel)

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
					TrackID:   trackID,
					IsActive:  false,
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
		log.Printf("[Overwatch] mergeFullState: Found org_id='%s'", orgID)
	}
	if orgID, ok := data["organization_id"].(string); ok && orgID != "" {
		state.OrgID = orgID
		log.Printf("[Overwatch] mergeFullState: Found organization_id='%s'", orgID)
	}
	if state.OrgID == "" {
		log.Printf("[Overwatch] mergeFullState: WARNING - No org_id or organization_id in data")
	}

	// Python detection service format (NEW): detections.tracked_objects
	if detectionsData, ok := data["detections"].(map[string]interface{}); ok {
		// Check for tracked_objects (new format)
		if trackedObjects, ok := detectionsData["tracked_objects"].(map[string]interface{}); ok {
			s.mergeDetections(state, map[string]interface{}{"tracked_objects": trackedObjects})
			log.Printf("[Overwatch] Merged detections.tracked_objects with %d tracked objects", len(trackedObjects))
		} else if objectsData, ok := detectionsData["objects"].(map[string]interface{}); ok {
			// Fallback to old format: detections.objects
			s.mergeDetections(state, map[string]interface{}{"tracked_objects": objectsData})
			log.Printf("[Overwatch] Merged detections.objects with %d tracked objects", len(objectsData))
		}

		// Check for analytics nested inside detections (new format)
		if analyticsData, ok := detectionsData["analytics"].(map[string]interface{}); ok {
			s.mergeAnalytics(state, analyticsData)
			log.Printf("[Overwatch] Merged detections.analytics")
		}
	}

	// Python analytics format (OLD): top-level analytics.summary
	if analyticsData, ok := data["analytics"].(map[string]interface{}); ok {
		if summaryData, ok := analyticsData["summary"].(map[string]interface{}); ok {
			s.mergeAnalytics(state, summaryData)
			log.Printf("[Overwatch] Merged analytics.summary")
		}
	}

	// Python threat intelligence format (NEW): top-level threat_intelligence
	if threatData, ok := data["threat_intelligence"].(map[string]interface{}); ok {
		s.mergeThreatIntel(state, threatData)
		log.Printf("[Overwatch] Merged threat_intelligence")
	}

	// Python C4ISR format (OLD): c4isr.threat_intelligence
	if c4isrData, ok := data["c4isr"].(map[string]interface{}); ok {
		if threatData, ok := c4isrData["threat_intelligence"].(map[string]interface{}); ok {
			s.mergeThreatIntel(state, threatData)
			log.Printf("[Overwatch] Merged c4isr.threat_intelligence")
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

// sendEntityStatesUpdate sends an SSE update with entity states
func (s *Server) sendEntityStatesUpdate(sse *datastar.ServerSentEventGenerator, entityStatesByOrg map[string][]shared.EntityState) error {
	// Flatten entities for rendering
	flatEntities := []shared.EntityState{}
	for orgID, entities := range entityStatesByOrg {
		for _, entity := range entities {
			// Ensure orgID is set
			if entity.OrgID == "" {
				entity.OrgID = orgID
			}
			flatEntities = append(flatEntities, entity)
		}
	}

	// Debug: Log what we're sending
	totalEntities := 0
	for orgID, entities := range entityStatesByOrg {
		totalEntities += len(entities)
		log.Printf("[Overwatch] Sending SSE update for org '%s' with %d entities", orgID, len(entities))
		for _, entity := range entities {
			log.Printf("[Overwatch]   - Entity: %s (type: %s, has_analytics: %v, has_detections: %v, has_threat_intel: %v)",
				entity.EntityID, entity.EntityType,
				entity.Analytics != nil, entity.Detections != nil, entity.ThreatIntel != nil)

			// Debug analytics data
			if entity.Analytics != nil {
				log.Printf("[Overwatch]     Analytics: tracked=%d, active=%d, frames=%d, threats=%d",
					entity.Analytics.TrackedObjectsCount,
					entity.Analytics.ActiveObjectsCount,
					entity.Analytics.TotalFramesProcessed,
					entity.Analytics.ActiveThreatCount)
			}

			// Debug detections data
			if entity.Detections != nil && entity.Detections.TrackedObjects != nil {
				log.Printf("[Overwatch]     Detections: %d tracked objects", len(entity.Detections.TrackedObjects))
				for trackID, obj := range entity.Detections.TrackedObjects {
					log.Printf("[Overwatch]       - %s: %s (active: %v, confidence: %.2f, threat: %s)",
						trackID, obj.Label, obj.IsActive, obj.AvgConfidence, obj.ThreatLevel)
				}
			}

			// Debug threat intel data
			if entity.ThreatIntel != nil && entity.ThreatIntel.ThreatSummary != nil {
				log.Printf("[Overwatch]     ThreatIntel: alert_level=%s, total_threats=%d",
					entity.ThreatIntel.ThreatSummary.AlertLevel,
					entity.ThreatIntel.ThreatSummary.TotalThreats)
			}
		}
	}
	log.Printf("[Overwatch] Total entities in SSE update: %d across %d orgs", totalEntities, len(entityStatesByOrg))

	// Render entity cards as HTML using Templ
	var htmlBuilder strings.Builder
	if len(flatEntities) == 0 {
		// Render empty state
		htmlBuilder.WriteString(`<div class="empty-state" style="color: #888; padding: 40px; text-align: center;"><p>No entity states in global store. Waiting for telemetry data...</p></div>`)
	} else {
		// Render each entity card
		for _, entity := range flatEntities {
			if err := templates.EntityCard(entity).Render(context.Background(), &htmlBuilder); err != nil {
				log.Printf("[Overwatch] Error rendering entity card: %v", err)
				continue
			}
		}
	}

	html := htmlBuilder.String()
	log.Printf("[Overwatch] Rendered HTML length: %d bytes", len(html))

	// Log first 200 chars of HTML for debugging
	if len(html) > 200 {
		log.Printf("[Overwatch] HTML preview: %s...", html[:200])
	} else {
		log.Printf("[Overwatch] Full HTML: %s", html)
	}

	// Send the HTML update via PatchElements
	log.Printf("[Overwatch] Calling PatchElements with selector=#entities-container, mode=inner")
	err := sse.PatchElements(html,
		datastar.WithSelector("#entities-container"),
		datastar.WithMode(datastar.ElementPatchModeInner))

	if err != nil {
		log.Printf("[Overwatch] ERROR in PatchElements: %v", err)
		return err
	}

	log.Printf("[Overwatch] PatchElements completed successfully")

	// Send connection status and metadata signals
	signals := map[string]interface{}{
		"_isConnected": true,
		"lastUpdate":   time.Now().Format("15:04:05"),
	}

	if err := sse.PatchSignals(signals); err != nil {
		log.Printf("[Overwatch] ERROR in PatchSignals: %v", err)
		return err
	}

	log.Printf("[Overwatch] PatchSignals sent: _isConnected=true, lastUpdate=%s", signals["lastUpdate"])
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
		log.Printf("[Overwatch Debug] Error fetching KV entries: %v", err)
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
		log.Printf("[Overwatch Debug] Error encoding JSON: %v", err)
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
			log.Printf("Error removing organization row: %v", err)
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
