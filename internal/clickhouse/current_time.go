package clickhouse

import (
	"strings"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// currentTimeFunctions are the ClickHouse functions, and their aliases, whose
// value is the time the query runs, lower case.
var currentTimeFunctions = map[string]struct{}{
	"now":               {},
	"now64":             {},
	"nowinblock":        {},
	"nowinblock64":      {},
	"current_timestamp": {},
	"localtimestamp":    {},
	"utctimestamp":      {},
	"utc_timestamp":     {},
	"today":             {},
	"curdate":           {},
	"current_date":      {},
	"yesterday":         {},
	// The start of the query, or of the distributed query that sent it.
	"initialquerystarttime":    {},
	"initial_query_start_time": {},
}

// ReadsCurrentTime reports whether a SELECT statement calls a function whose
// value is the time it runs, such as now(), anywhere in the statement. Such a
// query cannot be run for a past instant.
func ReadsCurrentTime(query string) (bool, error) {
	selectQuery, err := ParseSelectQuery(query)
	if err != nil {
		return false, err
	}
	found := false
	clickhouseparser.Walk(selectQuery, func(node clickhouseparser.Expr) bool {
		if call, ok := node.(*clickhouseparser.FunctionExpr); ok && call.Name != nil {
			_, found = currentTimeFunctions[strings.ToLower(call.Name.Name)]
		}
		return !found
	})
	return found, nil
}
