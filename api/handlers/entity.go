package handlers

import (
	"encoding/json"
	"net/http"

	"constellation-overwatch/api/responses"
	"constellation-overwatch/api/services"
	"constellation-overwatch/pkg/ontology"
)

type EntityHandler struct {
	service *services.EntityService
}

func NewEntityHandler(service *services.EntityService) *EntityHandler {
	return &EntityHandler{
		service: service,
	}
}

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

func (h *EntityHandler) List(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	entities, err := h.service.ListEntities(orgID)
	if err != nil {
		responses.SendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	responses.SendSuccess(w, http.StatusOK, entities)
}

func (h *EntityHandler) Get(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	entityID := r.URL.Query().Get("entity_id")

	if orgID == "" || entityID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_PARAMS", "org_id and entity_id are required")
		return
	}

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
}

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
