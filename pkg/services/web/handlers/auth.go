package handlers

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/templates"
)

// AuthHandler handles login and logout for the web UI
type AuthHandler struct {
	sessionAuth *middleware.SessionAuth
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(sessionAuth *middleware.SessionAuth) *AuthHandler {
	return &AuthHandler{
		sessionAuth: sessionAuth,
	}
}

// HandleLogin handles GET and POST requests for the login page
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// If auth is not enabled, redirect to home
	if !h.sessionAuth.IsEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if r.Method == http.MethodPost {
		h.handleLoginPost(w, r)
		return
	}

	// GET - show login page
	component := templates.LoginPage("")
	component.Render(r.Context(), w)
}

func (h *AuthHandler) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")

	if !h.sessionAuth.ValidatePassword(password) {
		component := templates.LoginPage("Invalid access code")
		w.WriteHeader(http.StatusUnauthorized)
		component.Render(r.Context(), w)
		return
	}

	// Create session
	token, err := h.sessionAuth.CreateSession()
	if err != nil {
		component := templates.LoginPage("Authentication error")
		w.WriteHeader(http.StatusInternalServerError)
		component.Render(r.Context(), w)
		return
	}

	// Set session cookie
	middleware.SetSessionCookie(w, token)

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusFound)
}

// HandleLogout handles the logout request
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Get session token from cookie and destroy it
	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		h.sessionAuth.DestroySession(cookie.Value)
	}

	// Clear session cookie
	middleware.ClearSessionCookie(w)

	// Redirect to login
	http.Redirect(w, r, "/login", http.StatusFound)
}
