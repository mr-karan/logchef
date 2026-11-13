package clickhouse

import (
	"errors"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// QueryError carries a query diagnostic without the driver's error wrapper.
type QueryError struct {
	Message string
	Err     error
}

func (e *QueryError) Error() string { return e.Message }
func (e *QueryError) Unwrap() error { return e.Err }

func publicQueryError(err error) error {
	exception, ok := errors.AsType[*clickhouse.Exception](err)
	if !ok {
		return err
	}
	// Only query diagnostics from ClickHouse's src/Common/ErrorCodes.cpp are public.
	switch exception.Code {
	case 16, 34, 35, 36, 42, 43, 46, 47, 50, 53, 59, 60, 62, 63,
		69, 70, 80, 81, 158, 184, 191, 215, 307, 386, 396:
		message := exception.Message
		for strings.HasPrefix(message, "Received from ") {
			_, diagnostic, found := strings.Cut(message, ". DB::Exception: ")
			if !found {
				return err
			}
			message = diagnostic
		}
		message, _, _ = strings.Cut(message, "Stack trace:")
		message, _, _ = strings.Cut(message, "While executing ")
		message = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(message), ":"))
		// Shard addresses only arrive through the "Received from" prefix and schema path errors name a server directory; other text echoes the user's query.
		if message == "" || strings.Contains(message, "Received from ") || strings.Contains(message, "format_schema_path") {
			return err
		}
		return &QueryError{Message: limitMessage(message), Err: err}
	default:
		return err
	}
}

// limitSettings name the settings behind each ClickHouse limit message.
// ClickHouse reports a sort limit as a result limit.
var limitSettings = []struct{ prefix, settings string }{
	{"Limit for rows (controlled by ", ""},
	{"Limit for rows or bytes to read on leaf node", "max_rows_to_read_leaf or max_bytes_to_read_leaf"},
	{"Limit for rows or bytes to read", "max_rows_to_read or max_bytes_to_read"},
	{"Limit for rows to GROUP BY", "max_rows_to_group_by"},
	{"Limit for DISTINCT", "max_rows_in_distinct or max_bytes_in_distinct"},
	{"Limit for JOIN", "max_rows_in_join or max_bytes_in_join"},
	{"Limit for IN-set", "max_rows_in_set or max_bytes_in_set"},
	{"Limit for result", "max_result_rows, max_result_bytes, max_rows_to_sort or max_bytes_to_sort"},
}

// limitMessage explains a ClickHouse limit error: Logchef makes every limit
// fail the query rather than return part of its result.
func limitMessage(message string) string {
	if !strings.HasPrefix(message, "Limit for ") {
		return message
	}
	limit := "this limit"
	for _, known := range limitSettings {
		if strings.HasPrefix(message, known.prefix) {
			if known.settings != "" {
				limit = known.settings
			}
			break
		}
	}
	return message + ". Logchef has ClickHouse fail a query at " + limit + " rather than return part of the result. Raise the limit in the source settings or the ClickHouse user's settings profile, or narrow the query"
}
