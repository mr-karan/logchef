package clickhouse

import "time"

const (
	// Connection constants
	defaultTimeout          = 10 * time.Second
	defaultMaxOpenConns     = 5
	defaultMaxIdleConns     = 2
	defaultConnMaxLifetime  = time.Hour
	defaultBlockBufferSize  = 10
	defaultMaxExecutionTime = 60

	// Health check constants
	healthCheckInterval = 30 * time.Second
	healthCheckTimeout  = 10 * time.Second
	healthChannelBuffer = 100
)
