package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func userIDPtr(v pgtype.Int8) *models.UserID {
	if !v.Valid {
		return nil
	}
	id := models.UserID(v.Int64)
	return &id
}

func teamIDPtr(v pgtype.Int8) *models.TeamID {
	if !v.Valid {
		return nil
	}
	id := models.TeamID(v.Int64)
	return &id
}

func savedQueryToModel(r sqlc.SavedQuery) *models.SavedQuery {
	return &models.SavedQuery{
		ID:                int(r.ID),
		SourceID:          models.SourceID(r.SourceID),
		Name:              r.Name,
		Description:       textStr(r.Description),
		QueryType:         models.SavedQueryType(r.QueryType),
		QueryContent:      r.QueryContent,
		CreatedBy:         userIDPtr(r.CreatedBy),
		CreatedFromTeamID: teamIDPtr(r.CreatedFromTeamID),
		CreatedAt:         r.CreatedAt.Time,
		UpdatedAt:         r.UpdatedAt.Time,
	}
}

// CreateSavedQuery inserts a new saved query and returns the persisted record.
func (s *Store) CreateSavedQuery(ctx context.Context, sourceID models.SourceID, createdFromTeamID *models.TeamID, name, description, queryType, queryContent string, createdBy *models.UserID) (*models.SavedQuery, error) {
	params := sqlc.CreateSavedQueryParams{
		SourceID:     int64(sourceID),
		Name:         name,
		Description:  text(description),
		QueryType:    queryType,
		QueryContent: queryContent,
	}
	if createdFromTeamID != nil {
		params.CreatedFromTeamID = int8Val(int64(*createdFromTeamID))
	}
	if createdBy != nil {
		params.CreatedBy = int8Val(int64(*createdBy))
	}

	id, err := s.q.CreateSavedQuery(ctx, params)
	if err != nil {
		s.log.Error("failed to create saved query", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}
	return s.GetSavedQuery(ctx, int(id))
}

// GetSavedQuery returns a saved query by id, or models.ErrNotFound if missing.
func (s *Store) GetSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error) {
	row, err := s.q.GetSavedQuery(ctx, int64(queryID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting saved query id %d: %w", queryID, err)
	}
	return savedQueryToModel(row), nil
}

// UpdateSavedQuery overwrites the mutable fields of a saved query.
func (s *Store) UpdateSavedQuery(ctx context.Context, queryID int, name, description, queryType, queryContent string) error {
	err := s.q.UpdateSavedQuery(ctx, sqlc.UpdateSavedQueryParams{
		Name:         name,
		Description:  text(description),
		QueryType:    queryType,
		QueryContent: queryContent,
		ID:           int64(queryID),
	})
	if err != nil {
		s.log.Error("failed to update saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error updating saved query: %w", err)
	}
	return nil
}

// DeleteSavedQuery removes a saved query.
func (s *Store) DeleteSavedQuery(ctx context.Context, queryID int) error {
	if err := s.q.DeleteSavedQuery(ctx, int64(queryID)); err != nil {
		s.log.Error("failed to delete saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting saved query: %w", err)
	}
	return nil
}

// ListSavedQueriesForUser returns every saved query the user can see via any team.
func (s *Store) ListSavedQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedQuery, error) {
	rows, err := s.q.ListSavedQueriesForUser(ctx, int64(userID))
	if err != nil {
		s.log.Error("failed to list saved queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing saved queries for user: %w", err)
	}
	queries := make([]*models.SavedQuery, 0, len(rows))
	for i := range rows {
		r := rows[i]
		queries = append(queries, &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: teamIDPtr(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       textStr(r.Description),
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedBy:         userIDPtr(r.CreatedBy),
			CreatedAt:         r.CreatedAt.Time,
			UpdatedAt:         r.UpdatedAt.Time,
			SourceName:        r.SourceName,
		})
	}
	return queries, nil
}

// ListSavedQueriesForUserBySource returns saved queries for one source, scoped to a user.
func (s *Store) ListSavedQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedQuery, error) {
	rows, err := s.q.ListSavedQueriesForUserBySource(ctx, sqlc.ListSavedQueriesForUserBySourceParams{
		SourceID: int64(sourceID),
		UserID:   int64(userID),
	})
	if err != nil {
		s.log.Error("failed to list saved queries for user+source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}
	queries := make([]*models.SavedQuery, 0, len(rows))
	for i := range rows {
		r := rows[i]
		queries = append(queries, &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: teamIDPtr(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       textStr(r.Description),
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedBy:         userIDPtr(r.CreatedBy),
			CreatedAt:         r.CreatedAt.Time,
			UpdatedAt:         r.UpdatedAt.Time,
			SourceName:        r.SourceName,
		})
	}
	return queries, nil
}

// ListAllSavedQueries returns every saved query without applying source-access gates.
func (s *Store) ListAllSavedQueries(ctx context.Context) ([]*models.SavedQuery, error) {
	rows, err := s.q.ListAllSavedQueries(ctx)
	if err != nil {
		s.log.Error("failed to list all saved queries", "error", err)
		return nil, fmt.Errorf("error listing all saved queries: %w", err)
	}
	queries := make([]*models.SavedQuery, 0, len(rows))
	for i := range rows {
		r := rows[i]
		queries = append(queries, &models.SavedQuery{
			ID:                int(r.ID),
			SourceID:          models.SourceID(r.SourceID),
			CreatedFromTeamID: teamIDPtr(r.CreatedFromTeamID),
			Name:              r.Name,
			Description:       textStr(r.Description),
			QueryType:         models.SavedQueryType(r.QueryType),
			QueryContent:      r.QueryContent,
			CreatedBy:         userIDPtr(r.CreatedBy),
			CreatedAt:         r.CreatedAt.Time,
			UpdatedAt:         r.UpdatedAt.Time,
			SourceName:        r.SourceName,
		})
	}
	return queries, nil
}
