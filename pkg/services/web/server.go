package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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
	s.mux.HandleFunc("/entities", s.handleEntitiesPage)
	s.mux.HandleFunc("/entities/new", s.handleEntityForm)
	s.mux.HandleFunc("/entities/edit", s.handleEntityForm)
	s.mux.HandleFunc("/organizations/new", s.handleOrganizationForm)
	s.mux.HandleFunc("/streams", s.handleStreamsPage)
	s.mux.HandleFunc("/overwatch", s.handleOverwatchPage)

	// Web API endpoints (for Datastar/SSE)
	s.mux.HandleFunc("/api/organizations", s.handleAPIOrganizations)
	s.mux.HandleFunc("/api/entities", s.handleAPIEntities)
	s.mux.HandleFunc("/api/entities/", s.handleAPIEntity) // For specific entity operations
	s.mux.HandleFunc("/api/overwatch/kv", s.handleAPIOverwatchKV)

	// SSE endpoint for streams
	s.mux.HandleFunc("/api/streams/sse", s.handleStreamSSE)

	// REST API v1 endpoints
	s.mux.HandleFunc("/api/v1/health", s.handleHealthCheck)
	s.mux.HandleFunc("/api/v1/organizations", s.handleAPIV1Organizations)
	s.mux.HandleFunc("/api/v1/entities", s.handleAPIV1Entities)
}

func (s *Server) Start(port string) error {
	log.Printf("Starting Constellation Overwatch Edge Awareness Plane on port %s", port)
	return http.ListenAndServe(":"+port, s.mux)
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

		// Append the new organization row to the table
		log.Printf("[API] Rendering organization row component")
		component := templates.OrganizationRow(*org, org.OrgID)

		log.Printf("[API] Patching component with selector '#org-table tbody', mode: append")
		if err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#org-table tbody"),
			datastar.WithMode(datastar.ElementPatchModeAppend)); err != nil {
			log.Printf("[API] ERROR appending org row: %v", err)
			return
		}

		log.Printf("[API] ✓ SSE patch sent successfully - new row appended to table")
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
