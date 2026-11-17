package datasource

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertConditionCompiler is an optional interface for providers that compile
// a LogchefQL alert condition, a filter and an aggregation pipe, into the
// native query that their EvaluateAlert runs for the lookback that ends at the
// evaluation instant. The query must not depend on that instant, so that
// backtest replays it.
type AlertConditionCompiler interface {
	CompileAlertCondition(ctx context.Context, source *models.Source, query string, lookbackSeconds int) (*CompiledLogchefQL, error)
}

// AlertCondition is a LogchefQL alert condition compiled for one source.
type AlertCondition struct {
	Language models.QueryLanguage
	Query    string
	// GroupBy names the group columns in written order. Empty for an
	// ungrouped condition.
	GroupBy []string
	// renames maps a group column the backend returns under its field
	// name, as VictoriaLogs does, to the column's output name.
	renames map[string]string
}

// CompileAlertCondition compiles a LogchefQL alert condition and checks that
// it computes one value, for each group or for the whole window.
func (s *Service) CompileAlertCondition(ctx context.Context, sourceID models.SourceID, query string, lookbackSeconds int) (*AlertCondition, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return s.compileAlertCondition(ctx, source, provider, query, lookbackSeconds)
}

func (s *Service) compileAlertCondition(ctx context.Context, source *models.Source, provider Provider, query string, lookbackSeconds int) (*AlertCondition, error) {
	compiler, ok := provider.(AlertConditionCompiler)
	if !ok {
		return nil, fmt.Errorf("%s does not compile LogchefQL alert conditions: %w", source.SourceType, ErrOperationNotSupported)
	}
	source, err := s.compileSource(ctx, source, provider)
	if err != nil {
		return nil, err
	}
	compiled, err := compiler.CompileAlertCondition(ctx, source, query, lookbackSeconds)
	if err != nil {
		return nil, &ValidationError{Field: "query", Message: "invalid LogchefQL condition", Err: err}
	}
	groupBy, err := alertConditionGroups(compiled.Aggregation)
	if err != nil {
		return nil, &ValidationError{Field: "query", Message: err.Error()}
	}
	condition := &AlertCondition{Language: compiled.Language, Query: compiled.Query, GroupBy: groupBy}
	for _, column := range compiled.Aggregation.Columns {
		if column.Role == logchefql.RoleGroup && column.Source != "" && column.Source != column.Name {
			if condition.renames == nil {
				condition.renames = map[string]string{}
			}
			condition.renames[column.Source] = column.Name
		}
	}
	return condition, nil
}

// nameGroups renames the group columns of an evaluated condition to their
// output names, the names in group_by.
func (c *AlertCondition) nameGroups(result *models.QueryResult) {
	if len(c.renames) == 0 || result == nil {
		return
	}
	for i, column := range result.Columns {
		if name, ok := c.renames[column.Name]; ok {
			result.Columns[i].Name = name
		}
	}
	for _, row := range result.Logs {
		for source, name := range c.renames {
			if value, ok := row[source]; ok {
				delete(row, source)
				row[name] = value
			}
		}
	}
}

// alertConditionGroups returns the group columns of an aggregation that an
// alert can read: one value, for the window or for each group of at most
// models.MaxAlertGroupBy keys.
func alertConditionGroups(info *logchefql.AggregationInfo) ([]string, error) {
	if info == nil {
		return nil, fmt.Errorf("an alert condition needs an aggregation, such as | count()")
	}
	if info.Shape != logchefql.ShapeScalar && info.Shape != logchefql.ShapeGrouped {
		return nil, fmt.Errorf("an alert condition has one window per evaluation. Remove time from by")
	}
	var groups []string
	values := 0
	for _, column := range info.Columns {
		switch column.Role {
		case logchefql.RoleTime:
			// The shape check above refuses a time key.
		case logchefql.RoleGroup:
			groups = append(groups, column.Name)
		case logchefql.RoleValue:
			values++
		}
	}
	if values != 1 {
		return nil, fmt.Errorf("an alert condition computes one value. Remove the other aggregates")
	}
	if len(groups) > models.MaxAlertGroupBy {
		return nil, fmt.Errorf("an alert condition groups by at most %d keys", models.MaxAlertGroupBy)
	}
	return groups, nil
}
