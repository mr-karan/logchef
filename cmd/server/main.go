package main

import (
	"flag"
	"os"
	"strings"

	"github.com/mr-karan/logchef/pkg/logger"

	"github.com/mr-karan/logchef/internal/app"
)

var (
	// Build information, set by linker flags
	version    = "unknown"
	commit     = "unknown"
	commitDate = "unknown"
	buildTime  = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	// Initialize global logger with default configuration
	// This will be reconfigured with the proper level once the app loads its config
	log := logger.New(false)

	// Log startup information
	log.Info("starting logchef", 
		"version", version,
		"commit", commit,
		"commitDate", formatBuildString(commitDate),
		"buildTime", formatBuildString(buildTime))

	// Run the application with the initialized logger
	if err := app.Run(app.Options{
		ConfigPath: *configPath,
		WebFS:      getWebFS(),
		BuildInfo:  version + " (Commit: " + formatBuildString(commitDate) + " (" + commit + "), Build: " + formatBuildString(buildTime) + ")",
	}); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}

// formatBuildString replaces underscores with spaces for display
func formatBuildString(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}
