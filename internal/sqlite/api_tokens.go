package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// API Token methods

// CreateAPIToken inserts a new API token record into the database.
func (db *DB) CreateAPIToken(ctx context.Context, params sqlc.CreateAPITokenParams) (int64, error) {

	id, err := db.queries.CreateAPIToken(ctx, params)
	if err != nil {
		db.log.Error("failed to create API token record in db", "error", err, "user_id", params.UserID)
		return 0, fmt.Errorf("failed to create API token: %w", err)
	}

	return id, nil
}

// GetAPIToken retrieves an API token by ID.
func (db *DB) GetAPIToken(ctx context.Context, id int64) (sqlc.ApiToken, error) {

	token, err := db.queries.GetAPIToken(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return sqlc.ApiToken{}, models.ErrNotFound
		}
		db.log.Error("failed to get API token from db", "error", err, "token_id", id)
		return sqlc.ApiToken{}, fmt.Errorf("failed to get API token: %w", err)
	}

	return token, nil
}

// GetAPITokenByHash retrieves an API token by its hash (for authentication).
func (db *DB) GetAPITokenByHash(ctx context.Context, tokenHash string) (sqlc.ApiToken, error) {

	apiToken, err := db.queries.GetAPITokenByHash(ctx, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return sqlc.ApiToken{}, models.ErrNotFound
		}
		db.log.Error("failed to get API token by hash from db", "error", err)
		return sqlc.ApiToken{}, fmt.Errorf("failed to get API token by hash: %w", err)
	}

	return apiToken, nil
}

// ListAPITokensForUser retrieves all API tokens for a specific user.
func (db *DB) ListAPITokensForUser(ctx context.Context, userID int64) ([]sqlc.ApiToken, error) {

	tokens, err := db.queries.ListAPITokensForUser(ctx, userID)
	if err != nil {
		db.log.Error("failed to list API tokens for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to list API tokens for user: %w", err)
	}

	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last used timestamp for an API token.
func (db *DB) UpdateAPITokenLastUsed(ctx context.Context, id int64) error {

	err := db.queries.UpdateAPITokenLastUsed(ctx, id)
	if err != nil {
		db.log.Error("failed to update API token last used timestamp", "error", err, "token_id", id)
		return fmt.Errorf("failed to update API token last used: %w", err)
	}

	return nil
}

// DeleteAPIToken deletes an API token by ID and user ID (ensures user owns the token).
func (db *DB) DeleteAPIToken(ctx context.Context, params sqlc.DeleteAPITokenParams) error {

	err := db.queries.DeleteAPIToken(ctx, params)
	if err != nil {
		db.log.Error("failed to delete API token from db", "error", err, "token_id", params.ID, "user_id", params.UserID)
		return fmt.Errorf("failed to delete API token: %w", err)
	}

	return nil
}

// DeleteExpiredAPITokens removes all expired API tokens.
func (db *DB) DeleteExpiredAPITokens(ctx context.Context) error {

	err := db.queries.DeleteExpiredAPITokens(ctx)
	if err != nil {
		db.log.Error("failed to delete expired API tokens from db", "error", err)
		return fmt.Errorf("failed to delete expired API tokens: %w", err)
	}

	return nil
}
