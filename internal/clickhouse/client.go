package clickhouse

// Client connection lifecycle: options, construction, hooks, close/reconnect.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Default values for query execution
const (
	// queryTimeoutGrace is added to the ClickHouse max_execution_time when
	// deriving the Go context deadline, so ClickHouse's own timeout trips first
	// (returning a proper CH error); the Go deadline is only a backstop for
	// network/driver stalls that the server-side setting can't bound.
	queryTimeoutGrace = 5 * time.Second

	// fieldValuesConcurrency bounds how many per-field distinct-value queries run
	// in parallel in GetAllFilterableFieldValues, so a wide table doesn't fan out
	// into dozens of simultaneous ClickHouse queries.
	fieldValuesConcurrency = 6

	// DefaultQueryTimeout is the default max_execution_time in seconds if not specified
	DefaultQueryTimeout = 60
	// MaxQueryTimeout is the maximum allowed timeout to prevent resource abuse
	MaxQueryTimeout = 300 // 5 minutes
)

// Client represents a connection to a ClickHouse database using the native protocol.
// It provides methods for executing queries and retrieving metadata.
type Client struct {
	conn       driver.Conn // Underlying ClickHouse native connection.
	logger     *slog.Logger
	queryHooks []QueryHook         // Hooks to execute before/after queries.
	mu         sync.Mutex          // Protects shared resources within the client if any
	opts       *clickhouse.Options // Stores connection options for reconnection
	sourceID   string              // Source ID for metrics tracking
	source     *models.Source      // Source model for metrics with meaningful labels
	metrics    *metrics.ClickHouseMetrics
}

// ClientOptions holds configuration for establishing a new ClickHouse client connection.
type ClientOptions struct {
	Host      string         // Hostname or IP address.
	Database  string         // Target database name.
	Username  string         // Username for authentication.
	Password  string         // Password for authentication.
	Settings  map[string]any // Additional ClickHouse settings (e.g., max_execution_time).
	SourceID  string         // Source ID for metrics tracking.
	Source    *models.Source // Source model for enhanced metrics.
	TLSEnable bool           // Enable TLS for the connection.
}

// NewClient establishes a new connection to a ClickHouse server using the native protocol.
// It takes connection options and a logger, creates the connection, and returns a Client instance.
// Note: This does not automatically verify the connection with a ping - callers should do that if needed.
func NewClient(opts ClientOptions, logger *slog.Logger) (*Client, error) {
	// Ensure host includes the native protocol port if not specified.
	// Default to 9440 for TLS connections, 9000 for plaintext.
	host := opts.Host
	if !strings.Contains(host, ":") {
		if opts.TLSEnable {
			host += ":9440"
		} else {
			host += ":9000"
		}
	}

	// Build TLS config if enabled. Uses the system root CA pool; operators
	// who need a custom CA bundle should install it into the OS trust store.
	var tlsCfg *tls.Config
	if opts.TLSEnable {
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			logger.Warn("failed to load system cert pool, falling back to empty pool", "error", err)
			rootCAs = x509.NewCertPool()
		}
		tlsCfg = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		}
	}

	options := &clickhouse.Options{
		Addr: []string{host},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		Settings: clickhouse.Settings{
			// Default settings.
			"max_execution_time": 60,
		},
		DialTimeout: 10 * time.Second,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Protocol: clickhouse.Native,
		TLS:      tlsCfg,
	}

	// Apply any additional user-provided settings.
	if opts.Settings != nil {
		maps.Copy(options.Settings, opts.Settings)
	}

	logger.Debug("creating clickhouse connection",
		"host", host,
		"database", opts.Database,
		"protocol", "native",
		"tls", opts.TLSEnable,
	)

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("creating clickhouse connection: %w", err)
	}

	client := &Client{
		conn:       conn,
		logger:     logger,
		queryHooks: []QueryHook{}, // Initialize hooks slice.
		opts:       options,
		sourceID:   opts.SourceID,
		source:     opts.Source,
	}

	// Apply a default hook for basic query logging.
	client.AddQueryHook(NewLogQueryHook(logger, false)) // Verbose logging disabled by default.

	// Add metrics hook if source is provided
	if opts.Source != nil {
		client.AddQueryHook(metrics.NewMetricsQueryHook(opts.Source))
		client.metrics = metrics.NewClickHouseMetrics(opts.Source)
	}

	return client, nil
}

