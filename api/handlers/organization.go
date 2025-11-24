package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
)

type OrganizationHandler struct {
	service *services.OrganizationService
}

func NewOrganizationHandler(service *services.OrganizationService) *OrganizationHandler {
	return &OrganizationHandler{
		service: service,
	}
}

func (h *OrganizationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req ontology.CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.SendError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	org, err := h.service.CreateOrganization(&req)
	if err != nil {
		responses.SendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	responses.SendSuccess(w, http.StatusCreated, org)
}

func (h *OrganizationHandler) List(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.service.ListOrganizations()
	if err != nil {
		responses.SendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	responses.SendSuccess(w, http.StatusOK, orgs)
}

func (h *OrganizationHandler) Get(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	org, err := h.service.GetOrganization(orgID)
	if err != nil {
		if err.Error() == "organization not found" {
			responses.SendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			responses.SendError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		}
		return
	}

	responses.SendSuccess(w, http.StatusOK, org)
}

func (h *OrganizationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		responses.SendError(w, http.StatusBadRequest, "MISSING_ORG_ID", "org_id is required")
		return
	}

	err := h.service.DeleteOrganization(orgID)
	if err != nil {
		if err.Error() == "organization not found" {
			responses.SendError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			responses.SendError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		}
		return
	}

	responses.SendSuccess(w, http.StatusOK, map[string]string{"message": "Organization deleted successfully"})
}
