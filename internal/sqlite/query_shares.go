package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateQueryShare persists an ad hoc query share token.
func (db *DB) CreateQueryShare(ctx context.Context, share *models.QueryShare) error {
	err := db.writeQueries.CreateQueryShare(ctx, sqlc.CreateQueryShareParams{
		Token:       share.Token,
		TeamID:      int64(share.TeamID),
		SourceID:    int64(share.SourceID),
		CreatedBy:   int64(share.CreatedBy),
		PayloadJson: string(share.Payload),
		ExpiresAt:   share.ExpiresAt,
	})
	if err != nil {
		db.log.Error("failed to create query share", "error", err, "team_id", share.TeamID, "source_id", share.SourceID)
		return fmt.Errorf("error creating query share: %w", err)
	}
	return nil
}

// GetQueryShare retrieves an ad hoc query share by token.
func (db *DB) GetQueryShare(ctx context.Context, token string) (*models.QueryShare, error) {
	row, err := db.readQueries.GetQueryShare(ctx, token)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		db.log.Error("failed to get query share", "error", err, "token", token)
		return nil, fmt.Errorf("error getting query share: %w", err)
	}

	share := &models.QueryShare{
		Token:          row.Token,
		TeamID:         models.TeamID(row.TeamID),
		SourceID:       models.SourceID(row.SourceID),
		CreatedBy:      models.UserID(row.CreatedBy),
		Payload:        []byte(row.PayloadJson),
		ExpiresAt:      row.ExpiresAt,
		CreatedAt:      row.CreatedAt,
		CreatedByEmail: row.Email,
		CreatedByName:  row.FullName,
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
		if err == sql.ErrNoRows {
			return err
		}
		db.log.Error("failed to delete query share", "error", err, "token", token)
		return fmt.Errorf("error deleting query share: %w", err)
	}
	return nil
}

// PruneExpiredQueryShares removes expired query shares.
func (db *DB) PruneExpiredQueryShares(ctx context.Context, before time.Time) error {
	if err := db.writeQueries.PruneExpiredQueryShares(ctx, before); err != nil {
		db.log.Error("failed to prune expired query shares", "error", err)
		return fmt.Errorf("error pruning expired query shares: %w", err)
	}
	return nil
}
