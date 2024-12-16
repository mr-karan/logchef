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

	return config.ClickhouseConfig{
		Host:        parsedURL.Hostname(),
		Port:        port,
		Database:    queryParams.Get("database"),
		Username:    queryParams.Get("username"),
		Password:    queryParams.Get("password"),
		DialTimeout: dialTimeout,
	}
}
