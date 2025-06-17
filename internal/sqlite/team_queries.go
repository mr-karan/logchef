package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateTeamSourceQuery inserts a new saved query record associated with a team and source.
// It populates the ID field on the input query model upon success.
func (db *DB) CreateTeamSourceQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("creating team source query record", "team_id", query.TeamID, "source_id", query.SourceID, "name", query.Name)

	// Map domain model to sqlc parameters.
	description := sql.NullString{String: query.Description, Valid: query.Description != ""}
	params := sqlc.CreateTeamSourceQueryParams{
		TeamID:       int64(query.TeamID),
		SourceID:     int64(query.SourceID),
		Name:         query.Name,
		Description:  description,
		QueryType:    string(query.QueryType),
		QueryContent: query.QueryContent,
	}

	id, err := db.queries.CreateTeamSourceQuery(ctx, params)
	if err != nil {
		// Consider checking for specific constraint errors (e.g., FK violations if team/source don't exist).
		db.log.Error("failed to create team source query record in db", "error", err, "team_id", query.TeamID, "source_id", query.SourceID)
		return fmt.Errorf("error creating team source query: %w", err)
	}

	// Set auto-generated ID on the input model.
	query.ID = int(id)
	// Timestamps are handled by DB; caller doesn't need them updated here.

	db.log.Debug("team source query record created", "query_id", query.ID)
	return nil
}

