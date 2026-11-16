package reports

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// This file ports the stat path of frontend/src/utils/panelFrame.ts: column
// kinds, cell reading and reduceStat, without the comparison and the
// sparkline, which a report does not show.

// Tile is a rendered stat panel.
type Tile struct {
	// Value is the formatted value, or "" when the stat has no data.
	Value string
	// RowLabel names the row a first or last reducer selected.
	RowLabel string
	// Rows and Truncation describe the result the stat reduced; a cut
	// result prints its label, since a sum over it is wrong without one.
	Rows       int
	Truncation models.Truncation
}

// StatOptions is a stat panel's viz.
type StatOptions struct {
	Value   string
	Reduce  models.DashboardStatReduce
	OrderBy string
	Format  models.DashboardNumberFormat
	// Columns holds confirmed column kinds by name.
	Columns map[string]string
	// Untyped ignores the backend types: native LogsQL reports none.
	Untyped bool
}

type frameColumn struct {
	name        string
	backendType string
	kind        string
	repr        string
}

var (
	wrapperType   = regexp.MustCompile(`(?:Nullable|LowCardinality)\(`)
	closingParens = regexp.MustCompile(`\)+$`)
	smallInt      = regexp.MustCompile(`^U?Int(?:8|16|32)$`)
	wideInt       = regexp.MustCompile(`^U?Int(?:64|128|256)$`)
	decimalScale  = regexp.MustCompile(`Decimal(?:\(\d+,\s*|(?:32|64|128|256)\(\s*)(\d+)`)
	rfc3339       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$`)
	dateOnly      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	hasZone       = regexp.MustCompile(`Z$|[+-]\d\d:\d\d$`)
	fractionPart  = regexp.MustCompile(`\.(\d+)`)
)

func declaredColumn(name, backendType string) frameColumn {
	t := closingParens.ReplaceAllString(wrapperType.ReplaceAllString(backendType, ""), "")
	c := frameColumn{name: name, backendType: backendType, kind: "category"}
	switch {
	case t == "Date" || t == "Date32":
		c.kind = "date"
	case strings.HasPrefix(t, "DateTime"):
		c.kind = "time"
	case smallInt.MatchString(t):
		c.kind, c.repr = "number", "int"
	case wideInt.MatchString(t):
		c.kind, c.repr = "number", "bigint"
	case strings.HasPrefix(t, "Decimal"):
		c.kind, c.repr = "number", "decimal"
	case strings.HasPrefix(t, "Float"):
		c.kind, c.repr = "number", "float"
	case t == "Bool":
		c.kind = "bool"
	case strings.HasPrefix(t, "Array") || strings.HasPrefix(t, "Map") || strings.HasPrefix(t, "Tuple") ||
		strings.HasPrefix(t, "JSON") || strings.HasPrefix(t, "Object") || strings.HasPrefix(t, "Nested"):
		c.kind = "other"
	}
	return c
}

// frameColumns types the result columns, adds a column that only rows
// carry, and applies the confirmed kinds.
func frameColumns(res *models.QueryResult, confirmed map[string]string, untyped bool) []frameColumn {
	seen := make(map[string]bool, len(res.Columns))
	var cols []frameColumn
	add := func(name, backendType string) {
		seen[name] = true
		c := declaredColumn(name, backendType)
		if kind, ok := confirmed[name]; ok {
			c.kind, c.repr = kind, ""
			if kind == "number" {
				c.repr = cmp.Or(declaredColumn(name, backendType).repr, "decimal")
			}
		}
		cols = append(cols, c)
	}
	for _, c := range res.Columns {
		backendType := c.Type
		if untyped {
			backendType = ""
		}
		add(c.Name, backendType)
	}
	for _, row := range res.Logs {
		var extra []string
		for name := range row {
			if !seen[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			add(name, "")
		}
	}
	return cols
}

// jsonValue is a driver value as the frontend receives it in JSON.
func jsonValue(v any) any {
	if t, ok := v.(time.Time); ok {
		return t.Format(time.RFC3339Nano)
	}
	return v
}

// jsonFloat is the number the frontend reads from the JSON form of a
// driver number: a float32 encodes with its own shortest digits.
func jsonFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case float32:
		f, err := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'g', -1, 32), 64)
		return f, err == nil
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f, err == nil
	}
	return 0, false
}

// numberLiteral is the decimal text the frontend parses: a string, or the
// JSON form of a driver number.
func numberLiteral(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v), true
	}
	return "", false
}

// readCell is readCell of panelFrame.ts: nil for an invalid or empty cell.
func readCell(v any, c frameColumn) any {
	v = jsonValue(v)
	if v == nil {
		return nil
	}
	if c.kind == "category" || c.kind == "other" {
		return v
	}
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	switch c.kind {
	case "number":
		return readNumber(v, c)
	case "bool":
		switch v {
		case true, "true":
			return true
		case false, "false":
			return false
		}
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if c.kind == "date" {
		return readDate(s, c)
	}
	return readTime(s, c)
}

func readNumber(v any, c frameColumn) any {
	if c.repr == "float" {
		if f, ok := jsonFloat(v); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return floatNumber(f)
		}
		return nil
	}
	literal, ok := numberLiteral(v)
	if !ok {
		return nil
	}
	if e, ok := parseExact(literal); ok {
		return exactNumber(e)
	}
	return nil
}

func readDate(s string, c frameColumn) any {
	if declaredColumn(c.name, c.backendType).kind == "date" && len(s) >= 10 {
		s = s[:10]
	}
	if validDate(s) {
		return s
	}
	return nil
}

// readTime reads an instant. A ClickHouse DateTime without a zone is UTC.
func readTime(s string, c frameColumn) any {
	ts := s
	if strings.Contains(c.backendType, "DateTime") {
		ts = strings.Replace(s, " ", "T", 1)
		if !hasZone.MatchString(s) {
			ts += "Z"
		}
	}
	if !rfc3339.MatchString(ts) || !validDate(ts[:10]) {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		return nil
	}
	return ts
}

func validDate(s string) bool {
	if !dateOnly.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// compareCells orders two read cells of one column.
func compareCells(a, b any, c frameColumn) int {
	an, aNum := a.(Number)
	bn, bNum := b.(Number)
	switch {
	case aNum && bNum && !an.isFloat && !bn.isFloat:
		return an.exact.cmp(bn.exact)
	case aNum && bNum && an.isFloat && bn.isFloat:
		return cmp.Compare(an.float, bn.float)
	case c.kind == "time":
		as, bs := fmt.Sprint(a), fmt.Sprint(b)
		at, _ := time.Parse(time.RFC3339Nano, as)
		bt, _ := time.Parse(time.RFC3339Nano, bs)
		if d := cmp.Compare(at.UnixMilli(), bt.UnixMilli()); d != 0 {
			return d
		}
		left, right := fraction(as), fraction(bs)
		width := max(len(left), len(right))
		return compareText(left+strings.Repeat("0", width-len(left)), right+strings.Repeat("0", width-len(right)))
	}
	return compareText(fmt.Sprint(a), fmt.Sprint(b))
}

func fraction(s string) string {
	if m := fractionPart.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// compareText compares by code point, as the frontend does.
func compareText(a, b string) int {
	return slices.Compare([]rune(a), []rune(b))
}

// cellLabel prints the order cell of the selected row. An instant prints in
// loc as the tables print it.
func cellLabel(v any, c frameColumn, loc *time.Location) string {
	switch v := v.(type) {
	case Number:
		if v.isFloat {
			return strconv.FormatFloat(v.float, 'g', -1, 64)
		}
		decimals := -1
		if m := decimalScale.FindStringSubmatch(c.backendType); c.repr == "decimal" && m != nil {
			decimals, _ = strconv.Atoi(m[1])
		}
		return v.exact.text(decimals)
	case string:
		if c.kind == "time" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return formatInstant(t, loc)
			}
		}
		return v
	}
	if raw, err := json.Marshal(v); err == nil {
		return string(raw)
	}
	return fmt.Sprint(v)
}

// ReduceStat reduces a result-mode frame to one value with the stat rules of
// RFC 0003. A nil value means "No data".
func ReduceStat(res *models.QueryResult, opts StatOptions, loc *time.Location) (*Number, string, error) {
	cols := frameColumns(res, opts.Columns, opts.Untyped)
	if len(res.Logs) == 0 && len(cols) == 0 {
		return nil, "", nil
	}
	column := findColumn(cols, opts.Value)
	if column == nil {
		return nil, "", fmt.Errorf("column %q is missing from the result", opts.Value)
	}
	if column.kind != "number" {
		return nil, "", fmt.Errorf("column %q must be a number (backend type %s)", column.name, cmp.Or(column.backendType, "untyped"))
	}
	var order *frameColumn
	if opts.OrderBy != "" {
		if order = findColumn(cols, opts.OrderBy); order == nil {
			return nil, "", fmt.Errorf("column %q is missing from the result", opts.OrderBy)
		}
	}
	type row struct{ value, order any }
	rows := make([]row, len(res.Logs))
	for i, raw := range res.Logs {
		rows[i].value = readCell(raw[column.name], *column)
		if order != nil {
			rows[i].order = readCell(raw[order.name], *order)
		}
	}
	if opts.Reduce == models.DashboardStatReduceFirst || opts.Reduce == models.DashboardStatReduceLast {
		if order != nil {
			rows = slices.DeleteFunc(rows, func(r row) bool { return r.order == nil })
			slices.SortStableFunc(rows, func(a, b row) int { return compareCells(a.order, b.order, *order) })
		}
		if len(rows) == 0 {
			return nil, "", nil
		}
		selected := rows[len(rows)-1]
		if opts.Reduce == models.DashboardStatReduceFirst {
			selected = rows[0]
		}
		label := ""
		if order != nil {
			label = cellLabel(selected.order, *order, loc)
		}
		n, ok := selected.value.(Number)
		if !ok {
			return nil, label, nil
		}
		return &n, label, nil
	}
	var values []Number
	for _, r := range rows {
		if n, ok := r.value.(Number); ok {
			values = append(values, n)
		}
	}
	n, err := reduceNumbers(values, opts.Reduce)
	return n, "", err
}

func findColumn(cols []frameColumn, name string) *frameColumn {
	for i := range cols {
		if cols[i].name == name {
			return &cols[i]
		}
	}
	return nil
}

// reduceNumbers applies sum, avg, min or max. A frame column holds exact
// numbers or floats, never both.
func reduceNumbers(values []Number, reduce models.DashboardStatReduce) (*Number, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if !values[0].isFloat {
		result := values[0].exact
		for _, next := range values[1:] {
			if next.isFloat {
				return nil, errors.New("mixed numeric representations in a frame column")
			}
			switch {
			case reduce == models.DashboardStatReduceSum || reduce == models.DashboardStatReduceAvg:
				result = result.add(next.exact)
			case reduce == models.DashboardStatReduceMin && next.exact.cmp(result) < 0,
				reduce == models.DashboardStatReduceMax && next.exact.cmp(result) > 0:
				result = next.exact
			}
		}
		if reduce == models.DashboardStatReduceAvg {
			result = result.average(int64(len(values)))
		}
		n := exactNumber(result)
		return &n, nil
	}
	var floats []float64
	for _, v := range values {
		if v.isFloat {
			floats = append(floats, v.float)
		}
	}
	var sum float64
	for _, f := range floats {
		sum += f
	}
	result := floats[0]
	switch reduce {
	case models.DashboardStatReduceMin:
		result = slices.Min(floats)
	case models.DashboardStatReduceMax:
		result = slices.Max(floats)
	default:
	}
	switch reduce {
	case models.DashboardStatReduceSum:
		result = sum
	case models.DashboardStatReduceAvg:
		result = sum / float64(len(floats))
	default:
	}
	n := floatNumber(result)
	return &n, nil
}
