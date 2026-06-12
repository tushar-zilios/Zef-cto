package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cto/src/internal/config"
	"cto/src/internal/db"
	"cto/src/internal/logger"
	"cto/src/internal/routes"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Fatal error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if err := logger.Init(); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Cleanup()

	fmt.Println("Starting Zef CTO service...")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	port := cfg.Port

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, err = db.InitCTODB(ctx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	if db.CTOPoolReady() {
		log.Println("DB connection pool initialized successfully.")
	} else {
		log.Println("DB not configured (DATABASE_URL not set) — endpoints will return 503.")
	}
	defer db.CloseCTODB()

	router := routes.NewRouter()
	serverAddr := ":" + port

	srv := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	serverErrChan := make(chan error, 1)
	go func() {
		log.Printf("CTO service starting on %s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
	}()

	var runErr error
	select {
	case err := <-serverErrChan:
		log.Printf("HTTP server failed: %v", err)
		runErr = fmt.Errorf("HTTP server failed: %w", err)
	case sig := <-shutdownChan:
		log.Printf("Received signal %v, shutting down gracefully...", sig)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: HTTP server Shutdown failed: %v", err)
	} else {
		log.Println("HTTP server shutdown successfully.")
	}

	return runErr
}
