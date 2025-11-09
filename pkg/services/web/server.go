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
	"constellation-overwatch/pkg/services/web/templates"
	"constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

type Server struct {
	db        *db.Service
	nc        *nats.Conn
	natsEmbedded *embeddednats.EmbeddedNATS
	orgSvc    *services.OrganizationService
	entitySvc *services.EntityService
	mux       *http.ServeMux
}

func NewServer(dbService *db.Service, nc *nats.Conn, natsEmbedded *embeddednats.EmbeddedNATS) (*Server, error) {
	s := &Server{
		db:           dbService,
		nc:           nc,
		natsEmbedded: natsEmbedded,
		orgSvc:       services.NewOrganizationService(dbService.GetDB()),
		entitySvc:    services.NewEntityService(dbService.GetDB(), natsEmbedded),
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
	s.mux.HandleFunc("/streams", s.handleStreamsPage)

	// Web API endpoints (for Datastar/SSE)
	s.mux.HandleFunc("/api/organizations", s.handleAPIOrganizations)
	s.mux.HandleFunc("/api/entities", s.handleAPIEntities)
	s.mux.HandleFunc("/api/entities/", s.handleAPIEntity) // For specific entity operations

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

	var entities []ontology.Entity
	if orgID != "" {
		entities, err = s.entitySvc.ListEntities(orgID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		fmt.Fprintf(w, "event: datastar-patch-elements\n")
		fmt.Fprintf(w, "data: mode outer\n")
		fmt.Fprintf(w, "data: selector body\n")
		fmt.Fprintf(w, "data: elements ")
		component := templates.EntitiesPage(orgs, orgID, entities)
		component.Render(r.Context(), w)
		fmt.Fprintf(w, "\n\n")
		w.(http.Flusher).Flush()
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
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		fmt.Fprintf(w, "event: datastar-patch-elements\n")
		fmt.Fprintf(w, "data: mode inner\n")
		fmt.Fprintf(w, "data: selector #entity-form-modal\n")
		fmt.Fprintf(w, "data: elements ")
		component := templates.EntityForm(orgID, entity, isEdit)
		component.Render(r.Context(), w)
		fmt.Fprintf(w, "\n\n")
		w.(http.Flusher).Flush()
		return
	}

	component := templates.EntityForm(orgID, entity, isEdit)
	component.Render(r.Context(), w)
}

func (s *Server) handleStreamsPage(w http.ResponseWriter, r *http.Request) {
	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		fmt.Fprintf(w, "event: datastar-patch-elements\n")
		fmt.Fprintf(w, "data: mode outer\n")
		fmt.Fprintf(w, "data: selector body\n")
		fmt.Fprintf(w, "data: elements ")
		component := templates.StreamsPage()
		component.Render(r.Context(), w)
		fmt.Fprintf(w, "\n\n")
		w.(http.Flusher).Flush()
		return
	}

	component := templates.StreamsPage()
	component.Render(r.Context(), w)
}

// API handlers

func (s *Server) handleAPIOrganizations(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		orgs, err := s.orgSvc.ListOrganizations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode inner\n")
			fmt.Fprintf(w, "data: selector #org-table\n")
			fmt.Fprintf(w, "data: elements ")
			component := templates.OrganizationsTable(orgs, "")
			component.Render(r.Context(), w)
			fmt.Fprintf(w, "\n\n")
			w.(http.Flusher).Flush()
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organizations": orgs,
		})
	}
}

func (s *Server) handleAPIEntities(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		http.Error(w, "org_id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		entities, err := s.entitySvc.ListEntities(orgID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode inner\n")
			fmt.Fprintf(w, "data: selector #entity-table\n")
			fmt.Fprintf(w, "data: elements ")
			component := templates.EntitiesTable(orgID, entities)
			component.Render(r.Context(), w)
			fmt.Fprintf(w, "\n\n")
			w.(http.Flusher).Flush()
			return
		}

		// Otherwise return JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entities": entities,
		})

	case "POST":
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode append\n")
			fmt.Fprintf(w, "data: selector #entity-table tbody\n")
			fmt.Fprintf(w, "data: elements ")
			component := templates.EntityRow(orgID, *entity)
			component.Render(r.Context(), w)
			fmt.Fprintf(w, "\n\n")
			w.(http.Flusher).Flush()
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode outer\n")
			fmt.Fprintf(w, "data: selector #entity-%s\n", entityID)
			fmt.Fprintf(w, "data: elements ")
			component := templates.EntityRow(orgID, *entity)
			component.Render(r.Context(), w)
			fmt.Fprintf(w, "\n\n")
			w.(http.Flusher).Flush()
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode remove\n")
			fmt.Fprintf(w, "data: selector #entity-%s\n", entityID)
			fmt.Fprintf(w, "data: elements <div></div>\n\n")
			w.(http.Flusher).Flush()
			return
		}

		// Otherwise return success JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// SSE handler for real-time streams
func (s *Server) handleStreamSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel to signal when to stop
	done := r.Context().Done()

	// Subscribe to all NATS messages
	subject := "constellation.>"

	sub, err := s.nc.Subscribe(subject, func(m *nats.Msg) {
		// Parse the message
		var msg map[string]interface{}
		if err := json.Unmarshal(m.Data, &msg); err == nil {
			// Create stream message component
			timestamp := time.Now().Format("15:04:05")
			jsonData, _ := json.MarshalIndent(msg, "", "  ")

			component := templates.StreamMessage(m.Subject, timestamp, string(jsonData))

			// Write Datastar SSE event
			fmt.Fprintf(w, "event: datastar-patch-elements\n")
			fmt.Fprintf(w, "data: mode append\n")
			fmt.Fprintf(w, "data: selector #stream-messages\n")
			fmt.Fprintf(w, "data: elements ")
			component.Render(r.Context(), w)
			fmt.Fprintf(w, "\n\n")
			w.(http.Flusher).Flush()
		}
	})

	if err != nil {
		log.Printf("Failed to subscribe to NATS: %v", err)
		return
	}
	defer sub.Unsubscribe()

	// Send initial connection message
	fmt.Fprintf(w, "event: datastar-patch-elements\n")
	fmt.Fprintf(w, "data: mode inner\n")
	fmt.Fprintf(w, "data: selector #stream-messages\n")
	fmt.Fprintf(w, "data: elements <div class=\"stream-message\"><div class=\"msg-header\"><span class=\"msg-subject\">system</span><span class=\"msg-time\">%s</span></div><div class=\"msg-body\"><div class=\"msg-data\"><pre>Connected to stream</pre></div></div></div>\n\n", time.Now().Format("15:04:05"))
	w.(http.Flusher).Flush()

	// Keep connection open until client disconnects
	<-done
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
