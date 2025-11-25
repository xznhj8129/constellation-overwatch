package web

import (
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/handlers"
)

func NewRouter(
	orgSvc *services.OrganizationService,
	entitySvc *services.EntityService,
	natsEmbedded *embeddednats.EmbeddedNATS,
	sseHandler *SSEHandler,
	apiHandler http.Handler,
) *http.ServeMux {
	mux := http.NewServeMux()

	// Initialize handlers
	pageHandler := handlers.NewPageHandler(orgSvc, entitySvc)
	datastarHandler := handlers.NewDatastarHandler(orgSvc, entitySvc)
	overwatchHandler := handlers.NewOverwatchHandler(natsEmbedded, orgSvc)
	videoHandler := handlers.NewVideoHandler(natsEmbedded)

	// Serve static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("pkg/services/web/static"))))

	// Pages
	mux.HandleFunc("/", pageHandler.HandleEntitiesPage)
	mux.HandleFunc("/organizations", pageHandler.HandleEntitiesPage)
	mux.HandleFunc("/organizations/entities/new", pageHandler.HandleEntityForm)
	mux.HandleFunc("/organizations/entities/edit", pageHandler.HandleEntityForm)
	mux.HandleFunc("/organizations/new", pageHandler.HandleOrganizationForm)
	mux.HandleFunc("/organizations/edit/", datastarHandler.HandleOrganizationEdit)
	mux.HandleFunc("/organizations/cancel/", datastarHandler.HandleOrganizationCancel)
	mux.HandleFunc("/streams", pageHandler.HandleStreamsPage)
	mux.HandleFunc("/overwatch", pageHandler.HandleOverwatchPage)
	mux.HandleFunc("/fleet", pageHandler.HandleFleetPage)
	mux.HandleFunc("/fleet/edit/", datastarHandler.HandleFleetEdit)
	mux.HandleFunc("/fleet/cancel/", datastarHandler.HandleFleetCancel)
	mux.HandleFunc("/video", pageHandler.HandleVideoPage)

	// Web API endpoints (for Datastar/SSE)
	mux.HandleFunc("/api/organizations", datastarHandler.HandleAPIOrganizations)
	mux.HandleFunc("/api/organizations/", datastarHandler.HandleAPIOrganization) // Handles PUT/DELETE for specific org
	mux.HandleFunc("/api/organizations/update", datastarHandler.HandleAPIOrganizationUpdate)

	mux.HandleFunc("/api/entities", datastarHandler.HandleAPIEntities)
	mux.HandleFunc("/api/entities/", datastarHandler.HandleAPIEntity) // Handles PUT/DELETE for specific entity

	mux.HandleFunc("/api/fleet", datastarHandler.HandleAPIFleet)
	mux.HandleFunc("/api/fleet/update", datastarHandler.HandleAPIFleetUpdate)
	mux.HandleFunc("/api/fleet/", datastarHandler.HandleAPIFleetEntity) // Delete fleet entity

	mux.HandleFunc("/api/overwatch/kv", overwatchHandler.HandleAPIOverwatchKV)
	mux.HandleFunc("/api/overwatch/kv/watch", overwatchHandler.HandleAPIOverwatchKVWatch)
	mux.HandleFunc("/api/overwatch/kv/debug", overwatchHandler.HandleAPIOverwatchKVDebug)

	mux.HandleFunc("/api/video/list", videoHandler.HandleAPIVideoList)

	// Mount REST API
	if apiHandler != nil {
		mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
	}

	// SSE endpoint for streams
	mux.HandleFunc("/api/streams/sse", func(w http.ResponseWriter, r *http.Request) {
		sseHandler.StreamMessages(w, r)
	})

	return mux
}
