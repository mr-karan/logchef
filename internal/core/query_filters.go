package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// Filter pills join a saved query's text the way the Explorer joins them
// (frontend/src/utils/queryFilters.ts), so a report runs what the Explorer
// shows. Only LogchefQL and LogsQL take pills.

var (
	logsqlBareField    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)
	logsqlRegexSpecial = regexp.MustCompile(`[\\.+*?()|\[\]{}^$]`)
	logchefqlOr        = regexp.MustCompile(`(?i)\bor\b`)
	quotedSegment      = regexp.MustCompile(`^"((?:[^"\\]|\\.)*)"$`)
	escapedChar        = regexp.MustCompile(`\\(.)`)
)

// ApplyQueryFilters joins the enabled filters to text with "and", ahead of
// any pipe. Other languages, and queries without filters, are unchanged.
func ApplyQueryFilters(text string, language models.QueryLanguage, filters []models.QueryFilter) string {
	combined := text
	for _, filter := range filters {
		if filter.Disabled || filter.Validate() != nil {
			continue
		}
		switch language {
		case models.QueryLanguageLogchefQL:
			combined = appendLogchefQLCondition(combined, logchefQLFilter(filter))
		case models.QueryLanguageLogsQL:
			combined = appendLogsQLFilter(combined, logsQLFilter(filter))
		default:
			return text
		}
	}
	return combined
}

func quoteFilterString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}

func filterValueText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

// A string is quoted; a number or boolean is written as is.
func filterValueLiteral(value any) string {
	if s, ok := value.(string); ok {
		return quoteFilterString(s)
	}
	return filterValueText(value)
}

func logchefQLFilter(f models.QueryFilter) string {
	cond := func(op string, value any) string { return f.Field + op + filterValueLiteral(value) }
	switch f.Operator {
	case models.QueryFilterIs:
		if f.Negate {
			return cond("!=", f.Values[0])
		}
		return cond("=", f.Values[0])
	case models.QueryFilterContains:
		if f.Negate {
			return cond("!~", f.Values[0])
		}
		return cond("~", f.Values[0])
	case models.QueryFilterExists:
		if f.Negate {
			return f.Field + `=""`
		}
		return f.Field + `!=""`
	case models.QueryFilterIsOneOf:
		terms := make([]string, 0, len(f.Values))
		for _, value := range f.Values {
			if f.Negate {
				terms = append(terms, cond("!=", value))
			} else {
				terms = append(terms, cond("=", value))
			}
		}
		if f.Negate {
			return strings.Join(terms, " and ")
		}
		if len(terms) == 1 {
			return terms[0]
		}
		return "(" + strings.Join(terms, " or ") + ")"
	default:
		// LogchefQL has no "not", so an excluded range takes the opposite comparison.
		ops := map[models.QueryFilterOperator][2]string{
			models.QueryFilterGT: {">", "<="}, models.QueryFilterGTE: {">=", "<"},
			models.QueryFilterLT: {"<", ">="}, models.QueryFilterLTE: {"<=", ">"},
		}[f.Operator]
		if f.Negate {
			return cond(ops[1], f.Values[0])
		}
		return cond(ops[0], f.Values[0])
	}
}

// logsQLField names a field in LogsQL. A single quoted LogchefQL segment is
// the column name, since VictoriaLogs stores nested fields flattened.
func logsQLField(field string) string {
	name := field
	if m := quotedSegment.FindStringSubmatch(field); m != nil {
		name = escapedChar.ReplaceAllString(m[1], "$1")
	}
	if logsqlBareField.MatchString(name) {
		return name
	}
	return quoteFilterString(name)
}

func logsQLFilter(f models.QueryFilter) string {
	name := logsQLField(f.Field)
	not := ""
	if f.Negate {
		not = "NOT "
	}
	switch f.Operator {
	case models.QueryFilterIs:
		return fmt.Sprintf("%s%s:=%s", not, name, filterValueLiteral(f.Values[0]))
	case models.QueryFilterContains:
		pattern := logsqlRegexSpecial.ReplaceAllString(filterValueText(f.Values[0]), `\$0`)
		return fmt.Sprintf("%s%s:~%s", not, name, quoteFilterString(pattern))
	case models.QueryFilterExists:
		return fmt.Sprintf("%s%s:*", not, name)
	case models.QueryFilterIsOneOf:
		values := make([]string, 0, len(f.Values))
		for _, value := range f.Values {
			values = append(values, filterValueLiteral(value))
		}
		return fmt.Sprintf("%s%s:in(%s)", not, name, strings.Join(values, ","))
	default:
		op := map[models.QueryFilterOperator]string{
			models.QueryFilterGT: ">", models.QueryFilterGTE: ">=", models.QueryFilterLT: "<", models.QueryFilterLTE: "<=",
		}[f.Operator]
		return fmt.Sprintf("%s%s:%s%s", not, name, op, filterValueLiteral(f.Values[0]))
	}
}

// firstPipe is the index of the first pipe outside a quoted string, or -1.
func firstPipe(query string, quotes string) int {
	var quote byte
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case strings.IndexByte(quotes, c) >= 0:
			quote = c
		case c == '|':
			return i
		}
	}
	return -1
}

func appendLogchefQLCondition(query, condition string) string {
	where, sel := strings.TrimSpace(query), ""
	if i := firstPipe(query, `"'`); i >= 0 {
		where, sel = strings.TrimSpace(query[:i]), strings.TrimSpace(query[i+1:])
	}
	combined := condition
	if where != "" {
		if logchefqlOr.MatchString(where) {
			where = "(" + where + ")"
		}
		combined = where + " and " + condition
	}
	if sel == "" {
		return combined
	}
	return combined + " | " + sel
}

// logsQLTopLevelOr reports an "or" outside quotes and parentheses.
func logsQLTopLevelOr(filter string) bool {
	var quote byte
	depth := 0
	for i := 0; i < len(filter); i++ {
		c := filter[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && (i == 0 || isSpace(filter[i-1])) && len(filter) > i+2 &&
			strings.EqualFold(filter[i:i+2], "or") && (isSpace(filter[i+2]) || filter[i+2] == '('):
			return true
		}
	}
	return false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func appendLogsQLFilter(query, filter string) string {
	where, pipes := strings.TrimSpace(query), ""
	if i := firstPipe(query, "\"'`"); i >= 0 {
		where, pipes = strings.TrimSpace(query[:i]), strings.TrimSpace(query[i:])
	}
	combined := filter
	if where != "" && where != "*" {
		if logsQLTopLevelOr(where) {
			where = "(" + where + ")"
		}
		combined = where + " " + filter
	}
	if pipes == "" {
		return combined
	}
	return combined + " " + pipes
}
