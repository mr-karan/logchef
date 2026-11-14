package template

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/queryescape"
)

// BuiltinPrefix is reserved for built-in macros. New saved-query and dashboard
// variables may not start with it, which keeps the prefix free for later macros.
const BuiltinPrefix = "__"

// Interval is one of the histogram windows. A macro renders a duration only
// from this table, so no free-form duration reaches a query.
type Interval struct {
	Window     string
	Seconds    int
	clickhouse string
	logsql     string
}

var intervals = []Interval{
	{"1s", 1, "INTERVAL 1 SECOND", "1s"},
	{"5s", 5, "INTERVAL 5 SECOND", "5s"},
	{"10s", 10, "INTERVAL 10 SECOND", "10s"},
	{"15s", 15, "INTERVAL 15 SECOND", "15s"},
	{"30s", 30, "INTERVAL 30 SECOND", "30s"},
	{"1m", 60, "INTERVAL 1 MINUTE", "1m"},
	{"5m", 300, "INTERVAL 5 MINUTE", "5m"},
	{"10m", 600, "INTERVAL 10 MINUTE", "10m"},
	{"15m", 900, "INTERVAL 15 MINUTE", "15m"},
	{"30m", 1800, "INTERVAL 30 MINUTE", "30m"},
	{"1h", 3600, "INTERVAL 1 HOUR", "1h"},
	{"2h", 7200, "INTERVAL 2 HOUR", "2h"},
	{"3h", 10800, "INTERVAL 3 HOUR", "3h"},
	{"6h", 21600, "INTERVAL 6 HOUR", "6h"},
	{"12h", 43200, "INTERVAL 12 HOUR", "12h"},
	{"24h", 86400, "INTERVAL 1 DAY", "1d"},
}

// ParseInterval returns the interval for a histogram window such as "24h".
func ParseInterval(window string) (Interval, bool) {
	for _, interval := range intervals {
		if interval.Window == window {
			return interval, true
		}
	}
	return Interval{}, false
}

// IntervalWindows lists the accepted windows, shortest first.
func IntervalWindows() []string {
	windows := make([]string, len(intervals))
	for i, interval := range intervals {
		windows[i] = interval.Window
	}
	return windows
}

// Builtins are the parsed request values that built-in macros render from.
// A macro never renders request text. A nil or zero field means the request
// did not carry that input, and a macro that needs it fails.
type Builtins struct {
	From, To *time.Time
	// Location is the validated request timezone. Nil means UTC.
	Location *time.Location
	Interval Interval
	// TimestampColumn is the source's timestamp column, for {{__time_filter}}.
	TimestampColumn string
}

// macros is the closed set of built-in names. Each renders its expansion for
// one dialect, or reports why that dialect or request cannot use it.
var macros = map[string]func(Builtins, Dialect) (string, error){
	"__from": func(b Builtins, dialect Dialect) (string, error) {
		return renderInstant("__from", b.From, dialect)
	},
	"__to": func(b Builtins, dialect Dialect) (string, error) {
		return renderInstant("__to", b.To, dialect)
	},
	"__time_filter": func(b Builtins, dialect Dialect) (string, error) {
		if dialect == LogsQL {
			return "", fmt.Errorf("macro {{__time_filter}} is not available in LogsQL: the request's time range already applies")
		}
		from, err := renderInstant("__time_filter", b.From, dialect)
		if err != nil {
			return "", err
		}
		to, err := renderInstant("__time_filter", b.To, dialect)
		if err != nil {
			return "", err
		}
		if b.TimestampColumn == "" {
			return "", fmt.Errorf("macro {{__time_filter}} needs a source with a timestamp column")
		}
		column := queryescape.ClickHouseIdentifier(b.TimestampColumn)
		return column + " >= " + from + " AND " + column + " < " + to, nil
	},
	"__interval": func(b Builtins, dialect Dialect) (string, error) {
		if err := requireInterval("__interval", b); err != nil {
			return "", err
		}
		if dialect == LogsQL {
			return b.Interval.logsql, nil
		}
		return b.Interval.clickhouse, nil
	},
	"__interval_s": func(b Builtins, _ Dialect) (string, error) {
		if err := requireInterval("__interval_s", b); err != nil {
			return "", err
		}
		return strconv.Itoa(b.Interval.Seconds), nil
	},
	"__timezone": func(b Builtins, dialect Dialect) (string, error) {
		if dialect == LogsQL {
			return "", fmt.Errorf("macro {{__timezone}} is not available in LogsQL: use {{__tz_offset}}")
		}
		return formatString(location(b).String(), dialect)
	},
	"__tz_offset": func(b Builtins, dialect Dialect) (string, error) {
		if dialect != LogsQL {
			return "", fmt.Errorf("macro {{__tz_offset}} is not available in ClickHouse SQL: use {{__timezone}}")
		}
		if b.From == nil {
			return "", missingRange("__tz_offset")
		}
		// One fixed offset, read at the start of the range, as the histogram does.
		_, offset := b.From.In(location(b)).Zone()
		return strconv.Itoa(offset) + "s", nil
	},
}

// IsBuiltin reports whether name is a built-in macro.
func IsBuiltin(name string) bool {
	_, ok := macros[name]
	return ok
}

// HasBuiltinPrefix reports whether name starts with the reserved prefix.
func HasBuiltinPrefix(name string) bool {
	return strings.HasPrefix(name, BuiltinPrefix)
}

func location(b Builtins) *time.Location {
	if b.Location == nil {
		return time.UTC
	}
	return b.Location
}

func missingRange(name string) error {
	return fmt.Errorf("macro {{%s}} needs a time range: send start_time and end_time", name)
}

func requireInterval(name string, b Builtins) error {
	if b.Interval.Window == "" {
		return fmt.Errorf("macro {{%s}} needs an interval: send one of %s", name, strings.Join(IntervalWindows(), ", "))
	}
	return nil
}

// renderInstant writes an instant in UTC with a fixed layout.
func renderInstant(name string, instant *time.Time, dialect Dialect) (string, error) {
	if instant == nil {
		return "", missingRange(name)
	}
	utc := instant.UTC()
	if dialect == LogsQL {
		return strconv.Quote(utc.Format(time.RFC3339Nano)), nil
	}
	return "toDateTime64('" + utc.Format("2006-01-02 15:04:05.000") + "', 3, 'UTC')", nil
}

// expandMacro renders a built-in macro found outside comments. quote is the
// quote character of the token that holds it, or 0 in an expression.
func expandMacro(name string, quote byte, builtins Builtins, dialect Dialect) (string, error) {
	if dialect == LogchefQL {
		return "", fmt.Errorf("macro {{%s}} is not available in LogchefQL: the query's time range already applies", name)
	}
	if quote != 0 {
		return "", fmt.Errorf("macro {{%s}} cannot be used inside a string, a quoted identifier, a heredoc or a raw string", name)
	}
	return macros[name](builtins, dialect)
}

func reservedNameError(name string) error {
	return fmt.Errorf("variable name %s is reserved for a built-in macro", name)
}
