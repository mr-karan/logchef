package logchefql

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/pkg/models"
)

type Executor struct {
	queryExecutor db.QueryExecutor
}

func NewExecutor(queryExecutor db.QueryExecutor) *Executor {
	return &Executor{
		queryExecutor: queryExecutor,
	}
}

func (e *Executor) Execute(ctx context.Context, source *models.Source, query string) (*models.LogResponse, error) {
	// Parse LogchefQL to SQL
	parsedQuery, err := Parse(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogchefQL: %w", err)
	}

	// Convert to SQL - use * for SELECT to get all fields
	sql, args := parsedQuery.ToSQL(source.TableName)

	// Execute using the common executor
	return e.queryExecutor.ExecuteRawQuery(ctx, source, sql, args)
}
