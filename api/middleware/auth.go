package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
)

// GetAllowedOrigins returns the list of allowed origins from environment
// Returns empty slice if ALLOWED_ORIGINS is "*" or empty (allow all)
func GetAllowedOrigins() []string {
	origins := os.Getenv("ALLOWED_ORIGINS")
	if origins == "" || origins == "*" {
		return []string{} // Empty = allow all in NATS WebSocket
	}
	var result []string
	for _, o := range strings.Split(origins, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// IsOriginAllowed checks if an origin is in the allowed list
func IsOriginAllowed(origin string) bool {
	origins := os.Getenv("ALLOWED_ORIGINS")
	if origins == "" || origins == "*" {
		return true
	}
	for _, allowed := range strings.Split(origins, ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}

// BearerAuth validates the Bearer token in the Authorization header
func BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing Authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid Authorization header format")
			return
		}

		token := parts[1]
		expectedToken := os.Getenv("API_BEARER_TOKEN")
		if expectedToken == "" {
			expectedToken = "constellation-dev-token" // Default for dev
		}

		if token != expectedToken {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}
