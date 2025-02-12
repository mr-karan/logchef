package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Query methods

// CreateQuery creates a new query
func (d *DB) CreateQuery(ctx context.Context, query *models.Query) error {
	result, err := d.queries.CreateQuery.ExecContext(ctx,
		query.ID,
		query.SpaceID,
		query.Name,
		query.Description,
		query.QueryContent,
		query.CreatedBy,
		time.Now(),
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("error creating query: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetQuery retrieves a query by ID
func (d *DB) GetQuery(ctx context.Context, queryID string) (*models.Query, error) {
	var query models.Query
	err := d.queries.GetQuery.GetContext(ctx, &query, queryID)
	if err != nil {
		return nil, fmt.Errorf("error getting query: %w", err)
	}
	return &query, nil
}

// UpdateQuery updates an existing query
func (d *DB) UpdateQuery(ctx context.Context, query *models.Query) error {
	result, err := d.queries.UpdateQuery.ExecContext(ctx,
		query.Name,
		query.Description,
		query.QueryContent,
		time.Now(),
		query.ID,
	)
	if err != nil {
		return fmt.Errorf("error updating query: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// DeleteQuery deletes a query by ID
func (d *DB) DeleteQuery(ctx context.Context, queryID string) error {
	result, err := d.queries.DeleteQuery.ExecContext(ctx, queryID)
	if err != nil {
		return fmt.Errorf("error deleting query: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// ListQueries lists all queries in a space
func (d *DB) ListQueries(ctx context.Context, spaceID string) ([]*models.Query, error) {
	var queries []*models.Query
	err := d.queries.ListQueries.SelectContext(ctx, &queries, spaceID)
	if err != nil {
		return nil, fmt.Errorf("error listing queries: %w", err)
	}
	return queries, nil
}
