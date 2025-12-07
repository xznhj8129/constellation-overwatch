package api

import (
	"database/sql"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/handlers"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"

	"github.com/go-chi/chi/v5"
)

func NewRouter(db *sql.DB, nats *embeddednats.EmbeddedNATS) http.Handler {
	r := chi.NewRouter()

	// Services
	orgService := services.NewOrganizationService(db, nats)
	entityService := services.NewEntityService(db, nats)

	// Handlers
	healthHandler := handlers.NewHealthHandler(db, nats)
	orgHandler := handlers.NewOrganizationHandler(orgService)
	entityHandler := handlers.NewEntityHandler(entityService)
	monitorHandler := handlers.NewMonitorHandler()
	videoHandler := handlers.NewVideoHandler(nats)
	webrtcHandler := handlers.NewWebRTCHandler(nats)
	go func() {
		if err := webrtcHandler.Start(); err != nil {
			// Log error (using fmt for now as we don't have a logger passed in)
			// In a real app, we should pass a logger
			println("WebRTC Handler Start Error:", err.Error())
		}
	}()

	// Global Middleware
	r.Use(middleware.CORS)

	// Routes
	r.Route("/v1", func(r chi.Router) {
		// Health
		r.Get("/health", healthHandler.Check)

		// API Documentation
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(apiDocsHTML))
		})

		// System Monitor
		r.Get("/sys/monitor/sse", monitorHandler.SSE)

		// Organizations
		r.Route("/organizations", func(r chi.Router) {
			r.Use(middleware.BearerAuth)
			r.Get("/", orgHandler.List)
			r.Post("/", orgHandler.Create)
			r.Delete("/", orgHandler.Delete)
		})

		// Entities
		r.Route("/entities", func(r chi.Router) {
			r.Use(middleware.BearerAuth)
			r.Get("/", entityHandler.List)
			r.Post("/", entityHandler.Create)
			r.Put("/", entityHandler.Update)
			r.Delete("/", entityHandler.Delete)
		})

		// Video streams (no auth - streaming endpoints)
		r.Route("/video", func(r chi.Router) {
			r.Get("/list", videoHandler.List)
			r.Get("/stream/*", videoHandler.Stream)
		})

		// WebRTC Signaling
		r.Post("/webrtc/signal", webrtcHandler.Signal)
	})

	return r
}

const apiDocsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Constellation Overwatch API Documentation</title>
    <style>
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; 
            margin: 0; 
            padding: 20px; 
            background: #f5f5f5; 
            line-height: 1.6; 
        }
        .container { 
            max-width: 1200px; 
            margin: 0 auto; 
            background: white; 
            padding: 30px; 
            border-radius: 8px; 
            box-shadow: 0 2px 4px rgba(0,0,0,0.1); 
        }
        h1 { 
            color: #2c3e50; 
            border-bottom: 3px solid #3498db; 
            padding-bottom: 10px; 
        }
        h2 { 
            color: #34495e; 
            margin-top: 30px; 
        }
        .endpoint { 
            background: #ecf0f1; 
            border-left: 4px solid #3498db; 
            padding: 15px; 
            margin: 10px 0; 
            border-radius: 4px; 
        }
        .method { 
            display: inline-block; 
            padding: 4px 8px; 
            border-radius: 4px; 
            font-weight: bold; 
            color: white; 
            margin-right: 10px; 
        }
        .get { background: #2ecc71; }
        .post { background: #f39c12; }
        .put { background: #9b59b6; }
        .delete { background: #e74c3c; }
        .auth { 
            background: #e8f6f3; 
            border: 1px solid #16a085; 
            padding: 10px; 
            border-radius: 4px; 
            margin: 10px 0; 
        }
        .params { 
            background: #fdf2e9; 
            border: 1px solid #f39c12; 
            padding: 10px; 
            border-radius: 4px; 
            margin: 10px 0; 
        }
        code { 
            background: #2c3e50; 
            color: #ecf0f1; 
            padding: 2px 6px; 
            border-radius: 3px; 
            font-family: 'Monaco', 'Menlo', monospace; 
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🛰️ Constellation Overwatch API</h1>
        <p>C4ISR (Command, Control, Communications, and Intelligence) server mesh for drone/robotic communication</p>
        
        <div class="auth">
            <strong>🔐 Authentication:</strong> All protected endpoints require a Bearer token in the Authorization header:<br>
            <code>Authorization: Bearer &lt;your-token&gt;</code>
        </div>

        <h2>📡 Health & System</h2>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/health</strong>
            <p>Get the health status of the API and its dependencies</p>
        </div>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/sys/monitor/sse</strong>
            <p>Server-Sent Events endpoint for system monitoring</p>
        </div>

        <h2>🏢 Organizations</h2>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/organizations</strong> 🔒
            <p>List all organizations</p>
        </div>
        <div class="endpoint">
            <span class="method post">POST</span> <strong>/v1/organizations</strong> 🔒
            <p>Create a new organization</p>
            <div class="params">
                <strong>Body:</strong> <code>{"name": "string", "org_type": "company|agency|individual", "description": "string", "metadata": {}}</code>
            </div>
        </div>
        <div class="endpoint">
            <span class="method delete">DELETE</span> <strong>/v1/organizations</strong> 🔒
            <p>Delete an organization</p>
            <div class="params">
                <strong>Query:</strong> <code>org_id</code> (required)
            </div>
        </div>

        <h2>🤖 Entities</h2>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/entities</strong> 🔒
            <p>List entities for an organization</p>
            <div class="params">
                <strong>Query:</strong> <code>org_id</code> (required)
            </div>
        </div>
        <div class="endpoint">
            <span class="method post">POST</span> <strong>/v1/entities</strong> 🔒
            <p>Create a new entity</p>
            <div class="params">
                <strong>Query:</strong> <code>org_id</code> (required)<br>
                <strong>Body:</strong> <code>{"entity_type": "string", "name": "string", "position": {"latitude": 0.0, "longitude": 0.0, "altitude": 0.0}, "status": "active|inactive|unknown", "priority": "low|normal|high|critical", "metadata": {}}</code>
            </div>
        </div>
        <div class="endpoint">
            <span class="method put">PUT</span> <strong>/v1/entities</strong> 🔒
            <p>Update an existing entity</p>
            <div class="params">
                <strong>Query:</strong> <code>org_id</code> (required), <code>entity_id</code> (required)<br>
                <strong>Body:</strong> Object with fields to update
            </div>
        </div>
        <div class="endpoint">
            <span class="method delete">DELETE</span> <strong>/v1/entities</strong> 🔒
            <p>Delete an entity</p>
            <div class="params">
                <strong>Query:</strong> <code>org_id</code> (required), <code>entity_id</code> (required)
            </div>
        </div>

        <h2>📹 Video Streams</h2>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/video/list</strong>
            <p>List available video streams (no authentication required)</p>
        </div>
        <div class="endpoint">
            <span class="method get">GET</span> <strong>/v1/video/stream/*</strong>
            <p>Access video stream endpoint (no authentication required)</p>
        </div>

        <h2>📊 Response Codes</h2>
        <ul>
            <li><strong>200 OK:</strong> Success</li>
            <li><strong>201 Created:</strong> Resource created successfully</li>
            <li><strong>400 Bad Request:</strong> Invalid request parameters</li>
            <li><strong>401 Unauthorized:</strong> Missing or invalid authentication</li>
            <li><strong>404 Not Found:</strong> Resource not found</li>
            <li><strong>500 Internal Server Error:</strong> Server error</li>
            <li><strong>503 Service Unavailable:</strong> Service is down</li>
        </ul>

        <h2>🏷️ Entity Types</h2>
        <p>Common entity types include:</p>
        <ul>
            <li><code>aircraft_multirotor</code> - Drones/UAVs</li>
            <li><code>ground_vehicle</code> - Ground robots/vehicles</li>
            <li><code>sensor_fixed</code> - Stationary sensors</li>
            <li><code>control_station</code> - Command centers</li>
        </ul>

        <footer style="margin-top: 40px; padding-top: 20px; border-top: 1px solid #ecf0f1; color: #7f8c8d; text-align: center;">
            <p>🛰️ Constellation Overwatch API v1.0 • Built with ❤️ for C4ISR operations</p>
        </footer>
    </div>
</body>
</html>`
