package querybuilder

import (
	"backend-v2/pkg/models"
	"fmt"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

// NewFilterBuilder creates a new FilterBuilder instance
func NewFilterBuilder(tableName string, opts Options, filterGroups []models.FilterGroup) *FilterBuilder {
	return &FilterBuilder{
		TableName:    tableName,
		Options:      opts,
		FilterGroups: filterGroups,
	}
}

// Build implements the Builder interface for FilterBuilder
func (fb *FilterBuilder) Build() (*Query, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("toJSONString(tuple(*)) as raw_json")
	sb.From(fb.TableName)

	// Add time range conditions
	if !fb.Options.StartTime.IsZero() {
		sb.Where(sb.GE("timestamp", sqlbuilder.Raw(fmt.Sprintf("toDateTime64(%f, 3)", float64(fb.Options.StartTime.UnixNano())/1e9))))
	}
	if !fb.Options.EndTime.IsZero() {
		sb.Where(sb.LE("timestamp", sqlbuilder.Raw(fmt.Sprintf("toDateTime64(%f, 3)", float64(fb.Options.EndTime.UnixNano())/1e9))))
	}

	// Build filter groups
	hasMultipleGroups := len(fb.FilterGroups) > 1
	if hasMultipleGroups {
		var groupClauses []string
		for _, group := range fb.FilterGroups {
			// Create a new builder for each group
			groupBuilder := sqlbuilder.NewSelectBuilder()
			if err := fb.buildFilterGroup(groupBuilder, group, true); err != nil {
				return nil, fmt.Errorf("error building filter group: %w", err)
			}
			sql, _ := groupBuilder.Build()
			if whereStart := strings.Index(sql, " WHERE "); whereStart != -1 {
				groupClauses = append(groupClauses, sql[whereStart+7:])
			}
		}
		if len(groupClauses) > 0 {
			sb.Where("(" + strings.Join(groupClauses, " OR ") + ")")
		}
	} else if len(fb.FilterGroups) == 1 {
		if err := fb.buildFilterGroup(sb, fb.FilterGroups[0], false); err != nil {
			return nil, fmt.Errorf("error building filter group: %w", err)
		}
	}

	// Add sorting
	if fb.Options.Sort != nil {
		if fb.Options.Sort.Order == models.SortOrderDesc {
			sb.OrderBy(fb.Options.Sort.Field + " DESC")
		} else {
			sb.OrderBy(fb.Options.Sort.Field + " ASC")
		}
	} else {
		sb.OrderBy("timestamp DESC")
	}

	// Add limit
	if fb.Options.Limit > 0 {
		sb.Limit(fb.Options.Limit)
	}

	// Build the query
	sql, args := sb.Build()
	return &Query{SQL: sql, Args: args}, nil
}

// buildFilterGroup builds SQL conditions for a filter group
func (fb *FilterBuilder) buildFilterGroup(sb *sqlbuilder.SelectBuilder, group models.FilterGroup, isMultipleGroups bool) error {
	if len(group.Conditions) == 0 {
		return nil
	}

	// Build conditions for this group
	var conditions []string
	for _, condition := range group.Conditions {
		var condSQL string
		switch condition.Operator {
		case models.FilterOperatorEquals:
			condSQL = fmt.Sprintf("%s = '%s'", condition.Field, condition.Value)
		case models.FilterOperatorNotEquals:
			condSQL = fmt.Sprintf("%s != '%s'", condition.Field, condition.Value)
		case models.FilterOperatorContains:
			condSQL = fmt.Sprintf("position(%s, '%s') > 0", condition.Field, condition.Value)
		case models.FilterOperatorNotContains:
			condSQL = fmt.Sprintf("position(%s, '%s') = 0", condition.Field, condition.Value)
		case models.FilterOperatorGreaterThan:
			condSQL = fmt.Sprintf("%s > '%s'", condition.Field, condition.Value)
		case models.FilterOperatorLessThan:
			condSQL = fmt.Sprintf("%s < '%s'", condition.Field, condition.Value)
		case models.FilterOperatorGreaterEquals:
			condSQL = fmt.Sprintf("%s >= '%s'", condition.Field, condition.Value)
		case models.FilterOperatorLessEquals:
			condSQL = fmt.Sprintf("%s <= '%s'", condition.Field, condition.Value)
		default:
			return fmt.Errorf("unsupported operator %s for field %s", condition.Operator, condition.Field)
		}
		conditions = append(conditions, condSQL)
	}

	// Join conditions with the appropriate operator
	var operator string
	if group.Operator == models.GroupOperatorAnd {
		operator = " AND "
	} else {
		operator = " OR "
	}

	whereClause := strings.Join(conditions, operator)

	// Add parentheses around the group if it has multiple conditions or if there are multiple groups
	if len(conditions) > 1 || isMultipleGroups {
		whereClause = "(" + whereClause + ")"
	}

	// Add the group's conditions to the main query
	sb.Where(whereClause)

	return nil
}
