package clickhouse

// Server capabilities: what Logchef reads from the server behind a client's
// connections, read again after the driver dials a new connection.
//
// A statement's settings come from the last read, so a statement that the
// driver sends over a connection it dialed to an older server after that read
// can fail with UNKNOWN_SETTING. That statement fails once, with an error that
// says the server changed version, and the next statement gets settings read
// from the new server. Logchef never sends a statement twice: ClickHouse can
// raise UNKNOWN_SETTING after a statement had effects, even a SELECT, whose
// analysis runs functions.
//
// Logchef assumes one ClickHouse version behind a source's address at a time.
// The driver does not say which connection, or which server, ran a query, so
// a capability read and a query may reach different servers. With two
// versions behind one address at once, as during a rolling upgrade behind a
// load balancer, every query still sends limit, offset and
// skip_unavailable_shards as 0, Logchef's result limits, and throw for every
// overflow mode that all supported versions know. Only the settings a version
// may lack follow the last read: timeout_overflow_mode_leaf, from 23.11, and
// the flattened JSON format. A query that sends one of them to a server
// without it can fail with UNKNOWN_SETTING. A query that leaves out
// timeout_overflow_mode_leaf, because the last read reached an older server,
// can be cut on a newer server whose settings profile sets that mode to
// break, and its truncation can still read none.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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

// send runs a statement once with the settings settingsFor returns, and
// returns them. A connection the driver dialed after they were chosen may
// reach a server of another version that does not know one of them. When the
// statement then fails with UNKNOWN_SETTING and the settings read again for
// that server leave out one it sent, it fails with a QueryError that says the
// server changed version. That read replaces the capabilities the dial made
// stale. A setting the statement's own SQL names and the server does not know
// still fails it with ClickHouse's error.
func (c *Client) send(ctx context.Context, settingsFor func(context.Context) (clickhouse.Settings, error), run func(context.Context) error) (clickhouse.Settings, error) {
	dials := c.dials.Load()
	settings, err := settingsFor(ctx)
	if err != nil {
		return nil, err
	}
	err = run(clickhouse.Context(ctx, clickhouse.WithSettings(settings)))
	if !isUnknownSetting(err) || c.dials.Load() == dials {
		return settings, err
	}
	if current, readErr := settingsFor(ctx); readErr == nil && leavesOut(settings, current) {
		return settings, &QueryError{Message: "The ClickHouse server changed version during the query and does not know a setting Logchef sent. Run the query again", Err: err}
	}
	return settings, err
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
