package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateTeamQuery implements the auth.Store interface
func (db *DB) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("creating team query from TeamQuery model", "team_id", query.TeamID, "name", query.Name)

	description := sql.NullString{}
	if query.Description != "" {
		description.String = query.Description
		description.Valid = true
	}

	id, err := db.queries.CreateTeamQuery(ctx, sqlc.CreateTeamQueryParams{
		TeamID:       int64(query.TeamID),
		SourceID:     int64(query.SourceID),
		Name:         query.Name,
		Description:  description,
		QueryType:    string(query.QueryType),
		QueryContent: query.QueryContent,
	})

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

	description := sql.NullString{}
	if req.Description != "" {
		description.String = req.Description
		description.Valid = true
	}

	id, err := db.queries.CreateTeamQuery(ctx, sqlc.CreateTeamQueryParams{
		TeamID:       int64(teamID),
		SourceID:     int64(req.SourceID),
		Name:         req.Name,
		Description:  description,
		QueryType:    req.QueryType,
		QueryContent: req.QueryContent,
	})

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

	sqlcQuery, err := db.queries.GetTeamQuery(ctx, int64(queryID))
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query")
	}

	description := ""
	if sqlcQuery.Description.Valid {
		description = sqlcQuery.Description.String
	}

	// Convert sqlc.TeamQuery to models.TeamQuery
	return &models.TeamQuery{
		ID:           int(sqlcQuery.ID),
		TeamID:       models.TeamID(sqlcQuery.TeamID),
		SourceID:     models.SourceID(sqlcQuery.SourceID),
		Name:         sqlcQuery.Name,
		Description:  description,
		QueryType:    sqlcQuery.QueryType,
		QueryContent: sqlcQuery.QueryContent,
		Timestamps: models.Timestamps{
			CreatedAt: sqlcQuery.CreatedAt,
			UpdatedAt: sqlcQuery.UpdatedAt,
		},
	}, nil
}

// GetSavedTeamQuery gets a single team query by ID
func (db *DB) GetSavedTeamQuery(ctx context.Context, id int) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting saved team query", "query_id", id)

	sqlcQuery, err := db.queries.GetTeamQuery(ctx, int64(id))
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query")
	}

	description := ""
	if sqlcQuery.Description.Valid {
		description = sqlcQuery.Description.String
	}

	return &models.SavedTeamQuery{
		ID:           int(sqlcQuery.ID),
		TeamID:       int(sqlcQuery.TeamID),
		SourceID:     models.SourceID(sqlcQuery.SourceID),
		Name:         sqlcQuery.Name,
		Description:  description,
		QueryType:    sqlcQuery.QueryType,
		QueryContent: sqlcQuery.QueryContent,
		CreatedAt:    sqlcQuery.CreatedAt,
		UpdatedAt:    sqlcQuery.UpdatedAt,
	}, nil
}

// GetTeamQueryWithAccess gets a team query by ID and checks if the user has access to it
func (db *DB) GetTeamQueryWithAccess(ctx context.Context, id int, userID models.UserID) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting team query with access check", "query_id", id, "user_id", userID)

	sqlcQuery, err := db.queries.GetTeamQueryWithAccess(ctx, sqlc.GetTeamQueryWithAccessParams{
		ID:     int64(id),
		UserID: int64(userID),
	})
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query with access")
	}

	description := ""
	if sqlcQuery.Description.Valid {
		description = sqlcQuery.Description.String
	}

	return &models.SavedTeamQuery{
		ID:           int(sqlcQuery.ID),
		TeamID:       int(sqlcQuery.TeamID),
		SourceID:     models.SourceID(sqlcQuery.SourceID),
		Name:         sqlcQuery.Name,
		Description:  description,
		QueryType:    sqlcQuery.QueryType,
		QueryContent: sqlcQuery.QueryContent,
		CreatedAt:    sqlcQuery.CreatedAt,
		UpdatedAt:    sqlcQuery.UpdatedAt,
	}, nil
}

