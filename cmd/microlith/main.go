package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api"
	"github.com/Constellation-Overwatch/constellation-overwatch/db"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/transcoder"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/workers"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		logger.Info("No .env file found, using environment variables")
	}

	// Initialize logger (handled by init() in logger package)
	defer logger.Sync()

	logger.Info("Starting Constellation Overwatch Microlith...")

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize Database
	dbService, err := db.NewService()
	if err != nil {
		logger.Fatalw("Failed to initialize database service", "error", err)
	}
	if err := dbService.Start(ctx); err != nil {
		logger.Fatalw("Failed to start database service", "error", err)
	}
	defer dbService.Stop(ctx)

	// 2. Initialize Embedded NATS
	natsService, err := embeddednats.NewService()
	if err != nil {
		logger.Fatalw("Failed to initialize NATS service", "error", err)
	}
	if err := natsService.Start(ctx); err != nil {
		logger.Fatalw("Failed to start NATS service", "error", err)
	}
	defer natsService.Stop(ctx)

	// Wait for NATS to be ready
	time.Sleep(1 * time.Second)

	// Get NATS connection
	nc := natsService.Connection()

	// 3. Initialize Workers
	logger.Info("Initializing workers...")
	workerManager, err := workers.NewManager(natsService, dbService.GetDB())
	if err != nil {
		logger.Fatalw("Failed to initialize worker manager", "error", err)
	}

	// Start workers
	logger.Info("Starting workers...")
	if err := workerManager.Start(); err != nil {
		logger.Fatalw("Failed to start workers", "error", err)
	}
	defer workerManager.Stop(ctx)
	logger.Info("Workers started")

	// 3b. Initialize Video Transcoder (converts MPEG-TS to JPEG)
	logger.Info("Initializing video transcoder...")
	videoTranscoder := transcoder.New(nc)
	go func() {
		if err := videoTranscoder.Start(ctx); err != nil {
			logger.Errorw("Video transcoder error", "error", err)
		}
	}()
	logger.Info("Video transcoder started")

	// 4. Initialize API Router
	logger.Info("Initializing API router...")
	apiHandler := api.NewRouter(dbService.GetDB(), natsService)

	// 5. Initialize Web Server
	logger.Info("Initializing web server...")
	webServer, err := web.NewWebService(dbService, nc, natsService, apiHandler)
	if err != nil {
		logger.Fatalw("Failed to initialize web server", "error", err)
	}

	// Start web server
	logger.Info("Starting web server...")
	if err := webServer.Start(ctx); err != nil {
		logger.Fatalw("Failed to start web server", "error", err)
	}
	logger.Info("Web server start command issued")
	defer webServer.Stop(ctx)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Infow("Received signal, shutting down...", "signal", sig)

	// Cancel context to stop all services
	cancel()

	// Give services time to shut down
	time.Sleep(2 * time.Second)
	logger.Info("Shutdown complete")
}
