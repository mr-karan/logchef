package main

import (
	"flag"
	"os"

	"github.com/mr-karan/logchef/pkg/logger"

	"github.com/mr-karan/logchef/internal/app"
)

var (
	// Build information, set by linker flags
	buildString = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	// Initialize global logger with default configuration
	// This will be reconfigured once the app loads its config
	logger.Setup(logger.Config{}) // Use defaults
	log := logger.NewLogger("main")

	// Log startup information
	log.Info("starting logchef server",
		"buildInfo", buildString,
	)

	// Run the application with the initialized logger
	if err := app.Run(app.Options{
		ConfigPath: *configPath,
		WebFS:      getWebFS(),
		BuildInfo:  buildString,
		Logger:     log,
	}); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}
