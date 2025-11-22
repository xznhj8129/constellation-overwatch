package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"constellation-overwatch/db"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
	"constellation-overwatch/pkg/services"
	"constellation-overwatch/pkg/services/web"
	"constellation-overwatch/pkg/services/workers"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	} else {
		log.Println("Loaded configuration from .env file")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create service manager
	serviceManager := services.NewManager()

	// Initialize database service
	dbService, err := db.NewService()
	if err != nil {
		log.Fatal("Failed to initialize database service:", err)
	}
	serviceManager.AddService(dbService)

	// Initialize NATS service
	natsService, err := embeddednats.NewService()
	if err != nil {
		log.Fatal("Failed to initialize NATS service:", err)
	}
	serviceManager.AddService(natsService)

	// Start core services (DB and NATS)
	log.Println("Starting core services...")
	if err := serviceManager.Start(ctx); err != nil {
		log.Fatal("Failed to start core services:", err)
	}

	// Initialize workers service
	workerManager, err := workers.NewManager(natsService, dbService.GetDB())
	if err != nil {
		log.Fatal("Failed to create worker manager:", err)
	}
	if err := workerManager.Start(); err != nil {
		log.Fatal("Failed to start workers:", err)
	}

	// Initialize web service
	webService, err := web.NewWebService(dbService, natsService.GetConnection(), natsService)
	if err != nil {
		log.Fatal("Failed to create web service:", err)
	}
	if err := webService.Start(ctx); err != nil {
		log.Fatal("Failed to start web service:", err)
	}

	// Print startup information
	printStartupInfo()

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down server...")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop services in reverse order
	if err := webService.Stop(shutdownCtx); err != nil {
		log.Printf("Failed to stop web service: %v", err)
	}

	if err := workerManager.Stop(); err != nil {
		log.Printf("Failed to stop workers: %v", err)
	}

	if err := serviceManager.Stop(shutdownCtx); err != nil {
		log.Printf("Failed to stop services: %v", err)
	}

	log.Println("Server shutdown complete")
}

func printStartupInfo() {
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")

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
}

func getAPIToken() string {
	token := os.Getenv("API_BEARER_TOKEN")
	if token == "" {
		token = "constellation-dev-token"
	}
	return token
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
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