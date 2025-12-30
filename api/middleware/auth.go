package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
)

const (
	SessionCookieName = "overwatch_session"
	sessionDuration   = 24 * time.Hour
)

type session struct {
	token     string
	expiresAt time.Time
}

// SessionAuth handles session-based authentication for the web UI
type SessionAuth struct {
	password string
	sessions map[string]*session
	mu       sync.RWMutex
}

// NewSessionAuth creates a new session auth handler
func NewSessionAuth() *SessionAuth {
	return &SessionAuth{
		password: os.Getenv("WEB_UI_PASSWORD"),
		sessions: make(map[string]*session),
	}
}

// IsEnabled returns true if password auth is configured
func (s *SessionAuth) IsEnabled() bool {
	return s.password != ""
}

// ValidatePassword checks if the provided password is correct
func (s *SessionAuth) ValidatePassword(password string) bool {
	return subtle.ConstantTimeCompare([]byte(s.password), []byte(password)) == 1
}

// CreateSession creates a new session and returns the session token
func (s *SessionAuth) CreateSession() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	tokenStr := hex.EncodeToString(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[tokenStr] = &session{
		token:     tokenStr,
		expiresAt: time.Now().Add(sessionDuration),
	}

	// Cleanup expired sessions
	s.cleanupExpiredSessions()

	return tokenStr, nil
}

// ValidateSession checks if the session token is valid
func (s *SessionAuth) ValidateSession(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	return time.Now().Before(sess.expiresAt)
}

// DestroySession removes a session
func (s *SessionAuth) DestroySession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// cleanupExpiredSessions removes expired sessions (must be called with lock held)
func (s *SessionAuth) cleanupExpiredSessions() {
	now := time.Now()
	for token, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, token)
		}
	}
}

// RequireSession is middleware that checks for a valid session cookie
func (s *SessionAuth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is not enabled, pass through
		if !s.IsEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		// Check for session cookie
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || !s.ValidateSession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SetSessionCookie sets the session cookie on the response
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie clears the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

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
		expectedToken := os.Getenv("OVERWATCH_TOKEN")
		if expectedToken == "" {
			expectedToken = "reindustrialize-dev-token" // Default for dev
		}

		if token != expectedToken {
			responses.SendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}
