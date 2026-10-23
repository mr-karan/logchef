package main

import (
	"errors"
	"flag"
	"io/fs"
	"os"

	"github.com/mr-karan/logchef/internal/app"
	"github.com/mr-karan/logchef/pkg/logger"
)

var (
	// buildString is the build version information, set by linker flags during build.
	buildString = "unknown"
	// versionString is the version information, set by linker flags during build.
	versionString = "unknown"
)

const defaultConfigPath = "config.toml"

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to config file")
	flag.Parse()

	// Initialize logger before config is loaded.
	// It will be reconfigured with the correct level once the app loads its config.
	log := logger.New(false)

	log.Info("starting logchef", "buildInfo", buildString)

	// Run the application.
	if err := app.Run(app.Options{
		ConfigPath: resolveConfigPath(*configPath, configFlagSet()),
		WebFS:      getWebFS(),
		BuildInfo:  buildString,
		Version:    versionString,
	}); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}

// configFlagSet reports whether --config was passed on the command line.
func configFlagSet() bool {
	set := false
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == "config" })
	return set
}

// resolveConfigPath returns the config file to load. The default path is
// optional so a deployment can configure through the environment alone, and an
// empty result means exactly that. A path given with --config is returned
// as is, so a missing file fails when it is loaded.
func resolveConfigPath(path string, explicit bool) string {
	if explicit {
		return path
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return path
}
