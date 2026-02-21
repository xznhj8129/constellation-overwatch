package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
)

// APIKeyMiddleware handles API key authentication and scope enforcement.
type APIKeyMiddleware struct {
	db *sql.DB
}

// NewAPIKeyMiddleware creates a new APIKeyMiddleware with the given database connection.
func NewAPIKeyMiddleware(db *sql.DB) *APIKeyMiddleware {
	return &APIKeyMiddleware{db: db}
}

// Authenticate is HTTP middleware that validates API keys from the X-API-Key header
// or from Bearer tokens with the cow_ prefix. Keys that do not carry the cow_live_
// or cow_test_ prefix fall through to legacy OVERWATCH_TOKEN validation.
func (m *APIKeyMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := extractAPIKey(r)
		if raw == "" {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing API key or Authorization header")
			return
		}

		// If the key carries the Constellation Overwatch prefix, validate against the database.
		if strings.HasPrefix(raw, "cow_live_") || strings.HasPrefix(raw, "cow_test_") {
			m.authenticateDBKey(w, r, next, raw)
			return
		}

		// Legacy fallback: constant-time comparison against OVERWATCH_TOKEN.
		m.authenticateLegacyToken(w, r, next, raw)
	})
}

// authenticateDBKey hashes the raw key, looks it up in the api_keys table,
// and injects identity claims into the request context.
func (m *APIKeyMiddleware) authenticateDBKey(w http.ResponseWriter, r *http.Request, next http.Handler, raw string) {
	hash := hashKey(raw)

	var keyID, userID, orgID, role, scopesJSON string
	var revoked int
	var expiresAt sql.NullString

	err := m.db.QueryRow(
		`SELECT key_id, user_id, org_id, role, scopes, revoked, expires_at
		 FROM api_keys WHERE key_hash = ?`, hash,
	).Scan(&keyID, &userID, &orgID, &role, &scopesJSON, &revoked, &expiresAt)

	if err == sql.ErrNoRows {
		responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid API key")
		return
	}
	if err != nil {
		logger.Errorw("Failed to query API key", "error", err)
		responses.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Authentication failed")
		return
	}

	if revoked == 1 {
		responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "API key has been revoked")
		return
	}

	if expiresAt.Valid {
		exp, parseErr := time.Parse(time.RFC3339, expiresAt.String)
		if parseErr == nil && time.Now().After(exp) {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "API key has expired")
			return
		}
	}

	// Parse scopes from comma-separated string.
	scopes := parseScopes(scopesJSON)

	// Update last_used asynchronously.
	go func() {
		_, _ = m.db.Exec(
			`UPDATE api_keys SET last_used_at = ? WHERE key_id = ?`,
			time.Now().Format(time.RFC3339), keyID,
		)
	}()

	ctx := r.Context()
	ctx = context.WithValue(ctx, ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, ContextKeyUserRole, role)
	ctx = context.WithValue(ctx, ContextKeyOrgID, orgID)
	ctx = context.WithValue(ctx, ContextKeyAPIKey, keyID)
	ctx = context.WithValue(ctx, ContextKeyScopes, scopes)

	next.ServeHTTP(w, r.WithContext(ctx))
}

// authenticateLegacyToken performs a constant-time comparison against the
// OVERWATCH_TOKEN environment variable for backwards compatibility.
func (m *APIKeyMiddleware) authenticateLegacyToken(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
	expectedToken := os.Getenv("OVERWATCH_TOKEN")
	if expectedToken == "" {
		expectedToken = "reindustrialize-dev-token"
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
		responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid token")
		return
	}

	next.ServeHTTP(w, r)
}

// RequireScope returns middleware that ensures the authenticated API key
// possesses the given scope (or the "admin" scope, which implies all scopes).
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scopes := ScopesFromContext(r.Context())
			if !hasScope(scopes, scope) {
				responses.SendError(w, http.StatusForbidden, "FORBIDDEN", "Insufficient scope: "+scope)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractAPIKey retrieves the API key from the X-API-Key header first,
// then falls back to the Authorization: Bearer header.
func extractAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

// hashKey computes the SHA-256 hex digest of a raw API key.
func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// parseScopes splits a comma-separated scope string into a slice.
func parseScopes(s string) []string {
	if s == "" {
		return nil
	}
	var scopes []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			scopes = append(scopes, trimmed)
		}
	}
	return scopes
}

// hasScope checks whether the given scope (or "admin") is present in the list.
func hasScope(scopes []string, required string) bool {
	for _, s := range scopes {
		if s == required || s == "admin" {
			return true
		}
	}
	return false
}
