package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// mapSavedQueryRow converts a generated sqlc.SavedQuery into the domain model.
func mapSavedQueryRow(row sqlc.SavedQuery) *models.SavedQuery {
	q := &models.SavedQuery{
		ID:                int(row.ID),
		SourceID:          models.SourceID(row.SourceID),
		Name:              row.Name,
		Description:       row.Description.String,
		QueryType:         models.SavedQueryType(row.QueryType),
		QueryContent:      row.QueryContent,
		CreatedFromTeamID: nullableTeamID(row.CreatedFromTeamID),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		q.CreatedBy = &uid
	}
	return q
}

func nullableTeamID(value sql.NullInt64) *models.TeamID {
	if !value.Valid {
		return nil
	}
	teamID := models.TeamID(value.Int64)
	return &teamID
}

// CreateSavedQuery inserts a new saved query and returns the persisted record.
func (db *DB) CreateSavedQuery(ctx context.Context, sourceID models.SourceID, createdFromTeamID *models.TeamID, name, description, queryType, queryContent string, createdBy *models.UserID) (*models.SavedQuery, error) {
	params := sqlc.CreateSavedQueryParams{
		SourceID:     int64(sourceID),
		Name:         name,
		Description:  nullString(description),
		QueryType:    queryType,
		QueryContent: queryContent,
	}
	if createdFromTeamID != nil {
		params.CreatedFromTeamID = sql.NullInt64{Int64: int64(*createdFromTeamID), Valid: true}
	}
	if createdBy != nil {
		params.CreatedBy = sql.NullInt64{Int64: int64(*createdBy), Valid: true}
	}

	id, err := db.writeQueries.CreateSavedQuery(ctx, params)
	if err != nil {
		db.log.Error("failed to create saved query", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}

	return db.GetSavedQuery(ctx, int(id))
}

// GetSavedQuery returns a saved query by id, or ErrQueryNotFound if missing.
func (db *DB) GetSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error) {
	row, err := db.readQueries.GetSavedQuery(ctx, int64(queryID))
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting saved query id %d", queryID))
	}
	return mapSavedQueryRow(row), nil
}

// UpdateSavedQuery overwrites the mutable fields of a saved query.
func (db *DB) UpdateSavedQuery(ctx context.Context, queryID int, name, description, queryType, queryContent string) error {
	params := sqlc.UpdateSavedQueryParams{
		Name:         name,
		Description:  nullString(description),
		QueryType:    queryType,
		QueryContent: queryContent,
		ID:           int64(queryID),
	}
	if err := db.writeQueries.UpdateSavedQuery(ctx, params); err != nil {
		db.log.Error("failed to update saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error updating saved query: %w", err)
	}
	return nil
}

// DeleteSavedQuery removes a saved query.
func (db *DB) DeleteSavedQuery(ctx context.Context, queryID int) error {
	if err := db.writeQueries.DeleteSavedQuery(ctx, int64(queryID)); err != nil {
		db.log.Error("failed to delete saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting saved query: %w", err)
	}
	return nil
}

// ListSavedQueriesForUser returns every saved query the user can see via any of their teams.
func (db *DB) ListSavedQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedQuery, error) {
	rows, err := db.readQueries.ListSavedQueriesForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list saved queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing saved queries for user: %w", err)
	}

	queries := make([]*models.SavedQuery, 0, len(rows))
	for _, r := range rows {
		q := &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: nullableTeamID(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       r.Description.String,
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
			SourceName:        r.SourceName,
		}
		if r.CreatedBy.Valid {
			uid := models.UserID(r.CreatedBy.Int64)
			q.CreatedBy = &uid
		}
		queries = append(queries, q)
	}
	return queries, nil
}

// ListAllSavedQueries returns every saved query with no source-access gate. This
// is the global-admin browse surface only; callers MUST authorize the admin role
// before invoking it. Rows the caller can't run are marked non-runnable upstream.
func (db *DB) ListAllSavedQueries(ctx context.Context) ([]*models.SavedQuery, error) {
	rows, err := db.readQueries.ListAllSavedQueries(ctx)
	if err != nil {
		db.log.Error("failed to list all saved queries", "error", err)
		return nil, fmt.Errorf("error listing all saved queries: %w", err)
	}

	queries := make([]*models.SavedQuery, 0, len(rows))
	for _, r := range rows {
		q := &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: nullableTeamID(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       r.Description.String,
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
			SourceName:        r.SourceName,
		}
		if r.CreatedBy.Valid {
			uid := models.UserID(r.CreatedBy.Int64)
			q.CreatedBy = &uid
		}
		queries = append(queries, q)
	}
	return queries, nil
}

// ListAccessibleSourceIDsForUser returns the set of source IDs the user can reach
// via any team, for marking `runnable` on browse lists without an N+1 check.
func (db *DB) ListAccessibleSourceIDsForUser(ctx context.Context, userID models.UserID) (map[models.SourceID]bool, error) {
	rows, err := db.readQueries.ListAccessibleSourceIDsForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list accessible source ids", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing accessible source ids: %w", err)
	}
	set := make(map[models.SourceID]bool, len(rows))
	for _, id := range rows {
		set[models.SourceID(id)] = true
	}
	return set, nil
}

// ListSavedQueriesForUserBySource returns saved queries for one source, scoped to a user's access.
func (db *DB) ListSavedQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedQuery, error) {
	rows, err := db.readQueries.ListSavedQueriesForUserBySource(ctx, sqlc.ListSavedQueriesForUserBySourceParams{
		SourceID: int64(sourceID),
		UserID:   int64(userID),
	})
	if err != nil {
		db.log.Error("failed to list saved queries for user+source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}

	queries := make([]*models.SavedQuery, 0, len(rows))
	for _, r := range rows {
		q := &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: nullableTeamID(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       r.Description.String,
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
			SourceName:        r.SourceName,
		}
		if r.CreatedBy.Valid {
			uid := models.UserID(r.CreatedBy.Int64)
			q.CreatedBy = &uid
		}
		queries = append(queries, q)
	}
	return queries, nil
}
