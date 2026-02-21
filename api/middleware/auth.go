package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "overwatch_session"
	sessionDuration   = 24 * time.Hour
)

type session struct {
	token             string
	userID            string
	role              string
	orgID             string
	needsPasskeySetup bool
	expiresAt         time.Time
}

// SessionAuth handles session-based authentication for the web UI
type SessionAuth struct {
	sessions map[string]*session
	mu       sync.RWMutex
}

// NewSessionAuth creates a new session auth handler
func NewSessionAuth() *SessionAuth {
	return &SessionAuth{
		sessions: make(map[string]*session),
	}
}

// CreateSessionForUser creates a new session with user identity and returns the session token.
// Set needsPasskey to true for users that still need to register a passkey (bootstrap admin, invite flow).
// orgID is stored in the session for use by admin handlers to scope operations.
func (s *SessionAuth) CreateSessionForUser(userID, role string, needsPasskey bool, orgID string) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	tokenStr := hex.EncodeToString(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[tokenStr] = &session{
		token:             tokenStr,
		userID:            userID,
		role:              role,
		orgID:             orgID,
		needsPasskeySetup: needsPasskey,
		expiresAt:         time.Now().Add(sessionDuration),
	}

	// Cleanup expired sessions
	s.cleanupExpiredSessions()

	return tokenStr, nil
}

// ClearPasskeySetup removes the needsPasskeySetup flag from an existing session.
func (s *SessionAuth) ClearPasskeySetup(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[token]; ok {
		sess.needsPasskeySetup = false
	}
}

// IsPasskeySetup returns true if the session has the needsPasskeySetup flag set.
func (s *SessionAuth) IsPasskeySetup(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if sess, ok := s.sessions[token]; ok {
		return sess.needsPasskeySetup
	}
	return false
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

// getSession returns the session for a token, or nil if invalid/expired
func (s *SessionAuth) getSession(token string) *session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(sess.expiresAt) {
		return nil
	}
	return sess
}

// RequireSession is middleware that checks for a valid session cookie
// and injects user identity into the request context.
// If the session has needsPasskeySetup=true, non-passkey paths redirect to /setup-passkey.
func (s *SessionAuth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		sess := s.getSession(cookie.Value)
		if sess == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Enforce passkey setup redirect for users who haven't registered a passkey yet
		if sess.needsPasskeySetup {
			path := r.URL.Path
			allowed := path == "/setup-passkey" ||
				path == "/logout" ||
				strings.HasPrefix(path, "/auth/passkey/") ||
				strings.HasPrefix(path, "/static/")
			if !allowed {
				http.Redirect(w, r, "/setup-passkey", http.StatusFound)
				return
			}
		}

		// Inject user identity into request context
		ctx := r.Context()
		if sess.userID != "" {
			ctx = context.WithValue(ctx, ContextKeyUserID, sess.userID)
			ctx = context.WithValue(ctx, ContextKeyUserRole, sess.role)
		}
		if sess.orgID != "" {
			ctx = context.WithValue(ctx, ContextKeyOrgID, sess.orgID)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// secureCookies returns true when cookies should be marked Secure. This is
// determined by checking OVERWATCH_INSECURE (explicit override) and falling
// back to whether the configured base URL starts with "https://".
func secureCookies() bool {
	if v := os.Getenv("OVERWATCH_INSECURE"); v == "true" {
		return false
	}
	baseURL := os.Getenv("OVERWATCH_BASE_URL")
	if strings.HasPrefix(baseURL, "https://") {
		return true
	}
	return false
}

// SetSessionCookie sets the session cookie on the response
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(),
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
		Secure:   secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// GetAllowedOrigins returns the list of allowed origins from environment
func GetAllowedOrigins() []string {
	origins := os.Getenv("ALLOWED_ORIGINS")
	if origins == "" || origins == "*" {
		return []string{}
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
