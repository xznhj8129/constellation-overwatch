package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	fleet_components "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/fleet/components"
	org_components "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/organizations/components"
)

type DatastarHandler struct {
	orgSvc    *services.OrganizationService
	entitySvc *services.EntityService
}

func NewDatastarHandler(orgSvc *services.OrganizationService, entitySvc *services.EntityService) *DatastarHandler {
	return &DatastarHandler{
		orgSvc:    orgSvc,
		entitySvc: entitySvc,
	}
}

// Organization Handlers

func (h *DatastarHandler) HandleAPIOrganizations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		orgs, err := h.orgSvc.ListOrganizations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := org_components.OrganizationsTable(orgs, "")
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
		org, err := h.orgSvc.CreateOrganization(req)
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
		component := org_components.OrganizationRow(*org, org.OrgID)

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

func (h *DatastarHandler) HandleAPIOrganizationUpdate(w http.ResponseWriter, r *http.Request) {
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
	if err := h.orgSvc.UpdateOrganization(orgID, updates); err != nil {
		logger.Infof("[API] Error updating organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the updated organization
	org, err := h.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[API] Error fetching updated organization: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

	// Return the updated row via SSE using Morph mode for intelligent DOM diffing
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := org_components.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[API] Error patching updated row: %v", err)
		return
	}

	logger.Infow("[API] ✓ Organization row updated via SSE with Morph mode")
}

func (h *DatastarHandler) HandleAPIOrganization(w http.ResponseWriter, r *http.Request) {
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
		if err := h.orgSvc.UpdateOrganization(orgID, updates); err != nil {
			logger.Infof("[API] Error updating organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch the updated organization
		org, err := h.orgSvc.GetOrganization(orgID)
		if err != nil {
			logger.Infof("[API] Error fetching updated organization: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infof("[API] Organization updated: %s (ID: %s)", org.Name, org.OrgID)

		// Return the updated row via SSE using Morph mode for intelligent DOM diffing
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := org_components.OrganizationRow(*org, "")
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
		if err := h.orgSvc.DeleteOrganization(orgID); err != nil {
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

func (h *DatastarHandler) HandleOrganizationEdit(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from path: /organizations/edit/{orgID}
	orgID := r.URL.Path[len("/organizations/edit/"):]
	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	// Fetch the organization
	org, err := h.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[EDIT] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Infof("[EDIT] Returning edit row for organization: %s", org.Name)

	// Return the edit row component via SSE using Replace mode to force re-initialization of event listeners
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := org_components.OrganizationEditRow(*org)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		logger.Infof("[EDIT] Error patching edit row: %v", err)
	}
}

func (h *DatastarHandler) HandleOrganizationCancel(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from path: /organizations/cancel/{orgID}
	orgID := r.URL.Path[len("/organizations/cancel/"):]
	if orgID == "" {
		http.Error(w, "Organization ID required", http.StatusBadRequest)
		return
	}

	// Fetch the organization
	org, err := h.orgSvc.GetOrganization(orgID)
	if err != nil {
		logger.Infof("[CANCEL] Error fetching organization %s: %v", orgID, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Infof("[CANCEL] Returning normal row for organization: %s", org.Name)

	// Return the normal row component via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := org_components.OrganizationRow(*org, "")
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#org-row-"+orgID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[CANCEL] Error patching normal row: %v", err)
	}
}

// Entity Handlers

func (h *DatastarHandler) HandleAPIEntities(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")

	switch r.Method {
	case "GET":
		var entities []ontology.Entity
		var err error

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
			component := org_components.EntitiesTable(orgID, entities)
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
		entity, err := h.entitySvc.CreateEntity(orgID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Datastar, return SSE format with new row
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := org_components.EntityRow(orgID, *entity)
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

func (h *DatastarHandler) HandleAPIEntity(w http.ResponseWriter, r *http.Request) {
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
		entity, err := h.entitySvc.UpdateEntity(orgID, entityID, updates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If Datastar, return the updated row
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := org_components.EntityRow(orgID, *entity)
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
		err := h.entitySvc.DeleteEntity(orgID, entityID)
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

// Fleet Handlers

func (h *DatastarHandler) HandleAPIFleet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Fetch all entities
		entities, err := h.entitySvc.ListAllEntities()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch all organizations for rendering
		orgs, err := h.orgSvc.ListOrganizations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If this is a Datastar request, return SSE format
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := datastar.NewServerSentEventGenerator(w, r)
			component := fleet_components.FleetTable(orgs, entities)
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
		entity, err := h.entitySvc.CreateEntity(orgID, req)
		if err != nil {
			logger.Infow("[FLEET-API] Error creating entity: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infow("[FLEET-API] Entity created: %s (ID: %s)", entity.EntityType, entity.EntityID)

		// Fetch all organizations for rendering the row
		orgs, err := h.orgSvc.ListOrganizations()
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
		component := fleet_components.FleetRow(orgs, *entity)

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

func (h *DatastarHandler) HandleAPIFleetUpdate(w http.ResponseWriter, r *http.Request) {
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
	entity, err := h.entitySvc.UpdateEntity(orgID, entityID, updates)
	if err != nil {
		logger.Infow("[FLEET-API] Error updating entity: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infow("[FLEET-API] Entity updated: %s (ID: %s)", entity.EntityType, entity.EntityID)

	// Fetch all organizations for the dropdown in the returned row
	orgs, err := h.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infow("[FLEET-API] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the updated row via SSE using Morph mode
	sse := datastar.NewServerSentEventGenerator(w, r)
	component := fleet_components.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infow("[FLEET-API] Error patching updated row: %v", err)
		return
	}

	logger.Infow("[FLEET-API] ✓ Fleet entity row updated via SSE with Morph mode")
}

func (h *DatastarHandler) HandleAPIFleetEntity(w http.ResponseWriter, r *http.Request) {
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
			orgs, err := h.orgSvc.ListOrganizations()
			if err != nil {
				logger.Infow("[FLEET-API] Error fetching organizations: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Find the entity's org_id
			for _, org := range orgs {
				if _, err := h.entitySvc.GetEntity(org.OrgID, entityID); err == nil {
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
		if err := h.entitySvc.DeleteEntity(orgID, entityID); err != nil {
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

func (h *DatastarHandler) HandleFleetEdit(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path: /fleet/edit/{entityID}
	entityID := r.URL.Path[len("/fleet/edit/"):]
	if entityID == "" {
		http.Error(w, "Entity ID required", http.StatusBadRequest)
		return
	}

	// Fetch all organizations for the dropdown
	orgs, err := h.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infof("[FLEET-EDIT] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the entity - we need to find its org_id first
	// Try all organizations to find the entity
	var entity *ontology.Entity
	for _, org := range orgs {
		e, err := h.entitySvc.GetEntity(org.OrgID, entityID)
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
	component := fleet_components.FleetEditRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeReplace)); err != nil {
		logger.Infof("[FLEET-EDIT] Error patching edit row: %v", err)
	}
}

func (h *DatastarHandler) HandleFleetCancel(w http.ResponseWriter, r *http.Request) {
	// Extract entity ID from path: /fleet/cancel/{entityID}
	entityID := r.URL.Path[len("/fleet/cancel/"):]
	if entityID == "" {
		http.Error(w, "Entity ID required", http.StatusBadRequest)
		return
	}

	// Fetch all organizations for the dropdown
	orgs, err := h.orgSvc.ListOrganizations()
	if err != nil {
		logger.Infof("[FLEET-CANCEL] Error fetching organizations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch the entity
	var entity *ontology.Entity
	for _, org := range orgs {
		e, err := h.entitySvc.GetEntity(org.OrgID, entityID)
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
	component := fleet_components.FleetRow(orgs, *entity)
	if err := sse.PatchComponent(r.Context(), component,
		datastar.WithSelector("#fleet-row-"+entityID),
		datastar.WithMode(datastar.ElementPatchModeMorph)); err != nil {
		logger.Infof("[FLEET-CANCEL] Error patching normal row: %v", err)
	}
}
