// Package main provides the entry point for the LogChef CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/mr-karan/logchef/cmd/logchef/commands"
)

// Version information set by goreleaser
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Set up signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Build and run the CLI
	app := commands.New(version, commit, date)
	if err := app.Run(ctx, os.Args); err != nil {
		log.Error("command failed", "error", err)
		os.Exit(1)
	}
}
