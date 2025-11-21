package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"constellation-overwatch/db"
	"constellation-overwatch/pkg/shared"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
	"constellation-overwatch/pkg/services/web"
	"constellation-overwatch/pkg/services/workers"

	"github.com/joho/godotenv"
)

var (
	dbService *db.Service
	nats      *embeddednats.EmbeddedNATS
)

func initDB() error {
	var err error

	// Create database service with default config
	config := db.DefaultConfig()
	config.DBPath = "./db/constellation.db"
	config.AutoInitialize = true

	dbService, err = db.New(config)
	if err != nil {
		return fmt.Errorf("failed to initialize database service: %w", err)
	}

	// Verify schema is properly initialized
	if err := dbService.VerifySchema(); err != nil {
		log.Printf("Schema verification failed: %v", err)
		log.Println("Attempting to initialize schema...")
		if err := dbService.InitializeSchema(); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	log.Println("Database service initialized successfully")
	return nil
}

func initNATS() error {
	var err error

	config := embeddednats.DefaultConfig()
	config.DataDir = "./data/nats"

	// Read NATS host from environment
	if natsHost := os.Getenv("NATS_HOST"); natsHost != "" {
		config.Host = natsHost
	}

	// Read NATS port from environment
	if natsPort := os.Getenv("NATS_PORT"); natsPort != "" {
		if port, err := strconv.Atoi(natsPort); err == nil {
			config.Port = port
		}
	}

	nats, err = embeddednats.New(config)
	if err != nil {
		return fmt.Errorf("failed to create embedded NATS: %w", err)
	}

	if err := nats.Start(); err != nil {
		return fmt.Errorf("failed to start embedded NATS: %w", err)
	}

	// Create constellation streams
	if err := nats.CreateConstellationStreams(); err != nil {
		return fmt.Errorf("failed to create constellation streams: %w", err)
	}

	// Create global state KV bucket
	if err := nats.CreateGlobalStateKV(shared.KVBucketGlobalState); err != nil {
		return fmt.Errorf("failed to create global state KV bucket: %w", err)
	}

	// Create durable consumers
	consumers := []struct {
		stream   string
		consumer string
		filter   string
	}{
		{shared.StreamEntities, shared.ConsumerEntityProcessor, shared.SubjectEntitiesAll},
		{shared.StreamCommands, shared.ConsumerCommandProcessor, shared.SubjectCommandsAll},
		{shared.StreamEvents, shared.ConsumerEventProcessor, shared.SubjectEventsAll},
		{shared.StreamTelemetry, shared.ConsumerTelemetryProcessor, shared.SubjectTelemetryAll},
	}

	for _, c := range consumers {
		if err := nats.CreateDurableConsumer(c.stream, c.consumer, c.filter); err != nil {
			return fmt.Errorf("failed to create consumer %s: %w", c.consumer, err)
		}
	}

	log.Println("NATS JetStream initialized successfully")
	return nil
}

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	} else {
		log.Println("Loaded configuration from .env file")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer dbService.Close()

	// Initialize embedded NATS
	if err := initNATS(); err != nil {
		log.Fatal("Failed to initialize NATS:", err)
	}

	// Start NATS workers with database access
	workerManager, err := workers.NewManager(nats, dbService.GetDB())
	if err != nil {
		log.Fatal("Failed to create worker manager:", err)
	}
	if err := workerManager.Start(); err != nil {
		log.Fatal("Failed to start workers:", err)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create unified web server (includes both web UI and REST API)
	webServer, err := web.NewServer(dbService, nats.GetConnection(), nats)
	if err != nil {
		log.Fatal("Failed to create web server:", err)
	}

	// Get host and port from environment
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0" // Default to all interfaces
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start unified server in goroutine
	go func() {
		bindAddr := fmt.Sprintf("%s:%s", host, port)

		log.Printf("Starting Constellation Overwatch unified server on %s", bindAddr)
		log.Printf("Bearer token: %s", getAPIToken())
		log.Println("─────────────────────────────────────────────────────")
		log.Printf("Local access:")
		log.Printf("  Web UI:  http://localhost:%s", port)
		log.Printf("  API:     http://localhost:%s/api/v1/", port)

		// If binding to all interfaces, show network IP
		if host == "0.0.0.0" || host == "" {
			if localIP := getLocalIP(); localIP != "" {
				log.Printf("Network access (other devices on LAN):")
				log.Printf("  Web UI:  http://%s:%s", localIP, port)
				log.Printf("  API:     http://%s:%s/api/v1/", localIP, port)
				log.Printf("  NATS:    nats://%s:4222", localIP)
			}
		}
		log.Println("─────────────────────────────────────────────────────")

		if err := webServer.Start(bindAddr); err != nil {
			log.Fatal("Server failed to start:", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down server...")

	// Stop workers
	if workerManager != nil {
		if err := workerManager.Stop(); err != nil {
			log.Printf("Failed to stop workers: %v", err)
		}
	}

	// Shutdown NATS
if nats != nil {
		if err := nats.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown NATS: %v", err)
		}
	}

	log.Println("Server shutdown complete")
}

func getAPIToken() string {
	token := os.Getenv("API_BEARER_TOKEN")
	if token == "" {
		token = "constellation-dev-token"
	}
	return token
}

// getLocalIP returns the non-loopback local IP of the host
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback then return it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}