// AddQueryHook registers a hook to be executed before and after queries run by this client.
func (c *Client) AddQueryHook(hook QueryHook) {
	c.queryHooks = append(c.queryHooks, hook)
}

// executeQueryWithHooks wraps the execution of a query function (`fn`)
// with the registered BeforeQuery and AfterQuery hooks.
func (c *Client) executeQueryWithHooks(ctx context.Context, query string, fn func(context.Context) error) error {
	var err error
	start := time.Now()

	// Execute BeforeQuery hooks.
	for _, hook := range c.queryHooks {
		ctx, err = hook.BeforeQuery(ctx, query)
		if err != nil {
			// If a hook fails, log and return the error immediately.
			c.logger.Error("query hook BeforeQuery failed", "hook", fmt.Sprintf("%T", hook), "error", err)
			return fmt.Errorf("BeforeQuery hook failed: %w", err)
		}
	}

	// Execute the actual query function.
	err = fn(ctx) // This might be conn.Query, conn.Exec, etc.
	duration := time.Since(start)

	// Execute AfterQuery hooks, regardless of query success/failure.
	for _, hook := range c.queryHooks {
		// Hooks should ideally handle logging internally if needed.
		hook.AfterQuery(ctx, query, err, duration)
	}

	return err // Return the error from the query function itself.
}

// Close terminates the underlying database connection with a timeout.
func (c *Client) Close() error {
	c.logger.Debug("closing clickhouse connection")

	// Create a timeout context for the close operation
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Create a channel to signal completion
	done := make(chan error, 1)

	// Close the connection in a goroutine
	go func() {
		done <- c.conn.Close()
	}()

	// Wait for close to complete or timeout
	select {
	case err := <-done:
		// Connection closed normally
		return err
	case <-ctx.Done():
		// Timeout occurred
		c.logger.Warn("timeout while closing clickhouse connection, abandoning")
		return fmt.Errorf("timeout while closing connection")
	}
}

// Reconnect attempts to re-establish the connection to the ClickHouse server.
// This is useful for recovering from connection failures during health checks.
func (c *Client) Reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	success := false
	defer func() {
		if c.metrics != nil {
			c.metrics.RecordReconnection(success)
			c.metrics.UpdateConnectionStatus(success)
		}
	}()

	// Only attempt reconnect if connection exists but is failing
	if c.conn != nil {
		// Try to close the existing connection first with a timeout
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()

		closeComplete := make(chan struct{})
		go func() {
			_ = c.conn.Close() // Ignore close errors
			close(closeComplete)
		}()

		// Wait for close to complete or timeout
		select {
		case <-closeComplete:
			// Successfully closed
			c.logger.Debug("successfully closed old connection for reconnect")
		case <-closeCtx.Done():
			// Timeout occurred
			c.logger.Warn("timeout closing old connection for reconnect, proceeding anyway")
		}
	}

	// Use stored connection options
	if c.opts == nil {
		return fmt.Errorf("missing connection options for reconnect")
	}

	// Create a new connection with the same settings
	newConn, err := clickhouse.Open(c.opts)
	if err != nil {
		return fmt.Errorf("reconnecting to clickhouse: %w", err)
	}

	// Test the new connection with a short timeout
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()

	if err := newConn.Ping(pingCtx); err != nil {
		// Clean up failed connection with timeout
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer closeCancel()

		go func() {
			_ = newConn.Close() // Clean up failed connection
			close(make(chan struct{}))
		}()

		// Just wait for timeout - we don't care about the result
		<-closeCtx.Done()

		return fmt.Errorf("ping after reconnect failed: %w", err)
	}

	// Replace the connection
	c.conn = newConn
	success = true
	c.logger.Debug("reconnected to clickhouse")
	return nil
}
