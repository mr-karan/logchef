package clickhouse

// Connectivity health checks (ping + table existence).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ping checks the connectivity to the ClickHouse server and optionally verifies a table exists.
// It uses short timeouts internally. Returns nil on success, or an error indicating the failure reason.
func (c *Client) Ping(ctx context.Context, database, table string) error {
	if c.conn == nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
		}
		return errors.New("clickhouse connection is nil")
	}

	// 1. Check basic connection with a short timeout.
	pingCtx, pingCancel := context.WithTimeout(ctx, 1*time.Second)
	defer pingCancel()

	if err := c.conn.Ping(pingCtx); err != nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
			c.metrics.UpdateConnectionStatus(false)
		}

		// Check if the error is due to the context deadline exceeding
		if errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("ping timed out after 1 second")
			return fmt.Errorf("ping timed out: %w", err)
		}
		return fmt.Errorf("ping failed: %w", err)
	}

	// 2. If database and table are provided, check table existence.
	if database == "" || table == "" {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(true)
			c.metrics.UpdateConnectionStatus(true)
		}
		return nil // Basic ping successful, no table check needed.
	}

	tableCtx, tableCancel := context.WithTimeout(ctx, 1*time.Second)
	defer tableCancel()

	// Query system.tables to check if the table exists. Using QueryRow and Scan.
	// If the table doesn't exist, QueryRow will return an error (sql.ErrNoRows or similar).
	query := `SELECT 1 FROM system.tables WHERE database = ? AND name = ? LIMIT 1`
	// Use uint8 as the target type for scanning SELECT 1, as recommended by the driver error.
	var exists uint8

	// No need for executeQueryWithHooks here, it's a simple metadata check.
	err := c.conn.QueryRow(tableCtx, query, database, table).Scan(&exists)
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
			c.metrics.UpdateConnectionStatus(false)
		}

		if errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("table check timed out", "database", database, "table", table, "timeout", "1s")
			return fmt.Errorf("table check timed out for %s.%s: %w", database, table, err)
		}
		// Check specifically for sql.ErrNoRows which indicates the table doesn't exist.
		// The clickhouse-go driver might wrap this, so checking the string might be necessary
		// if errors.Is(err, sql.ErrNoRows) doesn't work reliably across versions.
		// For now, we rely on the error message in the log.
		if strings.Contains(err.Error(), "no rows in result set") {
			c.logger.Debug("table not found in system.tables", "database", database, "table", table)
			return fmt.Errorf("table '%s.%s' not found: %w", database, table, err)
		} else {
			// Log other scan/query errors.
			c.logger.Debug("table existence check query failed", "database", database, "table", table, "error", err)
			return fmt.Errorf("checking table '%s.%s' failed: %w", database, table, err)
		}
	}

	// If Scan succeeds without error, the table exists.
	if c.metrics != nil {
		c.metrics.RecordConnectionValidation(true)
		c.metrics.UpdateConnectionStatus(true)
	}
	return nil
}
