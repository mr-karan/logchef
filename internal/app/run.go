package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Run creates, initializes, and runs the application, handling graceful shutdown.
func Run(opts Options) error {
	// Create base context for cancellation propagation.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create application instance.
	app, err := New(opts)
	if err != nil {
		return err // Error during config loading or basic app setup.
	}

	// Initialize core components (DBs, OIDC, HTTP server).
	if err := app.Initialize(ctx); err != nil {
		app.Logger.Error("failed to initialize application", "error", err)
		return err
	}

	// Set up signal handling for graceful shutdown.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Start the HTTP server in a separate goroutine.
	go func() {
		if err := app.Start(); err != nil {
			// Log the error and trigger shutdown if server fails to start.
			app.Logger.Error("failed to start application server", "error", err)
			// Use non-blocking send in case channel is already full or shutdown initiated.
			select {
			case shutdown <- syscall.SIGTERM:
			default:
			}
		}
	}()

	app.Logger.Info("application started")

	// Wait for shutdown signal (Ctrl+C or SIGTERM).
	<-shutdown
	app.Logger.Info("received shutdown signal")

	// Create a context with timeout for graceful shutdown phase.
	shutdownTimeout := 10 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	app.Logger.Info("shutting down application", "timeout", shutdownTimeout.String())

	// Perform graceful shutdown of components (server, DBs).
	if err := app.Shutdown(shutdownCtx); err != nil {
		// Log shutdown error, but exit gracefully anyway.
		app.Logger.Error("error during application shutdown", "error", err)
		os.Exit(1)
	}

	app.Logger.Info("shutdown complete")
	return nil // Return nil on successful shutdown sequence.
}
