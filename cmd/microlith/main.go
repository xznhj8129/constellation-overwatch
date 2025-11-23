package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"constellation-overwatch/db"
	"constellation-overwatch/pkg/services"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
	"constellation-overwatch/pkg/services/logger"
	"constellation-overwatch/pkg/services/web"
	"constellation-overwatch/pkg/services/workers"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		logger.Info("No .env file found, using environment variables")
	} else {
		logger.Info("Loaded configuration from .env file")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create service manager
	serviceManager := services.NewManager()

	// Initialize database service
	dbService, err := db.NewService()
	if err != nil {
		logger.Fatal("Failed to initialize database service", zap.Error(err))
	}
	serviceManager.AddService(dbService)

	// Initialize NATS service
	natsService, err := embeddednats.NewService()
	if err != nil {
		logger.Fatal("Failed to initialize NATS service", zap.Error(err))
	}
	serviceManager.AddService(natsService)

	// Start core services (DB and NATS)
	logger.Info("Starting core services...")
	if err := serviceManager.Start(ctx); err != nil {
		logger.Fatal("Failed to start core services", zap.Error(err))
	}

	// Initialize workers service
	workerManager, err := workers.NewManager(natsService, dbService.GetDB())
	if err != nil {
		logger.Fatal("Failed to create worker manager", zap.Error(err))
	}
	if err := workerManager.Start(); err != nil {
		logger.Fatal("Failed to start workers", zap.Error(err))
	}

	// Initialize web service
	webService, err := web.NewWebService(dbService, natsService.GetConnection(), natsService)
	if err != nil {
		logger.Fatal("Failed to create web service", zap.Error(err))
	}
	if err := webService.Start(ctx); err != nil {
		logger.Fatal("Failed to start web service", zap.Error(err))
	}

	// Print startup information
	printStartupInfo()

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down server...")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop services in reverse order
	if err := webService.Stop(shutdownCtx); err != nil {
		logger.Error("Failed to stop web service", zap.Error(err))
	}

	if err := workerManager.Stop(); err != nil {
		logger.Error("Failed to stop workers", zap.Error(err))
	}

	if err := serviceManager.Stop(shutdownCtx); err != nil {
		logger.Error("Failed to stop services", zap.Error(err))
	}

	logger.Info("Server shutdown complete")
}

func printStartupInfo() {
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")

	logger.Info("Bearer token", zap.String("token", getAPIToken()))
	logger.Info("─────────────────────────────────────────────────────")
	logger.Info("Local access:")
	logger.Info("Web UI available", zap.String("url", "http://localhost:"+port))
	logger.Info("API available", zap.String("url", "http://localhost:"+port+"/api/v1/"))

	// If binding to all interfaces, show network IP
	if host == "0.0.0.0" || host == "" {
		if localIP := getLocalIP(); localIP != "" {
			logger.Info("Network access (other devices on LAN):")
			logger.Info("Web UI network access", zap.String("url", "http://"+localIP+":"+port))
			logger.Info("API network access", zap.String("url", "http://"+localIP+":"+port+"/api/v1/"))
			logger.Info("NATS network access", zap.String("url", "nats://"+localIP+":4222"))
		}
	}
	logger.Info("─────────────────────────────────────────────────────")
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
