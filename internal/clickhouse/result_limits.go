package clickhouse

// Result limits: the settings Logchef sends with every query, so no
// ClickHouse limit can end a result without Logchef knowing, and the
// truncation state a result proves from them.

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"

	"github.com/mr-karan/logchef/pkg/models"
)

// overflowModes are the overflow modes of the ClickHouse limits that end a query
// without an error under break or any, except the result mode, whose break
// Logchef owns. Logchef sends each one as throw, so such a limit in a settings
// profile fails the query instead of cutting it. A server that does not know a
// mode fails the query, so since names the first release that has it and its
// limit.
var overflowModes = []overflowMode{
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

type overflowMode struct {
	name  string
	since proto.Version
}

// neutralSettings override the settings profile's row limit, offset and
// result caps, and make a result cap the profile fixes fail the query. Every
// supported server knows them, so a query that runs before Logchef knows the
// server can send them.
func neutralSettings() clickhouse.Settings {
	return clickhouse.Settings{
		"limit":                0,
		"offset":               0,
		"max_result_rows":      0,
		"max_result_bytes":     0,
		"result_overflow_mode": "throw",
	}
}

// resultQuerySettings returns every setting a result query sends. Logchef
// owns the result limits: the query's own row bound, no byte cap and break,
// so a cut at Logchef's bound is one the reader proves. A settings profile
// cannot override them. The source's settings override them, as the
// operator's caps.
func (c *Client) resultQuerySettings(ctx context.Context, opts QueryOptions) (clickhouse.Settings, error) {
	modes, err := c.throwModes.get(ctx)
	if err != nil {
		return nil, err
	}
	settings := clickhouse.Settings{
		"max_result_rows":      opts.ResultRows,
		"max_result_bytes":     0,
		"result_overflow_mode": "break",
	}
	maps.Copy(settings, c.querySettings)
	if c.flattensJSON(ctx) {
		settings[flattenedJSONSetting] = 1
	}
	return c.guarded(settings, modes, opts.TimeoutSeconds), nil
}

// ownQuerySettings returns every setting one of Logchef's own queries sends,
// such as a table's metadata or stats. Logchef reads all of that result, so
// no result cap applies, the source's included.
func (c *Client) ownQuerySettings(ctx context.Context, timeoutSeconds int) (clickhouse.Settings, error) {
	modes, err := c.throwModes.get(ctx)
	if err != nil {
		return nil, err
	}
	settings := clickhouse.Settings{}
	maps.Copy(settings, c.querySettings)
	maps.Copy(settings, neutralSettings())
	return c.guarded(settings, modes, &timeoutSeconds), nil
}

// guarded adds the settings nothing overrides: throw for every overflow mode
// in modes, no row limit, offset or skipped shard, and the query's timeout.
func (c *Client) guarded(settings, modes clickhouse.Settings, timeoutSeconds *int) clickhouse.Settings {
	maps.Copy(settings, modes)
	settings["limit"] = 0
	settings["offset"] = 0
	settings["skip_unavailable_shards"] = 0
	settings["max_execution_time"] = c.queryTimeout(timeoutSeconds)
	return settings
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
	// unsentModes reports an overflow mode the server knows that the query
	// did not send as throw.
	unsentModes bool
}

// truncation is the state of a result that stopped for reason after rows
// rows. The reader proves a row or byte cut. A result may be incomplete when
// its SQL's SETTINGS can end it early, when it left an overflow mode the
// server knows unsent, when it fills the reader's cap with no room for a row
// past it, unless the statement's own LIMIT set the cap, or when a result
// limit it sent in break mode could have ended it.
func (opts QueryOptions) truncation(rows int, reason string, end queryEnd) models.Truncation {
	switch {
	case reason == "byte_limit":
		return models.TruncationBytes
	case reason == "row_limit":
		return models.TruncationRows
	case end.ownSettingsMayCut,
		end.unsentModes,
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
// mode other than throw lets its limit cut the query, a row limit or offset
// cuts it, a skipped shard leaves out its rows, and a result row or byte
// limit cuts in Logchef's break mode unless the same SETTINGS make it throw.
// SQL the parser cannot read, or a setting name Logchef cannot decode, may
// hold any of them, so it may cut.
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
			if clause, ok := node.(*clickhouseparser.SettingsClause); ok && settingsClauseMayCut(clause) {
				mayCut = true
			}
			return !mayCut
		})
	}
	return mayCut
}

func settingsClauseMayCut(clause *clickhouseparser.SettingsClause) bool {
	resultLimit, resultThrows := false, false
	for _, item := range clause.Items {
		name, ok := settingName(item.Name)
		switch {
		case !ok:
			return true
		case strings.Contains(name, "overflow_mode"):
			if value, ok := item.Expr.(*clickhouseparser.StringLiteral); !ok || value.Literal != "throw" {
				return true
			}
			resultThrows = resultThrows || name == "result_overflow_mode"
		case name == "limit", name == "offset", name == "skip_unavailable_shards":
			if !isZero(item.Expr) {
				return true
			}
		case name == "max_result_rows", name == "max_result_bytes":
			resultLimit = resultLimit || !isZero(item.Expr)
		}
	}
	return resultLimit && !resultThrows
}

// settingName returns the lower-cased setting name ClickHouse reads from
// ident, or false when a quoted name holds an escape Logchef does not decode.
// ClickHouse names are lower case, so lower-casing can only match more names.
func settingName(ident *clickhouseparser.Ident) (string, bool) {
	name := ident.Name
	if ident.QuoteType != clickhouseparser.Unquoted {
		decoded, ok := decodeQuotedIdentifier(name)
		if !ok {
			return "", false
		}
		name = decoded
	}
	return strings.ToLower(name), true
}

// isZero reports a setting value that is a literal zero or false.
func isZero(expr clickhouseparser.Expr) bool {
	switch value := expr.(type) {
	case *clickhouseparser.NumberLiteral:
		number, err := strconv.ParseFloat(value.Literal, 64)
		return err == nil && number == 0
	case *clickhouseparser.BoolLiteral:
		return strings.EqualFold(value.Literal, "false")
	}
	return false
}
