package main

import (
	"flag"
	"os"

	"github.com/mr-karan/logchef/internal/app"
	"github.com/mr-karan/logchef/pkg/logger"
)

var (
	// buildString is the build version information, set by linker flags during build.
	buildString = "unknown"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	// Initialize logger before config is loaded.
	// It will be reconfigured with the correct level once the app loads its config.
	log := logger.New(false)

	log.Info("starting logchef", "buildInfo", buildString)

	// Run the application.
	if err := app.Run(app.Options{
		ConfigPath: *configPath,
		WebFS:      getWebFS(),
		BuildInfo:  buildString,
	}); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}
