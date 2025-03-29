package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateTeamSourceQuery creates a new query for a team and source
func (db *DB) CreateTeamSourceQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("creating team source query", "team_id", query.TeamID, "source_id", query.SourceID)

	description := sql.NullString{}
	if query.Description != "" {
		description.String = query.Description
		description.Valid = true
	}

	// Use the generated sqlc function and parameters
	id, err := db.queries.CreateTeamSourceQuery(ctx, sqlc.CreateTeamSourceQueryParams{
		TeamID:      int64(query.TeamID),
		SourceID:    int64(query.SourceID),
		Name:        query.Name,
		Description: description,
		// QueryType removed
		QueryContent: query.QueryContent,
	})
	if err != nil {
		db.log.Error("failed to create team source query", "error", err, "team_id", query.TeamID, "source_id", query.SourceID)
		return fmt.Errorf("error creating team source query: %w", err)
	}

	// Set the auto-generated ID
	query.ID = int(id)

	db.log.Debug("team source query created", "query_id", query.ID)
	return nil
}

// GetTeamSourceQuery retrieves a query by ID for a specific team and source
func (db *DB) GetTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting team source query", "team_id", teamID, "source_id", sourceID, "query_id", queryID)

	sqlcQuery, err := db.queries.GetTeamSourceQuery(ctx, sqlc.GetTeamSourceQueryParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team source query")
	}

	description := ""
	if sqlcQuery.Description.Valid {
		description = sqlcQuery.Description.String
	}

	// Convert sqlc.TeamQuery to models.SavedTeamQuery (assuming this is the desired return type)
	return &models.SavedTeamQuery{
		ID:           int(sqlcQuery.ID),
		TeamID:       models.TeamID(sqlcQuery.TeamID),
		SourceID:     models.SourceID(sqlcQuery.SourceID),
		Name:         sqlcQuery.Name,
		Description:  description,
		QueryType:    models.SavedQueryType(sqlcQuery.QueryType), // Keep QueryType if still present in model
		QueryContent: sqlcQuery.QueryContent,
		CreatedAt:    sqlcQuery.CreatedAt,
		UpdatedAt:    sqlcQuery.UpdatedAt,
	}, nil
}

// UpdateTeamSourceQuery updates a query for a team and source
func (db *DB) UpdateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int, name, description, queryContent string) error {
	db.log.Debug("updating team source query", "team_id", teamID, "source_id", sourceID, "query_id", queryID)

	desc := sql.NullString{}
	if description != "" {
		desc.String = description
		desc.Valid = true
	}

	err := db.queries.UpdateTeamSourceQuery(ctx, sqlc.UpdateTeamSourceQueryParams{
		Name:         name,
		Description:  desc,
		QueryContent: queryContent,
		ID:           int64(queryID),
		TeamID:       int64(teamID),
		SourceID:     int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to update team source query", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error updating team source query: %w", err)
	}

	return nil
}

// DeleteTeamSourceQuery deletes a query by ID for a specific team and source
func (db *DB) DeleteTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) error {
	db.log.Debug("deleting team source query", "team_id", teamID, "source_id", sourceID, "query_id", queryID)

	err := db.queries.DeleteTeamSourceQuery(ctx, sqlc.DeleteTeamSourceQueryParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to delete team source query", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error deleting team source query: %w", err)
	}

	return nil
}

// ListQueriesByTeamAndSource lists all queries for a specific team and source
func (db *DB) ListQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for team and source", "team_id", teamID, "source_id", sourceID)

	sqlcQueries, err := db.queries.ListQueriesByTeamAndSource(ctx, sqlc.ListQueriesByTeamAndSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to list queries for team and source", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for team and source: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       models.TeamID(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    models.SavedQueryType(sq.QueryType), // Keep QueryType if still present in model
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("team and source queries listed", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}
