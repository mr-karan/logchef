package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateQueryShare persists an ad hoc query share token.
func (db *DB) CreateQueryShare(ctx context.Context, share *models.QueryShare) error {
	teamID := sql.NullInt64{}
	if share.TeamID != nil {
		teamID = sql.NullInt64{Int64: int64(*share.TeamID), Valid: true}
	}
	err := db.writeQueries.CreateQueryShare(ctx, sqlc.CreateQueryShareParams{
		Token:       share.Token,
		SourceID:    int64(share.SourceID),
		TeamID:      teamID,
		CreatedBy:   int64(share.CreatedBy),
		PayloadJson: string(share.Payload),
		ExpiresAt:   share.ExpiresAt,
	})
	if err != nil {
		db.log.Error("failed to create query share", "error", err, "source_id", share.SourceID)
		return fmt.Errorf("error creating query share: %w", err)
	}
	return nil
}

// GetQueryShare retrieves an ad hoc query share by token.
func (db *DB) GetQueryShare(ctx context.Context, token string) (*models.QueryShare, error) {
	row, err := db.readQueries.GetQueryShare(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		db.log.Error("failed to get query share", "error", err, "token", token)
		return nil, fmt.Errorf("error getting query share: %w", err)
	}

	share := &models.QueryShare{
		Token:          row.Token,
		SourceID:       models.SourceID(row.SourceID),
		CreatedBy:      models.UserID(row.CreatedBy),
		Payload:        []byte(row.PayloadJson),
		ExpiresAt:      row.ExpiresAt,
		CreatedAt:      row.CreatedAt,
		CreatedByEmail: row.Email,
		CreatedByName:  row.FullName,
	}
	if row.TeamID.Valid {
		tid := models.TeamID(row.TeamID.Int64)
		share.TeamID = &tid
	}
	if row.LastAccessedAt.Valid {
		share.LastAccessedAt = &row.LastAccessedAt.Time
	}

	return share, nil
}

// TouchQueryShare updates the last access timestamp.
func (db *DB) TouchQueryShare(ctx context.Context, token string, accessedAt time.Time) error {
	if err := db.writeQueries.TouchQueryShare(ctx, sqlc.TouchQueryShareParams{
		LastAccessedAt: sql.NullTime{Time: accessedAt, Valid: true},
		Token:          token,
	}); err != nil {
		db.log.Error("failed to touch query share", "error", err, "token", token)
		return fmt.Errorf("error touching query share: %w", err)
	}
	return nil
}

// DeleteQueryShare removes a query share by token.
func (db *DB) DeleteQueryShare(ctx context.Context, token string) error {
	if _, err := db.writeQueries.DeleteQueryShare(ctx, token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		db.log.Error("failed to delete query share", "error", err, "token", token)
		return fmt.Errorf("error deleting query share: %w", err)
	}
	return nil
}

// GetUserTeamForSource returns a team ID that the user belongs to and that has access to the source.
func (db *DB) GetUserTeamForSource(ctx context.Context, userID models.UserID, sourceID models.SourceID) (models.TeamID, error) {
	teamID, err := db.readQueries.GetUserTeamForSource(ctx, sqlc.GetUserTeamForSourceParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		return 0, translateNotFound(err)
	}
	return models.TeamID(teamID), nil
}

// PruneExpiredQueryShares removes expired query shares.
func (db *DB) PruneExpiredQueryShares(ctx context.Context, before time.Time) error {
	if err := db.writeQueries.PruneExpiredQueryShares(ctx, before); err != nil {
		db.log.Error("failed to prune expired query shares", "error", err)
		return fmt.Errorf("error pruning expired query shares: %w", err)
	}
	return nil
}
