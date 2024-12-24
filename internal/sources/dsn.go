package sources

import (
	"net/url"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/pkg/config"
)

// parseDSN converts a DSN string into ClickhouseConfig
func parseDSN(dsn string) config.ClickhouseConfig {
	parsedURL, err := url.Parse(dsn)
	if err != nil {
		return config.ClickhouseConfig{}
	}

	port, _ := strconv.Atoi(parsedURL.Port())
	queryParams := parsedURL.Query()

	dialTimeout, _ := time.ParseDuration(queryParams.Get("dial_timeout"))
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second // Default timeout
	}

	// Database is specified in the path, remove leading slash
	database := parsedURL.Path
	if len(database) > 0 && database[0] == '/' {
		database = database[1:]
	}

	return config.ClickhouseConfig{
		Host:        parsedURL.Hostname(),
		Port:        port,
		Database:    database,
		Username:    queryParams.Get("username"),
		Password:    queryParams.Get("password"),
		DialTimeout: dialTimeout,
	}
}
