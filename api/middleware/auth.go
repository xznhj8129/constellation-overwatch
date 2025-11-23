package middleware

import (
	"net/http"
	"os"
	"strings"

	"constellation-overwatch/api/responses"
)

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
