package clickhouse

// Server capabilities: what Logchef reads from the server behind a client's
// connections, read again after the driver dials a new connection.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"golang.org/x/sync/singleflight"
)

// capabilityReadTimeout bounds a shared capability read, a dial and one small
// query, apart from the deadline of any caller waiting on it.
const capabilityReadTimeout = 10 * time.Second

const (
	flattenedJSONSetting = "output_format_native_use_flattened_dynamic_and_json_serialization"
	// chExceptionUnknownSetting is ClickHouse's UNKNOWN_SETTING.
	chExceptionUnknownSetting int32 = 115
)

// serverFact is a value read from the server behind a client's connections.
// The driver may dial a new connection that reaches another server, after a
// restart or an address change, so every dial makes the value stale.
// Concurrent lookups of a stale value share one read, and a failed read is not
// kept.
type serverFact[T any] struct {
	// dials counts the connections the client's driver dialed.
	dials *atomic.Uint64
	read  func(context.Context) (T, error)
	fill  singleflight.Group

	mu    sync.Mutex
	value T
	known bool
	// dial is the dial count when the read of value began, so a connection
	// dialed during the read makes it stale.
	dial uint64
}

func (f *serverFact[T]) get(ctx context.Context) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	dial := f.dials.Load()
	f.mu.Lock()
	value, fresh := f.value, f.known && f.dial == dial
	f.mu.Unlock()
	if fresh {
		return value, nil
	}
	// The read keeps the first caller's values but not its cancellation, so
	// a caller that gives up does not fail the others waiting on it.
	result := f.fill.DoChan(strconv.FormatUint(dial, 10), func() (any, error) {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), capabilityReadTimeout)
		defer cancel()
		value, err := f.read(readCtx)
		if err != nil {
			return nil, err
		}
		f.mu.Lock()
		if !f.known || dial >= f.dial {
			f.value, f.known, f.dial = value, true, dial
		}
		f.mu.Unlock()
		return value, nil
	})
	select {
	case read := <-result:
		if read.Err != nil {
			return zero, read.Err
		}
		return read.Val.(T), nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// readThrowModes returns throwModesFor the server's version.
func (c *Client) readThrowModes(ctx context.Context) (clickhouse.Settings, error) {
	var version string
	if err := c.conn.QueryRow(ownQuery(ctx, neutralSettings()), "SELECT version()").Scan(&version); err != nil {
		return nil, fmt.Errorf("reading the ClickHouse server version: %w", err)
	}
	return throwModesFor(version), nil
}

// throwModesFor returns throw for each mode in overflowModes that a server of
// version knows.
func throwModesFor(version string) clickhouse.Settings {
	server := proto.ParseVersion(version)
	modes := make(clickhouse.Settings, len(overflowModes))
	for _, mode := range overflowModes {
		if proto.CheckMinVersion(mode.since, server) {
			modes[mode.name] = "throw"
		}
	}
	return modes
}

// readFlattenedJSON reports whether the server takes the flattened native
// JSON format.
func (c *Client) readFlattenedJSON(ctx context.Context) (bool, error) {
	var supported uint64
	err := c.conn.QueryRow(ownQuery(ctx, neutralSettings()), "SELECT count() FROM system.settings WHERE name = '"+flattenedJSONSetting+"'").Scan(&supported)
	return supported > 0, err
}

// flattensJSON reports whether the server takes the flattened native JSON
// format; older servers must never receive the setting. A failed probe reads
// as false, and the next query probes again.
func (c *Client) flattensJSON(ctx context.Context) bool {
	enabled, err := c.flattenedJSON.get(ctx)
	return err == nil && enabled
}

// send runs a statement with the settings settingsFor returns, and returns
// them. A connection the driver dialed after they were chosen may reach a
// server that does not know one of them, which rejects the statement with
// UNKNOWN_SETTING. When the settings read again leave out one the statement
// sent, a statement that unrun proves did not run is sent once more with
// them. Any other statement fails with an error that says it was not sent
// again: UNKNOWN_SETTING alone does not prove the server rejected it before
// it ran, as throwIf can raise that code from a running INSERT. A setting
// the statement's own SQL names and the server does not know still fails it
// with ClickHouse's error.
func (c *Client) send(ctx context.Context, settingsFor func(context.Context) (clickhouse.Settings, error), unrun func() bool, run func(context.Context) error) (clickhouse.Settings, error) {
	dials := c.dials.Load()
	settings, err := settingsFor(ctx)
	if err != nil {
		return nil, err
	}
	err = run(clickhouse.Context(ctx, clickhouse.WithSettings(settings)))
	if !isUnknownSetting(err) || c.dials.Load() == dials {
		return settings, err
	}
	current, readErr := settingsFor(ctx)
	switch {
	case readErr != nil || !leavesOut(settings, current):
		return settings, err
	case !unrun():
		return settings, fmt.Errorf("a new connection reached a ClickHouse server that does not know a setting Logchef sent, and Logchef did not send the statement again because it may have run: check its effects, then run it again: %w", err)
	}
	return current, run(clickhouse.Context(ctx, clickhouse.WithSettings(current)))
}

// leavesOut reports whether current leaves out a setting that sent has.
func leavesOut(sent, current clickhouse.Settings) bool {
	for name := range sent {
		if _, ok := current[name]; !ok {
			return true
		}
	}
	return false
}

// isSelect reports SQL that parses as one SELECT statement. ClickHouse sends
// a SELECT's header block before it executes the query, so a SELECT that
// fails before the driver returns its rows was not executed, and a SELECT
// changes no data either way.
func isSelect(query string) bool {
	stmts, err := clickhouseparser.NewParser(query).ParseStmts()
	if err != nil || len(stmts) != 1 {
		return false
	}
	_, ok := stmts[0].(*clickhouseparser.SelectQuery)
	return ok
}

// modesUnsent reports whether a query that left out an overflow mode, as one
// its server did not know, ran on a server that knows it: the driver may have
// dialed a newer server after the modes were read. That mode's limit could
// then cut the result. Call it after the query released its rows: reading
// the modes again takes a pool connection, and queries that wait for one
// while they hold theirs can take the whole pool.
func (c *Client) modesUnsent(ctx context.Context, settings clickhouse.Settings) bool {
	if !slices.ContainsFunc(overflowModes, func(mode overflowMode) bool { return settings[mode.name] == nil }) {
		return false
	}
	modes, err := c.throwModes.get(ctx)
	if err != nil {
		return true
	}
	for name := range modes {
		if settings[name] == nil {
			return true
		}
	}
	return false
}

func isUnknownSetting(err error) bool {
	exception, ok := errors.AsType[*clickhouse.Exception](err)
	return ok && exception.Code == chExceptionUnknownSetting
}
