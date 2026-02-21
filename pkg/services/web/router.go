package web

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/mediamtx"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/dev"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/handlers"
)

func NewRouter(
	orgSvc *services.OrganizationService,
	entitySvc *services.EntityService,
	natsEmbedded *embeddednats.EmbeddedNATS,
	sseHandler *SSEHandler,
	mtxClient *mediamtx.Client,
	apiHandler http.Handler,
	sessionAuth *middleware.SessionAuth,
	authSvc *services.AuthService,
	userSvc *services.UserService,
	inviteSvc *services.InviteService,
	apiKeySvc *services.APIKeyService,
) *http.ServeMux {
	mux := http.NewServeMux()

	// Initialize handlers
	pageHandler := handlers.NewPageHandler(orgSvc, entitySvc)
	datastarHandler := handlers.NewDatastarHandler(orgSvc, entitySvc)
	overwatchHandler := handlers.NewOverwatchHandler(natsEmbedded, orgSvc)
	videoHandler := handlers.NewVideoHandler(mtxClient, entitySvc)
	authHandler := handlers.NewAuthHandler(sessionAuth, authSvc, userSvc)
	inviteHandler := handlers.NewInviteHandler(inviteSvc, userSvc, authSvc, sessionAuth)
	adminHandler := handlers.NewAdminHandler(userSvc, apiKeySvc, inviteSvc, natsEmbedded)
	specHandler := handlers.NewSpecHandler()

	// Serve static files (no auth required) - uses embedded filesystem
	mux.Handle("/static/", http.StripPrefix("/static/", StaticFileServer()))

	// Prometheus metrics endpoint (no auth required for scraping)
	mux.Handle("/metrics", metrics.Handler())

	// Health check endpoint (no auth required)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth routes (no session required)
	mux.HandleFunc("/login", authHandler.HandleLogin)
	mux.HandleFunc("/auth/passkey/login/begin", authHandler.HandlePasskeyLoginBegin)
	mux.HandleFunc("/auth/passkey/login/finish", authHandler.HandlePasskeyLoginFinish)

	// Invite routes (no session required - invite link is the auth)
	mux.HandleFunc("/invite/{token}", inviteHandler.HandleAcceptInvite)
	mux.HandleFunc("/invite/{token}/accept", inviteHandler.HandleFinalizeInvite)

	// Development-only routes (hot reload)
	if dev.IsDev() {
		hotReload := dev.NewHotReload()
		hotReload.SetupRoutes(mux)
	}

	// Helper to wrap handlers with session auth
	protect := func(h http.HandlerFunc) http.Handler {
		return sessionAuth.RequireSession(http.HandlerFunc(h))
	}

	// Protected pprof endpoints (debugging)
	metrics.RegisterPProf(mux, protect)

	// Metrics dashboard (protected)
	metricsHandler := handlers.NewMetricsHandler()
	mux.Handle("/metrics-ui", protect(metricsHandler.HandleMetricsPage))
	mux.Handle("/api/metrics/sse", protect(metricsHandler.HandleSSE))

	mux.HandleFunc("/logout", authHandler.HandleLogout)

	// Passkey setup page (protected — user must have a session, even with needsPasskeySetup)
	mux.Handle("/setup-passkey", protect(authHandler.HandleSetupPasskey))

	// Protected passkey registration (requires active session)
	mux.Handle("/auth/passkey/register/begin", protect(authHandler.HandlePasskeyRegisterBegin))
	mux.Handle("/auth/passkey/register/finish", protect(authHandler.HandlePasskeyRegisterFinish))

	// Protected admin API endpoints
	mux.Handle("/api/admin/users", protect(adminHandler.HandleListUsers))
	mux.Handle("/api/admin/invites", protect(adminHandler.HandleCreateInvite))
	mux.Handle("/api/admin/invites/{id}", protect(adminHandler.HandleRevokeInvite))
	mux.Handle("/api/admin/apikeys", protect(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.HandleListAPIKeys(w, r)
		case http.MethodPost:
			adminHandler.HandleCreateAPIKey(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/admin/apikeys/{id}", protect(adminHandler.HandleRevokeAPIKey))

	// Protected Pages
	mux.Handle("/", protect(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/map", http.StatusFound)
	}))
	mux.Handle("/organizations", protect(pageHandler.HandleEntitiesPage))
	mux.Handle("/organizations/entities/new", protect(pageHandler.HandleEntityForm))
	mux.Handle("/organizations/entities/edit", protect(pageHandler.HandleEntityForm))
	mux.Handle("/organizations/new", protect(pageHandler.HandleOrganizationForm))
	mux.Handle("/organizations/edit/", protect(datastarHandler.HandleOrganizationEdit))
	mux.Handle("/organizations/cancel/", protect(datastarHandler.HandleOrganizationCancel))
	mux.Handle("/streams", protect(pageHandler.HandleStreamsPage))
	mux.Handle("/overwatch", protect(pageHandler.HandleOverwatchPage))
	mux.Handle("/fleet", protect(pageHandler.HandleFleetPage))
	mux.Handle("/fleet/edit/", protect(datastarHandler.HandleFleetEdit))
	mux.Handle("/fleet/cancel/", protect(datastarHandler.HandleFleetCancel))
	mux.Handle("/map", protect(pageHandler.HandleMapPage))
	mux.Handle("/video", protect(pageHandler.HandleVideoPage))
	mux.Handle("/admin", protect(pageHandler.HandleAdminPage))

	// Protected Web API endpoints (for Datastar/SSE)
	mux.Handle("/api/organizations", protect(datastarHandler.HandleAPIOrganizations))
	mux.Handle("/api/organizations/", protect(datastarHandler.HandleAPIOrganization))
	mux.Handle("/api/organizations/update", protect(datastarHandler.HandleAPIOrganizationUpdate))

	mux.Handle("/api/entities", protect(datastarHandler.HandleAPIEntities))
	mux.Handle("/api/entities/", protect(datastarHandler.HandleAPIEntity))

	mux.Handle("/api/fleet", protect(datastarHandler.HandleAPIFleet))
	mux.Handle("/api/fleet/update", protect(datastarHandler.HandleAPIFleetUpdate))
	mux.Handle("/api/fleet/", protect(datastarHandler.HandleAPIFleetEntity))

	mux.Handle("/api/overwatch/kv", protect(overwatchHandler.HandleAPIOverwatchKV))
	mux.Handle("/api/overwatch/kv/watch", protect(overwatchHandler.HandleAPIOverwatchKVWatch))
	mux.Handle("/api/overwatch/kv/debug", protect(overwatchHandler.HandleAPIOverwatchKVDebug))

	mux.Handle("/api/video/list", protect(videoHandler.HandleAPIVideoList))
	mux.Handle("/api/video/status", protect(videoHandler.HandleAPIVideoStatus))

	// Mount REST API (has its own API key auth)
	if apiHandler != nil {
		mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
	}

	// OpenAPI Spec (no auth required)
	mux.Handle("/api/openapi.json", specHandler)

	// Protected SSE endpoint for streams
	mux.Handle("/api/streams/sse", protect(func(w http.ResponseWriter, r *http.Request) {
		sseHandler.StreamMessages(w, r)
	}))

	return mux
}
