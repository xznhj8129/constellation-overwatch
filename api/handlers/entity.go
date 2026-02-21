package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
)

type EntityHandler struct {
	service *services.EntityService
}

func NewEntityHandler(service *services.EntityService) *EntityHandler {
	return &EntityHandler{
		service: service,
	}
}

// Create godoc
// @Summary Create entity
// @Description Create a new entity within an organization
// @Tags Entities
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param entity body ontology.CreateEntityRequest true "Entity data"
// @Success 201 {object} ontology.Entity
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security APIKeyAuth
// @Router /entities [post]
func (h *EntityHandler) Create(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	var req ontology.CreateEntityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.SendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	entity, err := h.service.CreateEntity(orgID, &req)
	if err != nil {
		responses.SendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	responses.SendSuccess(w, http.StatusCreated, entity)
}

// ListOrGet godoc
// @Summary List or get entities
// @Description Get a list of entities for an organization, or a single entity if entity_id is provided
// @Tags Entities
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param entity_id query string false "Entity ID (optional - if provided, returns single entity)"
// @Success 200 {object} ontology.Entity "Single entity (when entity_id provided)"
// @Success 200 {array} ontology.Entity "List of entities (when entity_id not provided)"
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security APIKeyAuth
// @Router /entities [get]
func (h *EntityHandler) ListOrGet(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	// If entity_id provided, get single entity
	entityID := r.URL.Query().Get("entity_id")
	if entityID != "" {
		entity, err := h.service.GetEntity(orgID, entityID)
		if err != nil {
			if err.Error() == "entity not found" {
				responses.SendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			} else {
				responses.SendError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			}
			return
		}
		responses.SendSuccess(w, http.StatusOK, entity)
		return
	}

	// Otherwise, list all entities
	entities, err := h.service.ListEntities(orgID)
	if err != nil {
		responses.SendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	responses.SendSuccess(w, http.StatusOK, entities)
}

// Update godoc
// @Summary Update entity
// @Description Update an existing entity
// @Tags Entities
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param entity_id query string true "Entity ID"
// @Param updates body object true "Fields to update"
// @Success 200 {object} ontology.Entity
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security APIKeyAuth
// @Router /entities [put]
func (h *EntityHandler) Update(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		responses.SendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	entity, err := h.service.UpdateEntity(orgID, entityID, updates)
	if err != nil {
		if err.Error() == "entity not found" {
			responses.SendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			responses.SendError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		}
		return
	}

	responses.SendSuccess(w, http.StatusOK, entity)
}

// Delete godoc
// @Summary Delete entity
// @Description Delete an entity by ID
// @Tags Entities
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param entity_id query string true "Entity ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security APIKeyAuth
// @Router /entities [delete]
func (h *EntityHandler) Delete(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

	err := h.service.DeleteEntity(orgID, entityID)
	if err != nil {
		if err.Error() == "entity not found" {
			responses.SendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			responses.SendError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		}
		return
	}

	responses.SendSuccess(w, http.StatusOK, map[string]string{"message": "Entity deleted successfully"})
}