// GetTeamSourceQuery retrieves a specific saved query by its ID, scoped to a team and source.
// Returns core.ErrQueryNotFound if not found.
func (db *DB) GetTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {
	db.log.Debug("getting team source query record", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	params := sqlc.GetTeamSourceQueryParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	sqlcQuery, err := db.queries.GetTeamSourceQuery(ctx, params)
	if err != nil {
		// Use handleNotFoundError, although a specific core.ErrQueryNotFound might be defined.
		return nil, handleNotFoundError(err, fmt.Sprintf("getting team query id %d", queryID))
	}

	// Map sqlc result to the SavedTeamQuery domain model.
	return &models.SavedTeamQuery{
		ID:           int(sqlcQuery.ID),
		TeamID:       models.TeamID(sqlcQuery.TeamID),
		SourceID:     models.SourceID(sqlcQuery.SourceID),
		Name:         sqlcQuery.Name,
		Description:  sqlcQuery.Description.String, // Handle NULL string
		QueryType:    models.SavedQueryType(sqlcQuery.QueryType),
		QueryContent: sqlcQuery.QueryContent,
		CreatedAt:    sqlcQuery.CreatedAt,
		UpdatedAt:    sqlcQuery.UpdatedAt,
		// CreatedByUserID is not present in the sqlc model/query.
	}, nil
}

// UpdateTeamSourceQuery updates an existing saved query record.
// Only non-empty fields in the input arguments are intended to be updated (though the current SQL updates all).
// The `updated_at` timestamp is automatically set by the query.
func (db *DB) UpdateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int, name, description, queryType, queryContent string) error {
	db.log.Debug("updating team source query record", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	// Prepare parameters, handling potentially empty update values.
	// The SQL query updates all specified fields; partial updates would require a different query or dynamic SQL.
	desc := sql.NullString{String: description, Valid: description != ""} // Handle empty description correctly.
	params := sqlc.UpdateTeamSourceQueryParams{
		Name:         name,
		Description:  desc,
		QueryType:    queryType,
		QueryContent: queryContent,
		ID:           int64(queryID),
		TeamID:       int64(teamID),
		SourceID:     int64(sourceID),
	}

	err := db.queries.UpdateTeamSourceQuery(ctx, params)
	if err != nil {
		// sqlc Update typically doesn't return ErrNoRows. Check for other errors.
		db.log.Error("failed to update team source query record in db", "error", err, "query_id", queryID)
		return fmt.Errorf("error updating team source query: %w", err)
	}

	db.log.Debug("team source query record updated successfully", "query_id", queryID)
	return nil
}

// DeleteTeamSourceQuery removes a specific saved query record.
func (db *DB) DeleteTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) error {
	db.log.Debug("deleting team source query record", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	params := sqlc.DeleteTeamSourceQueryParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	err := db.queries.DeleteTeamSourceQuery(ctx, params)
	if err != nil {
		// sqlc Delete typically doesn't return ErrNoRows.
		db.log.Error("failed to delete team source query record from db", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting team source query: %w", err)
	}

	db.log.Debug("team source query record deleted successfully", "query_id", queryID)
	return nil
}

// ListQueriesByTeamAndSource retrieves all saved queries for a specific team and source, ordered by creation date.
func (db *DB) ListQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing queries for team and source", "team_id", teamID, "source_id", sourceID)

	params := sqlc.ListQueriesByTeamAndSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	sqlcQueries, err := db.queries.ListQueriesByTeamAndSource(ctx, params)
	if err != nil {
		db.log.Error("failed to list queries for team and source from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing queries for team and source: %w", err)
	}

	// Map results to domain model slice.
	queries := make([]*models.SavedTeamQuery, 0, len(sqlcQueries))
	for _, sq := range sqlcQueries {
		queries = append(queries, &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       models.TeamID(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  sq.Description.String, // Handle NULL string
			QueryType:    models.SavedQueryType(sq.QueryType),
			QueryContent: sq.QueryContent,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		})
	}

	db.log.Debug("team and source queries listed", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// ListBookmarkedQueriesByTeamAndSource retrieves all bookmarked saved queries for a specific team and source, ordered by creation date.
func (db *DB) ListBookmarkedQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	db.log.Debug("listing bookmarked queries for team and source", "team_id", teamID, "source_id", sourceID)

	params := sqlc.ListBookmarkedQueriesByTeamAndSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	sqlcQueries, err := db.queries.ListBookmarkedQueriesByTeamAndSource(ctx, params)
	if err != nil {
		db.log.Error("failed to list bookmarked queries for team and source from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing bookmarked queries for team and source: %w", err)
	}

	// Map results to domain model slice.
	queries := make([]*models.SavedTeamQuery, 0, len(sqlcQueries))
	for _, sq := range sqlcQueries {
		queries = append(queries, &models.SavedTeamQuery{
			ID:           int(sq.ID),
			TeamID:       models.TeamID(sq.TeamID),
			SourceID:     models.SourceID(sq.SourceID),
			Name:         sq.Name,
			Description:  sq.Description.String, // Handle NULL string
			QueryType:    models.SavedQueryType(sq.QueryType),
			QueryContent: sq.QueryContent,
			IsBookmarked: sq.IsBookmarked.Bool,
			CreatedAt:    sq.CreatedAt,
			UpdatedAt:    sq.UpdatedAt,
		})
	}

	db.log.Debug("team and source queries listed", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// CreateTeamSourceBookmarkedQuery inserts a new bookmarked saved query record associated with a team and source.
// It populates the ID field on the input query model upon success.
func (db *DB) CreateTeamSourceBookmarkedQuery(ctx context.Context, query *models.BookmarkedTeamQuery) error {
	db.log.Debug("creating team source query record", "team_id", query.TeamID, "source_id", query.SourceID, "name", query.Name)

	// Map domain model to sqlc parameters.
	description := sql.NullString{String: query.Description, Valid: query.Description != ""}
	params := sqlc.CreateTeamSourceBookmarkedQueryParams{
		TeamID:       int64(query.TeamID),
		SourceID:     int64(query.SourceID),
		Name:         query.Name,
		Description:  description,
		QueryType:    string(query.QueryType),
		QueryContent: query.QueryContent,
		IsBookmarked: sql.NullBool{Bool: query.IsBookmarked, Valid: true},
	}

	id, err := db.queries.CreateTeamSourceBookmarkedQuery(ctx, params)
	if err != nil {
		// Consider checking for specific constraint errors (e.g., FK violations if team/source don't exist).
		db.log.Error("failed to create team source bookmarked query record in db", "error", err, "team_id", query.TeamID, "source_id", query.SourceID)
		return fmt.Errorf("error creating team source bookmarked query: %w", err)
	}

	// Set auto-generated ID on the input model.
	query.ID = int(id)
	// Timestamps are handled by DB; caller doesn't need them updated here.

	db.log.Debug("team source bookmarked query record created", "query_id", query.ID)
	return nil
}	