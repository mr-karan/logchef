package logchefql

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mr-karan/logchef/internal/template"
)

// AggFunc is one of the eleven aggregation functions.
type AggFunc string

const (
	AggCount         AggFunc = "count"
	AggCountDistinct AggFunc = "count_distinct"
	AggSum           AggFunc = "sum"
	AggAvg           AggFunc = "avg"
	AggMin           AggFunc = "min"
	AggMax           AggFunc = "max"
	AggP50           AggFunc = "p50"
	AggP75           AggFunc = "p75"
	AggP90           AggFunc = "p90"
	AggP95           AggFunc = "p95"
	AggP99           AggFunc = "p99"
)

var aggFuncs = []AggFunc{AggCount, AggCountDistinct, AggSum, AggAvg, AggMin, AggMax, AggP50, AggP75, AggP90, AggP95, AggP99}

// Aggregate is one value of an aggregation, such as count() or p95(duration).
type Aggregate struct {
	Func  AggFunc `json:"func"`
	Field any     `json:"field,omitempty"` // string or NestedField. nil for count()
	Name  string  `json:"name"`            // output column name, from "as" or the default rule

	pos     Position
	aliased bool
	alias   Position
}

// GroupKey is one `by` key: the time bucket or a field.
type GroupKey struct {
	Time     bool   `json:"time,omitempty"`     // the time bucket key
	Interval string `json:"interval,omitempty"` // "5m" for time(5m). "" means the request interval
	Field    any    `json:"field,omitempty"`    // string or NestedField. nil when Time is true
	Name     string `json:"name"`               // output column name

	pos     Position
	aliased bool
	alias   Position
}

// SortKey orders the result by one output column.
type SortKey struct {
	Name string `json:"name"`
	Desc bool   `json:"desc,omitempty"`

	pos Position
}

// AggregationNode is the aggregation pipe of a query.
type AggregationNode struct {
	Aggregates []Aggregate `json:"aggregates"`     // 1 to 10
	By         []GroupKey  `json:"by,omitempty"`   // 0 to 5, at most one with Time
	Sort       *SortKey    `json:"sort,omitempty"` // nil means the default order
	Limit      int         `json:"limit,omitempty"`

	pos      Position
	limitRaw string
	limitPos Position
}

// ResultRole says what a result column holds.
type ResultRole string

const (
	RoleTime  ResultRole = "time"
	RoleGroup ResultRole = "group"
	RoleValue ResultRole = "value"
)

// ResultColumn describes one column of an aggregated result.
type ResultColumn struct {
	Name string     `json:"name"`
	Role ResultRole `json:"role"`
	// Type is the column type to report when the backend reports none.
	// "DateTime" for time, "String" for group, "UInt64" for count and
	// count_distinct, "Float64" for every other value.
	Type string `json:"type"`
	// Source is the field that the backend returns the column under. It
	// differs from Name on VictoriaLogs only.
	Source string `json:"-"`
}

// AggregationShape is the row layout that the `by` keys produce.
type AggregationShape string

const (
	ShapeScalar            AggregationShape = "scalar"
	ShapeGrouped           AggregationShape = "grouped"
	ShapeTimeseries        AggregationShape = "timeseries"
	ShapeGroupedTimeseries AggregationShape = "grouped_timeseries"
)

// AggregationInfo describes the result of a compiled aggregation. It is nil
// for a query that returns log rows.
type AggregationInfo struct {
	Shape AggregationShape `json:"shape"`
	// Columns lists time first, then group keys, then values, each in
	// written order.
	Columns []ResultColumn `json:"columns"`
	// Interval is the bucket window in use, such as "5m". "" without a time key.
	Interval string `json:"interval"`
	// Limit is the query's own limit, or 0.
	Limit int `json:"limit"`
}

const (
	maxAggregates  = 10
	maxGroupKeys   = 5
	maxResultLimit = 10000
	reservedPrefix = "__lc_"
)

var aliasPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

var nonNameChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// Info describes the result of the aggregation. requestInterval is the
// interval of the request, used by a time key that has no window of its own.
func (n *AggregationNode) Info(requestInterval string) *AggregationInfo {
	info := &AggregationInfo{Limit: n.Limit}
	fields := 0
	for _, key := range n.By {
		if key.Time {
			info.Interval = key.Interval
			if info.Interval == "" {
				info.Interval = requestInterval
			}
			info.Columns = append(info.Columns, ResultColumn{Name: key.Name, Role: RoleTime, Type: "DateTime", Source: key.Name})
		}
	}
	for _, key := range n.By {
		if !key.Time {
			fields++
			info.Columns = append(info.Columns, ResultColumn{Name: key.Name, Role: RoleGroup, Type: "String", Source: key.Name})
		}
	}
	for _, agg := range n.Aggregates {
		valueType := "Float64"
		if agg.Func == AggCount || agg.Func == AggCountDistinct {
			valueType = "UInt64"
		}
		info.Columns = append(info.Columns, ResultColumn{Name: agg.Name, Role: RoleValue, Type: valueType, Source: agg.Name})
	}
	hasTime := len(n.By) > fields
	switch {
	case hasTime && fields > 0:
		info.Shape = ShapeGroupedTimeseries
	case hasTime:
		info.Shape = ShapeTimeseries
	case fields > 0:
		info.Shape = ShapeGrouped
	default:
		info.Shape = ShapeScalar
	}
	return info
}

// columnNames lists the output names in result order.
func (n *AggregationNode) columnNames() []string {
	info := n.Info("")
	names := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		names[i] = col.Name
	}
	return names
}

func toPosition(pos lexer.Position) Position {
	return Position{Line: pos.Line, Column: pos.Column}
}

func convertAggregate(pa *PAggregate) *AggregationNode {
	node := &AggregationNode{pos: toPosition(pa.Pos)}
	for _, item := range pa.Items {
		agg := Aggregate{Func: AggFunc(strings.ToLower(item.Func)), pos: toPosition(item.Pos)}
		if item.Field != nil {
			agg.Field = convertFieldPath(item.Field)
		}
		agg.Name = defaultAggregateName(agg)
		if item.Alias != nil {
			agg.Name, agg.aliased, agg.alias = item.Alias.Name, true, toPosition(item.Alias.Pos)
		}
		node.Aggregates = append(node.Aggregates, agg)
	}
	for _, pk := range pa.By {
		key := GroupKey{pos: toPosition(pk.Pos)}
		if pk.Time {
			key.Time, key.Name = true, "time"
			if pk.Window != nil {
				key.Interval = windowText(pk.Window)
			}
		} else {
			key.Field = convertFieldPath(pk.Field)
			key.Name = getFieldName(key.Field)
		}
		if pk.Alias != nil {
			key.Name, key.aliased, key.alias = pk.Alias.Name, true, toPosition(pk.Alias.Pos)
		}
		node.By = append(node.By, key)
	}
	if pa.Sort != nil {
		node.Sort = &SortKey{
			Name: getFieldName(convertFieldPath(pa.Sort.Field)),
			Desc: strings.EqualFold(pa.Sort.Order, "desc"),
			pos:  toPosition(pa.Sort.Pos),
		}
	}
	if pa.Limit != nil {
		node.limitRaw, node.limitPos = pa.Limit.Value, toPosition(pa.Limit.Pos)
		if n, err := strconv.Atoi(pa.Limit.Value); err == nil {
			node.Limit = n
		}
	}
	return node
}

// windowText joins the Number and Ident tokens of a window. A space between
// them stays in the text, so "5 m" fails the window table.
func windowText(w *PWindow) string {
	if w.Unit.Pos.Offset == w.Pos.Offset+len(w.Number) {
		return w.Number + w.Unit.Value
	}
	return w.Number + " " + w.Unit.Value
}

func defaultAggregateName(agg Aggregate) string {
	if agg.Field == nil {
		return string(agg.Func)
	}
	return string(agg.Func) + "_" + nonNameChar.ReplaceAllString(getFieldName(agg.Field), "_")
}

func aggregationError(pos Position, format string, args ...any) *ParseError {
	return &ParseError{Code: ErrInvalidAggregation, Message: fmt.Sprintf(format, args...), Position: &pos}
}

// validateAggregation checks the rules that the grammar cannot express.
func validateAggregation(n *AggregationNode) *ParseError {
	if len(n.Aggregates) > maxAggregates {
		return aggregationError(n.Aggregates[maxAggregates].pos, "a query can have at most %d aggregations", maxAggregates)
	}
	for _, agg := range n.Aggregates {
		if err := validateAggregate(agg); err != nil {
			return err
		}
	}

	timeKeys, fieldKeys, err := validateGroupKeys(n.By)
	if err != nil {
		return err
	}

	names := n.columnNames()
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, reservedPrefix) {
			return aggregationError(n.pos, "the name %q starts with %q, which is reserved", name, reservedPrefix)
		}
		if seen[name] {
			return aggregationError(n.pos, "two columns are named %q. Name one with \"as\"", name)
		}
		seen[name] = true
	}

	if n.Sort != nil && !seen[n.Sort.Name] {
		return aggregationError(n.Sort.pos, "cannot sort by %q. Columns are %s", n.Sort.Name, strings.Join(names, ", "))
	}
	if n.limitRaw != "" {
		if n.Limit < 1 || n.Limit > maxResultLimit || strconv.Itoa(n.Limit) != n.limitRaw {
			return aggregationError(n.limitPos, "limit must be between 1 and %d", maxResultLimit)
		}
		if timeKeys > 0 && fieldKeys > 0 {
			return aggregationError(n.limitPos, "limit cannot be used with time and a field, because it would cut buckets and not series. Add a filter to reduce the series")
		}
	}
	return nil
}

