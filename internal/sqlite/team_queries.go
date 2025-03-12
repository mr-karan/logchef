package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// CreateTeamQuery implements the auth.Store interface
func (db *DB) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("creating team query from TeamQuery model", "team_id", query.TeamID, "name", query.Name)

	var id int64
	err := db.queries.CreateTeamQuery.QueryRowContext(ctx,
		query.TeamID,
		query.SourceID,
		query.Name,
		query.Description,
		query.QueryType,
		query.QueryContent,
	).Scan(&id)

	if err != nil {
		db.log.Error("failed to create team query", "error", err, "team_id", query.TeamID)
		return fmt.Errorf("error creating team query: %w", err)
	}

	// Set the ID on the original query object
	query.ID = int(id)

	return nil
}

// CreateSavedTeamQuery creates a new saved query for a team (used by the team query service)
func (db *DB) CreateSavedTeamQuery(ctx context.Context, teamID models.TeamID, req models.CreateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	db.log.Debug("creating saved team query", "team_id", teamID, "name", req.Name, "source_id", req.SourceID)

	var id int64
	err := db.queries.CreateTeamQuery.QueryRowContext(ctx,
		teamID,
		req.SourceID,
		req.Name,
		req.Description,
		req.QueryType,
		req.QueryContent,
	).Scan(&id)

	if err != nil {
		db.log.Error("failed to create saved team query", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error creating saved team query: %w", err)
	}

	// Get the created query
	query, err := db.GetSavedTeamQuery(ctx, int(id))
	if err != nil {
		db.log.Error("failed to get created team query", "error", err, "query_id", id)
		return nil, fmt.Errorf("error getting created team query: %w", err)
	}

	return query, nil
}

// GetTeamQuery implements the auth.Store interface
func (db *DB) GetTeamQuery(ctx context.Context, queryID int) (*models.TeamQuery, error) {
	db.log.Debug("getting team query", "query_id", queryID)

	var savedQuery models.SavedTeamQuery
	err := db.queries.GetTeamQuery.GetContext(ctx, &savedQuery, queryID)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query")
	}

	// Convert SavedTeamQuery to TeamQuery
	return &models.TeamQuery{
		ID:           savedQuery.ID,
		TeamID:       models.TeamID(savedQuery.TeamID),
		SourceID:     models.SourceID(savedQuery.SourceID),
		Name:         savedQuery.Name,
		Description:  savedQuery.Description,
		QueryContent: savedQuery.QueryContent,
		Timestamps: models.Timestamps{
			CreatedAt: savedQuery.CreatedAt,
			UpdatedAt: savedQuery.UpdatedAt,
		},
	}, nil
}

// GetSavedTeamQuery gets a single team query by ID
func (db *DB) GetSavedTeamQuery(ctx context.Context, id int) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting saved team query", "query_id", id)

	var query models.SavedTeamQuery
	err := db.queries.GetTeamQuery.GetContext(ctx, &query, id)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query")
	}

	return &query, nil
}

// GetTeamQueryWithAccess gets a team query by ID and checks if the user has access to it
func (db *DB) GetTeamQueryWithAccess(ctx context.Context, id int, userID models.UserID) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting team query with access check", "query_id", id, "user_id", userID)

	var query models.SavedTeamQuery
	err := db.queries.GetTeamQueryWithAccess.GetContext(ctx, &query, id, userID)

	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query with access")
	}

	return &query, nil
}

// UpdateTeamQuery implements auth.Store interface
func (db *DB) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("updating team query", "query_id", query.ID, "source_id", query.SourceID)

	// Update the record
	result, err := db.queries.UpdateTeamQuery.ExecContext(ctx,
		query.Name,
		query.Description,
		query.SourceID,
		query.QueryType,
		query.QueryContent,
		time.Now().UTC(),
		query.ID,
	)
	if err != nil {
		db.log.Error("failed to update team query", "error", err, "query_id", query.ID)
		return fmt.Errorf("error updating team query: %w", err)
	}

	if err := checkRowsAffected(result, "update team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "query_id", query.ID)
		return err
	}

	return nil
}

