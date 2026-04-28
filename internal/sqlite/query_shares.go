package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const createQueryShareSQL = `
INSERT INTO query_shares (
    token,
    team_id,
    source_id,
    created_by,
    payload_json,
    expires_at
) VALUES (?, ?, ?, ?, ?, ?)
`

const getQueryShareSQL = `
SELECT
    qs.token,
    qs.team_id,
    qs.source_id,
    qs.created_by,
    qs.payload_json,
    qs.expires_at,
    qs.last_accessed_at,
    qs.created_at,
    u.email,
    u.full_name
FROM query_shares qs
JOIN users u ON u.id = qs.created_by
WHERE qs.token = ?
`

const touchQueryShareSQL = `
UPDATE query_shares
SET last_accessed_at = ?
WHERE token = ?
`

const deleteQueryShareSQL = `
DELETE FROM query_shares
WHERE token = ?
`

const pruneExpiredQuerySharesSQL = `
DELETE FROM query_shares
WHERE expires_at < ?
`

// CreateQueryShare persists an ad hoc query share token.
func (db *DB) CreateQueryShare(ctx context.Context, share *models.QueryShare) error {
	_, err := db.writeDB.ExecContext(ctx,
		createQueryShareSQL,
		share.Token,
		int64(share.TeamID),
		int64(share.SourceID),
		int64(share.CreatedBy),
		string(share.Payload),
		share.ExpiresAt,
	)
	if err != nil {
		db.log.Error("failed to create query share", "error", err, "team_id", share.TeamID, "source_id", share.SourceID)
		return fmt.Errorf("error creating query share: %w", err)
	}
	return nil
}

// GetQueryShare retrieves an ad hoc query share by token.
func (db *DB) GetQueryShare(ctx context.Context, token string) (*models.QueryShare, error) {
	var (
		share        models.QueryShare
		payload      string
		lastAccessed sql.NullTime
		email        sql.NullString
		fullName     sql.NullString
	)

	err := db.readDB.QueryRowContext(ctx, getQueryShareSQL, token).Scan(
		&share.Token,
		&share.TeamID,
		&share.SourceID,
		&share.CreatedBy,
		&payload,
		&share.ExpiresAt,
		&lastAccessed,
		&share.CreatedAt,
		&email,
		&fullName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		db.log.Error("failed to get query share", "error", err, "token", token)
		return nil, fmt.Errorf("error getting query share: %w", err)
	}

	share.Payload = []byte(payload)
	if lastAccessed.Valid {
		share.LastAccessedAt = &lastAccessed.Time
	}
	if email.Valid {
		share.CreatedByEmail = email.String
	}
	if fullName.Valid {
		share.CreatedByName = fullName.String
	}

	return &share, nil
}

// TouchQueryShare updates the last access timestamp.
func (db *DB) TouchQueryShare(ctx context.Context, token string, accessedAt time.Time) error {
	if _, err := db.writeDB.ExecContext(ctx, touchQueryShareSQL, accessedAt, token); err != nil {
		db.log.Error("failed to touch query share", "error", err, "token", token)
		return fmt.Errorf("error touching query share: %w", err)
	}
	return nil
}

// DeleteQueryShare removes a query share by token.
func (db *DB) DeleteQueryShare(ctx context.Context, token string) error {
	res, err := db.writeDB.ExecContext(ctx, deleteQueryShareSQL, token)
	if err != nil {
		db.log.Error("failed to delete query share", "error", err, "token", token)
		return fmt.Errorf("error deleting query share: %w", err)
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// PruneExpiredQueryShares removes expired query shares.
func (db *DB) PruneExpiredQueryShares(ctx context.Context, before time.Time) error {
	if _, err := db.writeDB.ExecContext(ctx, pruneExpiredQuerySharesSQL, before); err != nil {
		db.log.Error("failed to prune expired query shares", "error", err)
		return fmt.Errorf("error pruning expired query shares: %w", err)
	}
	return nil
}
