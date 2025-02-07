package clickhouse

import (
	"backend-v2/pkg/querybuilder"
)

// buildFilterQuery builds a query from filter parameters
func buildFilterQuery(params LogQueryParams, tableName string) string {
	// Create query builder based on mode
	opts := querybuilder.Options{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Limit:     params.Limit,
		Sort:      params.Sort,
	}

	builder := querybuilder.NewFilterBuilder(tableName, opts, params.Conditions)
	query, err := builder.Build()
	if err != nil {
		return ""
	}

	return query.SQL
}
