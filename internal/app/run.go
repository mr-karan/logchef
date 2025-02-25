package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Run creates, initializes, and runs the application until shutdown
// This provides a simpler way to run the application with proper lifecycle management
func Run(opts Options) error {
	// Create base context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get logger from options or use default
	log := opts.Logger
	if log == nil {
		log = slog.Default().With("component", "app_runner")
	}

	// Create application
	app, err := New(opts)
	if err != nil {
		log.Error("failed to create application", "error", err)
		return err
	}

	// Initialize application components with context
	if err := app.Initialize(ctx); err != nil {
		app.log.Error("failed to initialize application", "error", err)
		return err
	}

	// Handle graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Start the application in a goroutine
	go func() {
		if err := app.Start(); err != nil {
			app.log.Error("application error", "error", err)
			shutdown <- os.Interrupt
		}
	}()

	app.log.Info("application started")

	// Wait for shutdown signal
	<-shutdown
	app.log.Info("shutdown signal received")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shutdown application
	if err := app.Shutdown(shutdownCtx); err != nil {
		app.log.Error("error during application shutdown", "error", err)
		return err
	}

	app.log.Info("shutdown complete")
	return nil
}
