package querybuilder

import (
	"fmt"
	"strings"

	"backend-v2/pkg/models"
)

// FilterBuilder builds queries from filter conditions
type FilterBuilder struct {
	tableName  string
	options    Options
	conditions []models.FilterCondition
}

// NewFilterBuilder creates a new filter builder
func NewFilterBuilder(tableName string, options Options, conditions []models.FilterCondition) *FilterBuilder {
	return &FilterBuilder{
		tableName:  tableName,
		options:    options,
		conditions: conditions,
	}
}

// Build builds the query
func (b *FilterBuilder) Build() (*Query, error) {
	var conditions []string

	// Add time range conditions
	if !b.options.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp >= toDateTime64('%s', 3)", b.options.StartTime.Format("2006-01-02 15:04:05.000")))
	}
	if !b.options.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp <= toDateTime64('%s', 3)", b.options.EndTime.Format("2006-01-02 15:04:05.000")))
	}

	// Add filter conditions
	for _, cond := range b.conditions {
		condition, err := b.buildCondition(cond)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}

	// Build WHERE clause
	var whereClause string
	if len(conditions) > 0 {
		whereClause = fmt.Sprintf("WHERE %s", strings.Join(conditions, " AND "))
	}

	// Build ORDER BY clause
	var orderClause string
	if b.options.Sort != nil {
		orderClause = fmt.Sprintf("ORDER BY %s %s", b.options.Sort.Field, b.options.Sort.Order)
	} else {
		orderClause = "ORDER BY timestamp DESC"
	}

	// Build LIMIT clause
	var limitClause string
	if b.options.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", b.options.Limit)
	}

	// Build final query
	query := fmt.Sprintf(`
		SELECT *
		FROM %s
		%s
		%s
		%s
	`, b.tableName, whereClause, orderClause, limitClause)

	return &Query{
		SQL:  query,
		Args: nil,
	}, nil
}

// buildCondition builds a single filter condition
func (b *FilterBuilder) buildCondition(cond models.FilterCondition) (string, error) {
	switch cond.Operator {
	case models.FilterOperatorEquals:
		return fmt.Sprintf("%s = '%v'", cond.Field, cond.Value), nil
	case models.FilterOperatorNotEquals:
		return fmt.Sprintf("%s != '%v'", cond.Field, cond.Value), nil
	case models.FilterOperatorContains:
		return fmt.Sprintf("position(%s, '%v') > 0", cond.Field, cond.Value), nil
	case models.FilterOperatorNotContains:
		return fmt.Sprintf("position(%s, '%v') = 0", cond.Field, cond.Value), nil
	case models.FilterOperatorIContains:
		return fmt.Sprintf("position(lower(%s), lower('%v')) > 0", cond.Field, cond.Value), nil
	case models.FilterOperatorStartsWith:
		return fmt.Sprintf("startsWith(%s, '%v')", cond.Field, cond.Value), nil
	case models.FilterOperatorEndsWith:
		return fmt.Sprintf("endsWith(%s, '%v')", cond.Field, cond.Value), nil
	case models.FilterOperatorIn:
		values, ok := cond.Value.([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid value for IN operator: %v", cond.Value)
		}
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = fmt.Sprintf("'%v'", v)
		}
		return fmt.Sprintf("%s IN (%s)", cond.Field, strings.Join(quoted, ", ")), nil
	case models.FilterOperatorNotIn:
		values, ok := cond.Value.([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid value for NOT IN operator: %v", cond.Value)
		}
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = fmt.Sprintf("'%v'", v)
		}
		return fmt.Sprintf("%s NOT IN (%s)", cond.Field, strings.Join(quoted, ", ")), nil
	case models.FilterOperatorIsNull:
		return fmt.Sprintf("%s IS NULL", cond.Field), nil
	case models.FilterOperatorIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", cond.Field), nil
	default:
		return "", fmt.Errorf("unsupported operator: %s", cond.Operator)
	}
}