// UpdateSavedTeamQuery updates an existing team query returning the SavedTeamQuery model
func (db *DB) UpdateSavedTeamQuery(ctx context.Context, id int, req models.UpdateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	db.log.Debug("updating saved team query", "query_id", id)

	// Get the existing query first
	existing, err := db.GetSavedTeamQuery(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update only the provided fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" || req.Description == "" {
		// Allow explicitly setting to empty string
		existing.Description = req.Description
	}
	if req.SourceID != 0 {
		existing.SourceID = req.SourceID
	}
	if req.QueryType != "" {
		existing.QueryType = req.QueryType
	}
	if req.QueryContent != "" {
		existing.QueryContent = req.QueryContent
	}

	// Update the record
	now := time.Now().UTC()
	result, err := db.queries.UpdateTeamQuery.ExecContext(ctx,
		existing.Name,
		existing.Description,
		existing.SourceID,
		existing.QueryType,
		existing.QueryContent,
		now,
		id,
	)
	if err != nil {
		db.log.Error("failed to update team query", "error", err, "query_id", id)
		return nil, fmt.Errorf("error updating team query: %w", err)
	}

	if err := checkRowsAffected(result, "update team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "query_id", id)
		return nil, err
	}

	// Refresh from database
	return db.GetSavedTeamQuery(ctx, id)
}

// DeleteTeamQuery implements the auth.Store interface
func (db *DB) DeleteTeamQuery(ctx context.Context, queryID int) error {
	db.log.Debug("deleting team query", "query_id", queryID)

	result, err := db.queries.DeleteTeamQuery.ExecContext(ctx, queryID)
	if err != nil {
		db.log.Error("failed to delete team query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting team query: %w", err)
	}

	if err := checkRowsAffected(result, "delete team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "query_id", queryID)
		return err
	}

	return nil
}

// ListTeamQueries lists all saved queries for a team
func (db *DB) ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.TeamQuery, error) {
	db.log.Debug("listing team queries", "team_id", teamID)

	var savedQueries []*models.SavedTeamQuery
	err := db.queries.ListTeamQueries.SelectContext(ctx, &savedQueries, teamID)
	if err != nil {
		db.log.Error("failed to list team queries", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team queries: %w", err)
	}

	// Convert SavedTeamQuery to TeamQuery
	queries := make([]*models.TeamQuery, len(savedQueries))
	for i, sq := range savedQueries {
		queries[i] = &models.TeamQuery{
			ID:           sq.ID,
			TeamID:       models.TeamID(sq.TeamID),
			Name:         sq.Name,
			Description:  sq.Description,
			QueryContent: sq.QueryContent,
		}
	}

	db.log.Debug("team queries listed", "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListSavedTeamQueries lists all saved queries for a team returning SavedTeamQuery models
func (db *DB) ListSavedTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing saved team queries", "team_id", teamID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListTeamQueries.SelectContext(ctx, &queries, teamID)
	if err != nil {
		db.log.Error("failed to list saved team queries", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing saved team queries: %w", err)
	}

	db.log.Debug("saved team queries listed", "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUserAndTeam lists all queries for a specific team that a user has access to
func (db *DB) ListQueriesForUserAndTeam(ctx context.Context, userID models.UserID, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user and team", "user_id", userID, "team_id", teamID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListQueriesForUserAndTeam.SelectContext(ctx, &queries, teamID, userID)

	if err != nil {
		db.log.Error("failed to list team queries for user", "error", err, "user_id", userID, "team_id", teamID)
		return nil, fmt.Errorf("error listing team queries for user: %w", err)
	}

	db.log.Debug("user team queries listed", "user_id", userID, "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUser lists all queries that a user has access to across all their teams
func (db *DB) ListQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user across all teams", "user_id", userID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListQueriesForUser.SelectContext(ctx, &queries, userID)

	if err != nil {
		db.log.Error("failed to list queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing queries for user: %w", err)
	}

	db.log.Debug("user queries listed", "user_id", userID, "count", len(queries))
	return queries, nil
}

// ListQueriesBySource lists all queries for a specific source
func (db *DB) ListQueriesBySource(ctx context.Context, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for source", "source_id", sourceID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListQueriesBySource.SelectContext(ctx, &queries, sourceID)

	if err != nil {
		db.log.Error("failed to list queries for source", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for source: %w", err)
	}

	db.log.Debug("source queries listed", "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// ListQueriesByTeamAndSource lists all queries for a specific team and source
func (db *DB) ListQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for team and source", "team_id", teamID, "source_id", sourceID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListQueriesByTeamAndSource.SelectContext(ctx, &queries, teamID, sourceID)

	if err != nil {
		db.log.Error("failed to list queries for team and source", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for team and source: %w", err)
	}

	db.log.Debug("team and source queries listed", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUserBySource lists all queries for a specific source that a user has access to
func (db *DB) ListQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user and source", "user_id", userID, "source_id", sourceID)

	var queries []*models.SavedTeamQuery
	err := db.queries.ListQueriesForUserBySource.SelectContext(ctx, &queries, userID, sourceID)

	if err != nil {
		db.log.Error("failed to list queries for user and source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for user and source: %w", err)
	}

	db.log.Debug("user and source queries listed", "user_id", userID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}
