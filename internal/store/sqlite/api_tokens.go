package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// API token persistence. These methods own the translation between
// models.APIToken and the stored representation (scopes as a JSON column,
// nullable timestamps), so callers never see sqlc or driver types.

// CreateAPIToken inserts a new API token and returns its generated ID. The
// caller supplies a fully-formed model (hash, prefix, scopes, expiry already
// set); scope serialization and null handling happen here.
func (db *DB) CreateAPIToken(ctx context.Context, token *models.APIToken) (int, error) {
	scopes, err := marshalTokenScopes(token.Scopes)
	if err != nil {
		return 0, err
	}

	id, err := db.writeQueries.CreateAPIToken(ctx, sqlc.CreateAPITokenParams{
		UserID:    int64(token.UserID),
		Name:      token.Name,
		TokenHash: token.TokenHash,
		Prefix:    token.Prefix,
		ExpiresAt: nullTime(token.ExpiresAt),
		Scopes:    scopes,
	})
	if err != nil {
		db.log.Error("failed to create API token record in db", "error", err, "user_id", token.UserID)
		return 0, fmt.Errorf("failed to create API token: %w", err)
	}
	return int(id), nil
}

// GetAPIToken retrieves an API token by ID.
func (db *DB) GetAPIToken(ctx context.Context, id int) (*models.APIToken, error) {
	row, err := db.readQueries.GetAPIToken(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		db.log.Error("failed to get API token from db", "error", err, "token_id", id)
		return nil, fmt.Errorf("failed to get API token: %w", err)
	}
	return apiTokenToModel(row), nil
}

// GetAPITokenByHash retrieves an API token by its hash (for authentication).
func (db *DB) GetAPITokenByHash(ctx context.Context, tokenHash string) (*models.APIToken, error) {
	row, err := db.readQueries.GetAPITokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		db.log.Error("failed to get API token by hash from db", "error", err)
		return nil, fmt.Errorf("failed to get API token by hash: %w", err)
	}
	return apiTokenToModel(row), nil
}

// ListAPITokensForUser retrieves all API tokens for a specific user.
func (db *DB) ListAPITokensForUser(ctx context.Context, userID models.UserID) ([]*models.APIToken, error) {
	rows, err := db.readQueries.ListAPITokensForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list API tokens for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to list API tokens for user: %w", err)
	}

	tokens := make([]*models.APIToken, len(rows))
	for i := range rows {
		row := rows[i]
		tokens[i] = apiTokenToModel(row)
	}
	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last-used timestamp for an API token.
func (db *DB) UpdateAPITokenLastUsed(ctx context.Context, id int) error {
	if err := db.writeQueries.UpdateAPITokenLastUsed(ctx, int64(id)); err != nil {
		db.log.Error("failed to update API token last used timestamp", "error", err, "token_id", id)
		return fmt.Errorf("failed to update API token last used: %w", err)
	}
	return nil
}

// DeleteAPIToken deletes an API token, scoped to its owner so a user cannot
// delete another user's token.
func (db *DB) DeleteAPIToken(ctx context.Context, id int, userID models.UserID) error {
	err := db.writeQueries.DeleteAPIToken(ctx, sqlc.DeleteAPITokenParams{
		ID:     int64(id),
		UserID: int64(userID),
	})
	if err != nil {
		db.log.Error("failed to delete API token from db", "error", err, "token_id", id, "user_id", userID)
		return fmt.Errorf("failed to delete API token: %w", err)
	}
	return nil
}

// DeleteExpiredAPITokens removes all expired API tokens.
func (db *DB) DeleteExpiredAPITokens(ctx context.Context) error {
	if err := db.writeQueries.DeleteExpiredAPITokens(ctx); err != nil {
		db.log.Error("failed to delete expired API tokens from db", "error", err)
		return fmt.Errorf("failed to delete expired API tokens: %w", err)
	}
	return nil
}

// apiTokenToModel maps a stored row to the backend-neutral model, decoding the
// scopes JSON and computing the Expired flag from the current clock.
func apiTokenToModel(row sqlc.ApiToken) *models.APIToken {
	token := &models.APIToken{
		ID:     int(row.ID),
		UserID: models.UserID(row.UserID),
		Name:   row.Name,
		Prefix: row.Prefix,
		Scopes: unmarshalTokenScopes(row.Scopes),
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
	}
	if row.LastUsedAt.Valid {
		token.LastUsedAt = &row.LastUsedAt.Time
	}
	if row.ExpiresAt.Valid {
		token.ExpiresAt = &row.ExpiresAt.Time
		token.Expired = time.Now().After(row.ExpiresAt.Time)
	}
	return token
}

func marshalTokenScopes(scopes []models.TokenScope) (string, error) {
	b, err := json.Marshal(scopes)
	if err != nil {
		return "", fmt.Errorf("failed to encode token scopes: %w", err)
	}
	return string(b), nil
}

func unmarshalTokenScopes(raw string) []models.TokenScope {
	if raw == "" {
		return []models.TokenScope{}
	}
	var scopes []models.TokenScope
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return []models.TokenScope{}
	}
	return scopes
}

// nullTime converts an optional time into a driver sql.NullTime.
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