// UpdateTeamQuery implements auth.Store interface
func (db *DB) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("updating team query", "query_id", query.ID, "source_id", query.SourceID)

	description := sql.NullString{}
	if query.Description != "" {
		description.String = query.Description
		description.Valid = true
	}

	// Update the record
	err := db.queries.UpdateTeamQuery(ctx, sqlc.UpdateTeamQueryParams{
		Name:         query.Name,
		Description:  description,
		SourceID:     int64(query.SourceID),
		QueryType:    query.QueryType,
		QueryContent: query.QueryContent,
		ID:           int64(query.ID),
	})
	if err != nil {
		db.log.Error("failed to update team query", "error", err, "query_id", query.ID)
		return fmt.Errorf("error updating team query: %w", err)
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

	description := sql.NullString{}
	if existing.Description != "" {
		description.String = existing.Description
		description.Valid = true
	}

	// Update the record
	err = db.queries.UpdateTeamQuery(ctx, sqlc.UpdateTeamQueryParams{
		Name:         existing.Name,
		Description:  description,
		SourceID:     int64(existing.SourceID),
		QueryType:    existing.QueryType,
		QueryContent: existing.QueryContent,
		ID:           int64(id),
	})
	if err != nil {
		db.log.Error("failed to update team query", "error", err, "query_id", id)
		return nil, fmt.Errorf("error updating team query: %w", err)
	}

	// Refresh from database
	return db.GetSavedTeamQuery(ctx, id)
}

// DeleteTeamQuery implements the auth.Store interface
func (db *DB) DeleteTeamQuery(ctx context.Context, queryID int) error {
	db.log.Debug("deleting team query", "query_id", queryID)

	err := db.queries.DeleteTeamQuery(ctx, int64(queryID))
	if err != nil {
		db.log.Error("failed to delete team query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting team query: %w", err)
	}

	return nil
}

// ListTeamQueries lists all saved queries for a team
func (db *DB) ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.TeamQuery, error) {
	db.log.Debug("listing team queries", "team_id", teamID)

	sqlcQueries, err := db.queries.ListTeamQueries(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team queries", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team queries: %w", err)
	}

	// Convert sqlc.TeamQuery to models.TeamQuery
	queries := make([]*models.TeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.TeamQuery{
			ID:           int(sq.ID),
			TeamID:       models.TeamID(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			Timestamps: models.Timestamps{
				CreatedAt: sq.CreatedAt,
				UpdatedAt: sq.UpdatedAt,
			},
		}
	}

	db.log.Debug("team queries listed", "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListSavedTeamQueries lists all saved queries for a team returning SavedTeamQuery models
func (db *DB) ListSavedTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing saved team queries", "team_id", teamID)

	sqlcQueries, err := db.queries.ListTeamQueries(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list saved team queries", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing saved team queries: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("saved team queries listed", "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUserAndTeam lists all queries for a specific team that a user has access to
func (db *DB) ListQueriesForUserAndTeam(ctx context.Context, userID models.UserID, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user and team", "user_id", userID, "team_id", teamID)

	sqlcQueries, err := db.queries.ListQueriesForUserAndTeam(ctx, sqlc.ListQueriesForUserAndTeamParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		db.log.Error("failed to list team queries for user", "error", err, "user_id", userID, "team_id", teamID)
		return nil, fmt.Errorf("error listing team queries for user: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("user team queries listed", "user_id", userID, "team_id", teamID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUser lists all queries that a user has access to across all their teams
func (db *DB) ListQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user across all teams", "user_id", userID)

	sqlcQueries, err := db.queries.ListQueriesForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing queries for user: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("user queries listed", "user_id", userID, "count", len(queries))
	return queries, nil
}

// ListQueriesBySource lists all queries for a specific source
func (db *DB) ListQueriesBySource(ctx context.Context, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for source", "source_id", sourceID)

	sqlcQueries, err := db.queries.ListQueriesBySource(ctx, int64(sourceID))
	if err != nil {
		db.log.Error("failed to list queries for source", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for source: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("source queries listed", "source_id", sourceID, "count", len(queries))
	return queries, nil
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
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("team and source queries listed", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// ListQueriesForUserBySource lists all queries for a specific source that a user has access to
func (db *DB) ListQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for user and source", "user_id", userID, "source_id", sourceID)

	sqlcQueries, err := db.queries.ListQueriesForUserBySource(ctx, sqlc.ListQueriesForUserBySourceParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to list queries for user and source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for user and source: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, len(sqlcQueries))
	for i, sq := range sqlcQueries {
		description := ""
		if sq.Description.Valid {
			description = sq.Description.String
		}

		queries[i] = &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       int(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  description,
			QueryType:    sq.QueryType,
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		}
	}

	db.log.Debug("user and source queries listed", "user_id", userID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}
