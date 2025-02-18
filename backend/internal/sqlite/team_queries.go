package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Team query methods

// CreateTeamQuery creates a new team query
func (db *DB) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	now := time.Now()
	query.CreatedAt = now
	query.UpdatedAt = now

	result, err := db.queries.CreateTeamQuery.ExecContext(ctx,
		query.ID,
		query.TeamID,
		query.Name,
		query.Description,
		query.QueryContent,
		query.CreatedAt,
		query.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("error creating team query: %w", err)
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

// GetTeamQuery gets a team query by ID
func (db *DB) GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error) {
	var query models.TeamQuery
	err := db.queries.GetTeamQuery.GetContext(ctx, &query, queryID)
	if err != nil {
		return nil, fmt.Errorf("error getting team query: %w", err)
	}
	return &query, nil
}

// UpdateTeamQuery updates a team query
func (db *DB) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	query.UpdatedAt = time.Now()

	result, err := db.queries.UpdateTeamQuery.ExecContext(ctx,
		query.Name,
		query.Description,
		query.QueryContent,
		query.UpdatedAt,
		query.ID,
	)
	if err != nil {
		return fmt.Errorf("error updating team query: %w", err)
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

// DeleteTeamQuery deletes a team query
func (db *DB) DeleteTeamQuery(ctx context.Context, queryID string) error {
	result, err := db.queries.DeleteTeamQuery.ExecContext(ctx, queryID)
	if err != nil {
		return fmt.Errorf("error deleting team query: %w", err)
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

// ListTeamQueries lists all queries for a team
func (db *DB) ListTeamQueries(ctx context.Context, teamID string) ([]*models.TeamQuery, error) {
	var queries []*models.TeamQuery
	err := db.queries.ListTeamQueries.SelectContext(ctx, &queries, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team queries: %w", err)
	}
	return queries, nil
}
