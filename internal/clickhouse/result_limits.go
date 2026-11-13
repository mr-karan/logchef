package clickhouse

// Result limits: the settings Logchef sends with every result query, so no
// ClickHouse limit can end a result without Logchef knowing, and the
// truncation state a result proves from them.

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"

	"github.com/mr-karan/logchef/pkg/models"
)

// throwModes are the overflow modes of the ClickHouse limits that end a query
// without an error under break or any. Logchef sends each one as throw, so
// such a limit in a settings profile fails the query instead of cutting it.
// A server that does not know a mode fails the query, so since names the
// first release that has it and its limit.
var throwModes = []struct {
	name  string
	since proto.Version
}{
	{name: "read_overflow_mode"},
	{name: "read_overflow_mode_leaf"},
	{name: "group_by_overflow_mode"},
	{name: "sort_overflow_mode"},
	{name: "distinct_overflow_mode"},
	{name: "set_overflow_mode"},
	{name: "join_overflow_mode"},
	{name: "transfer_overflow_mode"},
	{name: "timeout_overflow_mode"},
	{name: "timeout_overflow_mode_leaf", since: proto.Version{Major: 23, Minor: 11}},
}

// throwModeSettings returns throw for each mode in throwModes that the server
// knows, from its version, read once per client. The read runs on the
// caller's context: the driver's ServerVersion dials without it, so a server
// that never answers would hold the query for the whole dial timeout. A failed
// read fails the query.
func (c *Client) throwModeSettings(ctx context.Context) (clickhouse.Settings, error) {
	c.capabilitiesMu.Lock()
	modes := c.throwModes
	c.capabilitiesMu.Unlock()
	if modes != nil {
		return modes, nil
	}
	var version string
	if err := c.conn.QueryRow(ownQuery(ctx, nil), "SELECT version()").Scan(&version); err != nil {
		return nil, fmt.Errorf("reading the ClickHouse server version: %w", err)
	}
	server := proto.ParseVersion(version)
	modes = make(clickhouse.Settings, len(throwModes))
	for _, mode := range throwModes {
		if proto.CheckMinVersion(mode.since, server) {
			modes[mode.name] = "throw"
		}
	}
	c.capabilitiesMu.Lock()
	c.throwModes = modes
	c.capabilitiesMu.Unlock()
	return modes, nil
}

// resultQuerySettings returns every setting a result query sends. Logchef
// owns the result limits: the query's own row bound, no byte cap and break,
// so a cut at Logchef's bound is one the reader proves, and limit and offset
// off. A settings profile cannot override them. The source's settings
// override them, as the operator's caps, and nothing overrides the throw
// modes or the timeout.
func (c *Client) resultQuerySettings(ctx context.Context, opts QueryOptions) (clickhouse.Settings, error) {
	modes, err := c.throwModeSettings(ctx)
	if err != nil {
		return nil, err
	}
	settings := clickhouse.Settings{
		"max_result_rows":      opts.ResultRows,
		"max_result_bytes":     0,
		"result_overflow_mode": "break",
		"limit":                0,
		"offset":               0,
	}
	maps.Copy(settings, c.querySettings)
	maps.Copy(settings, modes)
	settings["max_execution_time"] = c.queryTimeout(opts.TimeoutSeconds)
	return settings, nil
}

// queryTimeout is the max_execution_time of a query: the request's, or the
// default, capped by the source's.
func (c *Client) queryTimeout(requested *int) int {
	timeout := DefaultQueryTimeout
	if requested != nil {
		timeout = *requested
	}
	if limit := settingInt(c.querySettings["max_execution_time"]); limit > 0 && limit < int64(timeout) {
		timeout = int(limit)
	}
	return timeout
}

// withQueryTimeout ends ctx when the query's timeout and its grace pass.
// The driver does not see the deadline: clickhouse-go replaces the query's
// max_execution_time with a context deadline plus five seconds.
func withQueryTimeout(ctx context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second+queryTimeoutGrace)
	return withoutDeadline{ctx}, cancel
}

