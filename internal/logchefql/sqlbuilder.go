package logchefql

import (
	"fmt"
	"strings"
)

// TimeInterval represents a parsed time interval
type TimeInterval struct {
	Value    int
	Unit     string // h, m, s, d
	Function string // now(), today(), etc.
}

// ParseTimeInterval parses time interval expressions like -1h, -30m, etc.
func ParseTimeInterval(expr string) (*TimeInterval, error) {
	if !strings.HasPrefix(expr, "-") {
		return nil, fmt.Errorf("invalid time interval: %s, must start with -", expr)
	}

	// Remove the minus sign
	expr = expr[1:]

	// Extract numeric value and unit
	var value int
	var unit string
	_, err := fmt.Sscanf(expr, "%d%s", &value, &unit)
	if err != nil {
		return nil, fmt.Errorf("invalid time interval format: %s", expr)
	}

	// Validate value
	if value == 0 {
		return nil, fmt.Errorf("time interval value cannot be zero")
	}

	// Validate unit
	switch unit {
	case "s", "m", "h", "d":
		// valid
	default:
		return nil, fmt.Errorf("invalid time unit: %s, must be s, m, h, or d", unit)
	}

	return &TimeInterval{
		Value:    value,
		Unit:     unit,
		Function: "now()", // default function
	}, nil
}

// SQLBuilder handles the conversion of LogchefQL AST to ClickHouse SQL
type SQLBuilder struct {
	query     *Query
	tableName string
	args      []interface{}
}

// NewSQLBuilder creates a new SQL builder for the given query
func NewSQLBuilder(query *Query, tableName string) *SQLBuilder {
	return &SQLBuilder{
		query:     query,
		tableName: tableName,
		args:      make([]interface{}, 0),
	}
}

// Build generates the SQL query and arguments
func (b *SQLBuilder) Build() (string, []interface{}, error) {
	var conditions []string

	for _, filter := range b.query.Filters {
		condition, err := b.buildCondition(filter)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, condition)
	}

	query := fmt.Sprintf("SELECT * FROM %s", b.tableName)
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	return query, b.args, nil
}

// buildCondition converts a single filter to SQL
func (b *SQLBuilder) buildCondition(filter *Filter) (string, error) {
	// Handle JSON path fields
	if filter.Field.IsJSONPath() {
		return b.buildJSONCondition(filter)
	}

	// Handle regular fields
	switch filter.Operator {
	case "~", "!~":
		return b.buildPatternCondition(filter)
	default:
		if filter.Value.RelativeTime != nil {
			return b.buildTimeCondition(filter)
		}
		return b.buildSimpleCondition(filter)
	}
}

// buildJSONCondition handles JSON field access
func (b *SQLBuilder) buildJSONCondition(filter *Filter) (string, error) {
	jsonPath := strings.Join(filter.Field.SubFields, ".")

	// Handle time intervals in JSON fields
	if filter.Value.RelativeTime != nil {
		b.args = append(b.args, jsonPath, filter.Value.GetValue())
		return fmt.Sprintf("JSONExtractString(%s, ?) %s now() - INTERVAL ?",
			filter.Field.Name,
			filter.Operator), nil
	}

	switch filter.Operator {
	case "~":
		b.args = append(b.args, jsonPath, "%"+filter.Value.GetValue()+"%")
		return fmt.Sprintf("JSONExtractString(%s, ?) ILIKE ?", filter.Field.Name), nil
	case "!~":
		b.args = append(b.args, jsonPath, "%"+filter.Value.GetValue()+"%")
		return fmt.Sprintf("JSONExtractString(%s, ?) NOT ILIKE ?", filter.Field.Name), nil
	default:
		b.args = append(b.args, jsonPath, filter.Value.GetValue())
		return fmt.Sprintf("JSONExtractString(%s, ?) %s ?", filter.Field.Name, filter.Operator), nil
	}
}

// buildPatternCondition handles LIKE/NOT LIKE patterns
func (b *SQLBuilder) buildPatternCondition(filter *Filter) (string, error) {
	b.args = append(b.args, "%"+filter.Value.GetValue()+"%")
	if filter.Operator == "~" {
		return fmt.Sprintf("%s ILIKE ?", filter.Field.Name), nil
	}
	return fmt.Sprintf("%s NOT ILIKE ?", filter.Field.Name), nil
}

// buildTimeCondition handles time intervals
func (b *SQLBuilder) buildTimeCondition(filter *Filter) (string, error) {
	interval, err := ParseTimeInterval(*filter.Value.RelativeTime)
	if err != nil {
		return "", err
	}

	b.args = append(b.args, filter.Value.GetValue())
	return fmt.Sprintf("%s %s %s - INTERVAL ?",
		filter.Field.Name,
		filter.Operator,
		interval.Function), nil
}

// buildSimpleCondition handles simple comparisons
func (b *SQLBuilder) buildSimpleCondition(filter *Filter) (string, error) {
	b.args = append(b.args, filter.Value.GetValue())
	return fmt.Sprintf("%s %s ?", filter.Field.Name, filter.Operator), nil
}
