package app

import (
	"context"
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

	// Create application
	app, err := New(opts)
	if err != nil {
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
			app.log.Error("failed to start application", "error", err)
			shutdown <- os.Interrupt
		}
	}()

	app.log.Info("application started")

	// Wait for shutdown signal
	<-shutdown
	app.log.Info("received shutdown signal")

	// Create shutdown context with timeout
	shutdownTimeout := 10 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	app.log.Info("shutting down application", "timeout", shutdownTimeout.String())

	// Shutdown application
	if err := app.Shutdown(shutdownCtx); err != nil {
		app.log.Error("failed to shutdown application", "error", err)
		return err
	}

	app.log.Info("shutdown complete")
	return nil
}
