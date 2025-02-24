package clickhouse

import (
	"backend-v2/pkg/querybuilder"
)

// buildQuery builds and validates a raw SQL query
func buildQuery(params LogQueryParams, tableName string) (string, error) {
	opts := querybuilder.Options{
		TableName: tableName,
		RawSQL:    params.RawSQL,
		Limit:     params.Limit,
	}

	builder := querybuilder.NewRawSQLBuilder(opts)
	query, err := builder.Build()
	if err != nil {
		return "", err
	}

	return query.SQL, nil
}
