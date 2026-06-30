package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateQueryShare persists an ad hoc query share token.
func (s *Store) CreateQueryShare(ctx context.Context, share *models.QueryShare) error {
	var teamID *int64
	if share.TeamID != nil {
		v := int64(*share.TeamID)
		teamID = &v
	}
	err := s.q.CreateQueryShare(ctx, sqlc.CreateQueryShareParams{
		Token:       share.Token,
		SourceID:    int64(share.SourceID),
		TeamID:      int8FromPtr(teamID),
		CreatedBy:   int64(share.CreatedBy),
		PayloadJson: string(share.Payload),
		ExpiresAt:   ts(share.ExpiresAt),
	})
	if err != nil {
		s.log.Error("failed to create query share", "error", err, "source_id", share.SourceID)
		return fmt.Errorf("error creating query share: %w", err)
	}
	return nil
}

// GetQueryShare retrieves an ad hoc query share by token. Returns models.ErrNotFound
// when absent, matching how the share handlers detect not-found across backends.
func (s *Store) GetQueryShare(ctx context.Context, token string) (*models.QueryShare, error) {
	row, err := s.q.GetQueryShare(ctx, token)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		s.log.Error("failed to get query share", "error", err, "token", token)
		return nil, fmt.Errorf("error getting query share: %w", err)
	}

	share := &models.QueryShare{
		Token:          row.Token,
		SourceID:       models.SourceID(row.SourceID),
		CreatedBy:      models.UserID(row.CreatedBy),
		Payload:        []byte(row.PayloadJson),
		ExpiresAt:      row.ExpiresAt.Time,
		CreatedAt:      row.CreatedAt.Time,
		CreatedByEmail: row.Email,
		CreatedByName:  row.FullName,
	}
	if row.TeamID.Valid {
		tid := models.TeamID(row.TeamID.Int64)
		share.TeamID = &tid
	}
	share.LastAccessedAt = tsPtr(row.LastAccessedAt)
	return share, nil
}

// TouchQueryShare updates the last-access timestamp.
func (s *Store) TouchQueryShare(ctx context.Context, token string, accessedAt time.Time) error {
	if err := s.q.TouchQueryShare(ctx, sqlc.TouchQueryShareParams{
		LastAccessedAt: ts(accessedAt),
		Token:          token,
	}); err != nil {
		s.log.Error("failed to touch query share", "error", err, "token", token)
		return fmt.Errorf("error touching query share: %w", err)
	}
	return nil
}

// DeleteQueryShare removes a query share by token. Returns models.ErrNotFound when no
// share matched, matching the SQLite backend.
func (s *Store) DeleteQueryShare(ctx context.Context, token string) error {
	if _, err := s.q.DeleteQueryShare(ctx, token); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to delete query share", "error", err, "token", token)
		return fmt.Errorf("error deleting query share: %w", err)
	}
	return nil
}

// GetUserTeamForSource returns a team the user belongs to that can access the source.
func (s *Store) GetUserTeamForSource(ctx context.Context, userID models.UserID, sourceID models.SourceID) (models.TeamID, error) {
	teamID, err := s.q.GetUserTeamForSource(ctx, sqlc.GetUserTeamForSourceParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		if notFound(err) {
			return 0, models.ErrNotFound
		}
		return 0, err
	}
	return models.TeamID(teamID), nil
}

// PruneExpiredQueryShares removes expired query shares.
func (s *Store) PruneExpiredQueryShares(ctx context.Context, before time.Time) error {
	if err := s.q.PruneExpiredQueryShares(ctx, ts(before)); err != nil {
		s.log.Error("failed to prune expired query shares", "error", err)
		return fmt.Errorf("error pruning expired query shares: %w", err)
	}
	return nil
}
