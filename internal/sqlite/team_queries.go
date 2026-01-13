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

	return nil
}

// GetTeamSourceQuery retrieves a specific saved query by its ID, scoped to a team and source.
// Returns core.ErrQueryNotFound if not found.
func (db *DB) GetTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {

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
		IsBookmarked: sqlcQuery.IsBookmarked,
		CreatedAt:    sqlcQuery.CreatedAt,
		UpdatedAt:    sqlcQuery.UpdatedAt,
		// CreatedByUserID is not present in the sqlc model/query.
	}, nil
}

// UpdateTeamSourceQuery updates an existing saved query record.
// Only non-empty fields in the input arguments are intended to be updated (though the current SQL updates all).
// The `updated_at` timestamp is automatically set by the query.
func (db *DB) UpdateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int, name, description, queryType, queryContent string) error {

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

	return nil
}

// DeleteTeamSourceQuery removes a specific saved query record.
func (db *DB) DeleteTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) error {

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

	return nil
}

// ListQueriesByTeam retrieves all saved queries for a team across all sources.
func (db *DB) ListQueriesByTeam(ctx context.Context, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	sqlcQueries, err := db.queries.ListQueriesByTeam(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list queries for team from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing queries for team: %w", err)
	}

	queries := make([]*models.SavedTeamQuery, 0, len(sqlcQueries))
	for i := range sqlcQueries {
		queries = append(queries, &models.SavedTeamQuery{
			ID:           int(sqlcQueries[i].ID),
			TeamID:       models.TeamID(sqlcQueries[i].TeamID),
			SourceID:     models.SourceID(sqlcQueries[i].SourceID),
			Name:         sqlcQueries[i].Name,
			Description:  sqlcQueries[i].Description.String,
			QueryType:    models.SavedQueryType(sqlcQueries[i].QueryType),
			QueryContent: sqlcQueries[i].QueryContent,
			IsBookmarked: sqlcQueries[i].IsBookmarked,
			CreatedAt:    sqlcQueries[i].CreatedAt,
			UpdatedAt:    sqlcQueries[i].UpdatedAt,
		})
	}

	return queries, nil
}

// ListQueriesByTeamAndSource retrieves all saved queries for a specific team and source.
func (db *DB) ListQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {

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
	for i := range sqlcQueries {
		queries = append(queries, &models.SavedTeamQuery{
			ID:           int(sqlcQueries[i].ID),
			TeamID:       models.TeamID(sqlcQueries[i].TeamID),
			SourceID:     models.SourceID(sqlcQueries[i].SourceID),
			Name:         sqlcQueries[i].Name,
			Description:  sqlcQueries[i].Description.String,
			QueryType:    models.SavedQueryType(sqlcQueries[i].QueryType),
			QueryContent: sqlcQueries[i].QueryContent,
			IsBookmarked: sqlcQueries[i].IsBookmarked,
			CreatedAt:    sqlcQueries[i].CreatedAt,
			UpdatedAt:    sqlcQueries[i].UpdatedAt,
		})
	}

	return queries, nil
}

// ToggleQueryBookmark toggles the bookmark status of a query.
// Returns the new bookmark status after toggling.
func (db *DB) ToggleQueryBookmark(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) (bool, error) {

	params := sqlc.ToggleQueryBookmarkParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}

	err := db.queries.ToggleQueryBookmark(ctx, params)
	if err != nil {
		db.log.Error("failed to toggle query bookmark in db", "error", err, "query_id", queryID)
		return false, fmt.Errorf("error toggling query bookmark: %w", err)
	}

	// Get the new bookmark status
	statusParams := sqlc.GetQueryBookmarkStatusParams{
		ID:       int64(queryID),
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	newStatus, err := db.queries.GetQueryBookmarkStatus(ctx, statusParams)
	if err != nil {
		db.log.Error("failed to get query bookmark status from db", "error", err, "query_id", queryID)
		return false, fmt.Errorf("error getting query bookmark status: %w", err)
	}

	return newStatus, nil
}