// validateGroupKeys checks the `by` keys and counts the time and field keys.
func validateGroupKeys(keys []GroupKey) (timeKeys, fieldKeys int, err *ParseError) {
	if len(keys) > maxGroupKeys {
		return 0, 0, aggregationError(keys[maxGroupKeys].pos, "a query can group by at most %d keys", maxGroupKeys)
	}
	grouped := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key.aliased && !aliasPattern.MatchString(key.Name) {
			return 0, 0, aggregationError(key.alias, "invalid name %q", key.Name)
		}
		if key.Time {
			timeKeys++
			if timeKeys > 1 {
				return 0, 0, aggregationError(key.pos, "a query can have at most one time key")
			}
			if key.Interval != "" {
				if _, ok := template.ParseInterval(key.Interval); !ok {
					return 0, 0, aggregationError(key.pos, "invalid interval %q: use one of %s", key.Interval, strings.Join(template.IntervalWindows(), ", "))
				}
			}
			continue
		}
		fieldKeys++
		field := getFieldName(key.Field)
		if err := checkFieldName(key.pos, field); err != nil {
			return 0, 0, err
		}
		if grouped[field] {
			return 0, 0, aggregationError(key.pos, "%q is grouped twice", field)
		}
		grouped[field] = true
	}
	return timeKeys, fieldKeys, nil
}

func validateAggregate(agg Aggregate) *ParseError {
	if !slices.Contains(aggFuncs, agg.Func) {
		names := make([]string, len(aggFuncs))
		for i, f := range aggFuncs {
			names[i] = string(f)
		}
		return aggregationError(agg.pos, "unknown function %q. Use %s or %s", string(agg.Func), strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
	}
	switch {
	case agg.Func == AggCount && agg.Field != nil:
		return aggregationError(agg.pos, "count takes no field. Write count()")
	case agg.Func == AggCountDistinct && agg.Field == nil:
		return aggregationError(agg.pos, "count_distinct needs a field, as in count_distinct(user_id)")
	case agg.Func != AggCount && agg.Field == nil:
		return aggregationError(agg.pos, "%s needs a field, as in %s(bytes)", agg.Func, agg.Func)
	}
	if agg.Field != nil {
		if err := checkFieldName(agg.pos, getFieldName(agg.Field)); err != nil {
			return err
		}
	}
	if agg.aliased && !aliasPattern.MatchString(agg.Name) {
		return aggregationError(agg.alias, "invalid name %q", agg.Name)
	}
	return nil
}

func checkFieldName(pos Position, field string) *ParseError {
	if strings.HasPrefix(field, reservedPrefix) {
		return aggregationError(pos, "the field %q starts with %q, which is reserved", field, reservedPrefix)
	}
	return nil
}

// AggregationOf returns the aggregation of a parsed query, or nil.
func AggregationOf(ast ASTNode) *AggregationNode {
	if q, ok := ast.(*QueryNode); ok {
		return q.Aggregate
	}
	return nil
}

// parseQuery parses a non-empty query into the AST and applies the rules
// that the grammar cannot express.
func parseQuery(query string) (ASTNode, *ParseError) {
	pq, err := ParseLogchefQL(query)
	if err != nil {
		return nil, convertParticipleError(query, err)
	}
	ast := ConvertToAST(pq)
	if agg := AggregationOf(ast); agg != nil {
		if perr := validateAggregation(agg); perr != nil {
			return nil, perr
		}
	}
	return ast, nil
}

// checkAggregationAllowed is the default deny: a caller that did not opt in
// needs log rows, and an aggregation would run wrong or be ignored there.
func checkAggregationAllowed(ast ASTNode, allow bool) *ParseError {
	agg := AggregationOf(ast)
	if agg == nil || allow {
		return nil
	}
	pos := agg.pos
	return &ParseError{
		Code:     ErrAggregationNotAllowed,
		Message:  "this feature needs log rows, and the query aggregates. Remove the pipe after the filter",
		Position: &pos,
	}
}
