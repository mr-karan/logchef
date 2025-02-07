package clickhouse

import "time"

const (
	// HTTP Client settings
	defaultHTTPTimeout     = 30 * time.Second
	defaultMaxIdleConns    = 100
	defaultMaxConnsPerHost = 100
	defaultIdleConnTimeout = 90 * time.Second
	defaultBufferSize      = "3000000"

	// ClickHouse HTTP settings
	defaultWaitEndOfQuery = "1"
	defaultMaxMemoryUsage = "10000000000" // 10GB
	defaultMaxExecTime    = "60"          // 60 seconds
	defaultFormat         = "JSONCompact"

	// Compression settings
	defaultCompressionMethod = "gzip"
	defaultZlibLevel         = "3" // Medium compression level

	// Health check constants
	healthCheckInterval = 30 * time.Second
	healthCheckTimeout  = 10 * time.Second
	healthCheckQuery    = "SELECT 1"
)

// HTTPSettings represents ClickHouse HTTP settings
type HTTPSettings struct {
	// Query execution settings
	MaxMemoryUsage   string
	MaxExecutionTime string
	WaitEndOfQuery   string
	Format           string
	BufferSize       string

	// Compression settings
	EnableCompression bool
	CompressionMethod string
	ZlibLevel         string
}

// DefaultHTTPSettings returns default HTTP settings
func DefaultHTTPSettings() HTTPSettings {
	return HTTPSettings{
		MaxMemoryUsage:    defaultMaxMemoryUsage,
		MaxExecutionTime:  defaultMaxExecTime,
		WaitEndOfQuery:    defaultWaitEndOfQuery,
		Format:            defaultFormat,
		BufferSize:        defaultBufferSize,
		EnableCompression: true,
		CompressionMethod: defaultCompressionMethod,
		ZlibLevel:         defaultZlibLevel,
	}
}
