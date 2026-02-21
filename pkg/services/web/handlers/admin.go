package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
)

// AdminHandler exposes administrative endpoints for managing users, invites,
// and API keys. Every method enforces an admin role check before proceeding.
type AdminHandler struct {
	userSvc      *services.UserService
	apiKeySvc    *services.APIKeyService
	inviteSvc    *services.InviteService
	natsEmbedded *embeddednats.EmbeddedNATS
}

// NewAdminHandler creates a new AdminHandler with the required service
// dependencies.
func NewAdminHandler(userSvc *services.UserService, apiKeySvc *services.APIKeyService, inviteSvc *services.InviteService, natsEmbedded *embeddednats.EmbeddedNATS) *AdminHandler {
	return &AdminHandler{
		userSvc:      userSvc,
		apiKeySvc:    apiKeySvc,
		inviteSvc:    inviteSvc,
		natsEmbedded: natsEmbedded,
	}
}

// requireAdmin checks that the authenticated user has the admin role.
// It writes a 403 JSON error and returns false when the check fails.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if middleware.UserRoleFromContext(r.Context()) != "admin" {
		responses.SendError(w, http.StatusForbidden, "FORBIDDEN", "Admin role required")
		return false
	}
	return true
}

// HandleListUsers returns a JSON list of users belonging to the session's
// organization.
func (h *AdminHandler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	users, err := h.userSvc.ListByOrg(orgID)
	if err != nil {
		logger.Errorf("Failed to list users for org %s: %v", orgID, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list users")
		return
	}

	responses.SendSuccess(w, http.StatusOK, users)
}

// createInviteRequest is the expected JSON body for HandleCreateInvite.
type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// HandleCreateInvite generates a new organization invite and returns the
// invite details together with the plaintext token (shown once).
func (h *AdminHandler) HandleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
		return
	}

	if req.Email == "" || req.Role == "" {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "email and role are required")
		return
	}

	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	invitedBy := middleware.UserIDFromContext(r.Context())

	invite, plainToken, err := h.inviteSvc.CreateInvite(orgID, req.Email, req.Role, invitedBy)
	if err != nil {
		logger.Errorf("Failed to create invite for %s: %v", req.Email, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create invite")
		return
	}

	type inviteResponse struct {
		*services.Invite
		Token string `json:"token"`
	}

	responses.SendSuccess(w, http.StatusCreated, inviteResponse{
		Invite: invite,
		Token:  plainToken,
	})
}

// HandleRevokeInvite marks an invite as revoked so it can no longer be
// accepted. The invite ID is read from the URL path.
func (h *AdminHandler) HandleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	inviteID := r.PathValue("id")
	if inviteID == "" {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "Missing invite id")
		return
	}

	if err := h.inviteSvc.RevokeInvite(inviteID); err != nil {
		logger.Errorf("Failed to revoke invite %s: %v", inviteID, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to revoke invite")
		return
	}

	responses.SendSuccess(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// createAPIKeyRequest is the expected JSON body for HandleCreateAPIKey.
type createAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// HandleCreateAPIKey generates a new API key for the authenticated user and
// returns the key details including the plaintext key (shown once). When NATS
// scopes are selected, an NKey pair is generated and registered with the
// embedded NATS server.
func (h *AdminHandler) HandleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
		return
	}

	if req.Name == "" {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
		return
	}

	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	userID := middleware.UserIDFromContext(r.Context())

	created, err := h.apiKeySvc.CreateKey(userID, orgID, req.Name, req.Scopes, nil)
	if err != nil {
		logger.Errorf("Failed to create API key %q: %v", req.Name, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create API key")
		return
	}

	// If an NKey was generated, register it with the NATS server.
	if created.NATSPubKey != "" && h.natsEmbedded != nil {
		perms := embeddednats.BuildNATSPermissions(req.Scopes, orgID)
		if perms != nil {
			if err := h.natsEmbedded.AddNKeyUser(created.NATSPubKey, perms); err != nil {
				logger.Errorf("Failed to register NKey with NATS: %v", err)
				// Key was created in DB but NATS registration failed -- log but still return the key.
			}
		}
	}

	responses.SendSuccess(w, http.StatusCreated, created)
}

// HandleRevokeAPIKey marks an API key as revoked so it can no longer
// authenticate. If the key had NATS credentials, the NKey is also removed.
func (h *AdminHandler) HandleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	keyID := r.PathValue("id")
	if keyID == "" {
		responses.SendError(w, http.StatusBadRequest, "BAD_REQUEST", "Missing key id")
		return
	}

	// Look up NATS pub key before revoking so we can remove it from NATS.
	natsPubKey, _ := h.apiKeySvc.GetNATSPubKey(keyID)

	if err := h.apiKeySvc.RevokeKey(keyID); err != nil {
		logger.Errorf("Failed to revoke API key %s: %v", keyID, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to revoke API key")
		return
	}

	// Remove NKey from NATS server if present.
	if natsPubKey != "" && h.natsEmbedded != nil {
		if err := h.natsEmbedded.RemoveNKeyUser(natsPubKey); err != nil {
			logger.Errorf("Failed to remove NKey from NATS: %v", err)
		}
	}

	responses.SendSuccess(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleListAPIKeys returns a JSON list of non-revoked API keys for the
// session's organization.
func (h *AdminHandler) HandleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	keys, err := h.apiKeySvc.ListKeys(orgID)
	if err != nil {
		logger.Errorf("Failed to list API keys for org %s: %v", orgID, err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list API keys")
		return
	}

	responses.SendSuccess(w, http.StatusOK, keys)
}
