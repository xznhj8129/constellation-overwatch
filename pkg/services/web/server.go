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

	// Create SSE generator
	sse := datastar.NewServerSentEventGenerator(w, r)

	log.Printf("[Overwatch] Client connected for KV watching from %s", r.RemoteAddr)

	// Send initial state - get all current entries
	entries, err := s.natsEmbedded.GetAllKVEntries()
	if err != nil {
		log.Printf("[Overwatch] Error fetching initial KV entries: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse entries into entity states organized by org
	entityStatesByOrg := s.parseKVEntriesToEntityStates(entries)

	// Send initial state
	if err := s.sendEntityStatesUpdate(sse, entityStatesByOrg); err != nil {
		log.Printf("[Overwatch] Error sending initial state: %v", err)
		return
	}

	// Start watching for changes in a goroutine
	ctx := r.Context()
	go func() {
		watchErr := s.natsEmbedded.WatchKV(ctx, func(key string, entry nats.KeyValueEntry) error {
			// Parse the updated entry
			updatedStates := s.parseKVEntriesToEntityStates([]nats.KeyValueEntry{entry})

			// Send the update
			return s.sendEntityStatesUpdate(sse, updatedStates)
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

// parseKVEntriesToEntityStates parses KV entries into EntityState objects organized by org
func (s *Server) parseKVEntriesToEntityStates(entries []nats.KeyValueEntry) map[string][]shared.EntityState {
	entityStatesByOrg := make(map[string][]shared.EntityState)

	for _, entry := range entries {
		// Try to parse as EntityState
		var entityState shared.EntityState
		if err := json.Unmarshal(entry.Value(), &entityState); err != nil {
			log.Printf("[Overwatch] Failed to parse KV entry %s as EntityState: %v", entry.Key(), err)
			continue
		}

		// Group by org_id
		orgID := entityState.OrgID
		if orgID == "" {
			orgID = "unknown"
		}

		entityStatesByOrg[orgID] = append(entityStatesByOrg[orgID], entityState)
	}

	return entityStatesByOrg
}

// sendEntityStatesUpdate sends an SSE update with entity states
func (s *Server) sendEntityStatesUpdate(sse *datastar.ServerSentEventGenerator, entityStatesByOrg map[string][]shared.EntityState) error {
	// Create a map of signals to update
	signals := make(map[string]interface{})

	// Add entity states by org
	signals["entityStatesByOrg"] = entityStatesByOrg
	signals["lastUpdate"] = time.Now().Format("15:04:05")
	signals["_isConnected"] = true // Indicate SSE connection is active

	// Send the signal update
	return sse.PatchSignals(signals)
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