type withoutDeadline struct{ context.Context } //nolint:containedctx // it wraps the query context to hide its deadline

func (withoutDeadline) Deadline() (time.Time, bool) { return time.Time{}, false }

func settingInt(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// queryEnd is what Logchef knows when a result query ends.
type queryEnd struct {
	// settings are the settings the query sent.
	settings clickhouse.Settings
	// resultBytes sums the server's result profile, when profiled says one arrived.
	resultBytes uint64
	profiled    bool
	// ownSettingsMayCut reports SETTINGS in the SQL that can end it early.
	ownSettingsMayCut bool
}

// truncation is the state of a result that stopped for reason after rows
// rows. The reader proves a row or byte cut. A result may be incomplete when
// its SQL's SETTINGS can end it early, when it fills the reader's cap with no
// room for a row past it, unless the statement's own LIMIT set the cap, or
// when a result limit it sent in break mode could have ended it.
func (opts QueryOptions) truncation(rows int, reason string, end queryEnd) models.Truncation {
	switch {
	case reason == "byte_limit":
		return models.TruncationBytes
	case reason == "row_limit":
		return models.TruncationRows
	case end.ownSettingsMayCut,
		opts.capFilled(rows),
		end.resultLimitMayHaveCut(rows, opts.LimitSource):
		return models.TruncationUnknown
	}
	return models.TruncationNone
}

// capFilled reports a result that filled the reader's cap when its row bound
// left no room for a row past the cap to prove a cut.
func (opts QueryOptions) capFilled(rows int) bool {
	probed := opts.ResultRows == 0 || opts.ResultRows > opts.MaxRows
	return !probed && opts.MaxRows > 0 && rows >= opts.MaxRows && opts.LimitSource != models.LimitSourceStatement
}

// resultLimitMayHaveCut reports whether a result limit the query sent in break
// mode could have ended it: a row limit the result filled, unless the
// statement's own LIMIT set it, or a byte limit the server's result profile
// reached or did not report. The profile counts allocated bytes, never fewer
// than max_result_bytes counts, so a profile below the limit proves it did
// not cut.
func (end queryEnd) resultLimitMayHaveCut(rows int, source models.LimitSource) bool {
	if end.settings["result_overflow_mode"] != "break" {
		return false
	}
	if limit := settingInt(end.settings["max_result_rows"]); limit > 0 && int64(rows) >= limit && source != models.LimitSourceStatement {
		return true
	}
	limit := settingInt(end.settings["max_result_bytes"])
	return limit > 0 && (!end.profiled || end.resultBytes >= uint64(limit))
}

// SettingsMayCut reports whether a query's own SETTINGS can end it early
// without an error. They override every setting Logchef sends. An overflow
// mode other than throw lets its limit cut the query, and a result row or
// byte limit cuts in Logchef's break mode unless the same SETTINGS make it
// throw. SQL the parser cannot read may hold either, so it may cut.
func SettingsMayCut(query string) bool {
	if !strings.Contains(strings.ToLower(query), "settings") {
		return false
	}
	stmts, err := clickhouseparser.NewParser(query).ParseStmts()
	if err != nil {
		return true
	}
	mayCut := false
	for _, stmt := range stmts {
		clickhouseparser.Walk(stmt, func(node clickhouseparser.Expr) bool {
			if clause, ok := node.(*clickhouseparser.SettingsClause); ok {
				mayCut = settingsClauseMayCut(clause)
			}
			return !mayCut
		})
	}
	return mayCut
}

func settingsClauseMayCut(clause *clickhouseparser.SettingsClause) bool {
	resultLimit, resultThrows := false, false
	for _, item := range clause.Items {
		name := strings.ToLower(item.Name.Name)
		switch {
		case strings.HasSuffix(name, "_overflow_mode"):
			if value, ok := item.Expr.(*clickhouseparser.StringLiteral); !ok || value.Literal != "throw" {
				return true
			}
			resultThrows = resultThrows || name == "result_overflow_mode"
		case name == "max_result_rows", name == "max_result_bytes":
			resultLimit = true
		}
	}
	return resultLimit && !resultThrows
}
