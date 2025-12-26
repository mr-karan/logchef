package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// TokenPrefix is the prefix for all API tokens
	TokenPrefix = "logchef_"
	// TokenLength is the length of the random part of the token (32 characters)
	TokenLength = 32
	// TokenPrefixLength is the length of the prefix shown to users
	TokenPrefixLength = 8
)

var (
	// ErrAPITokenNotFound is returned when an API token is not found
	ErrAPITokenNotFound = errors.New("API token not found")
	// ErrInvalidToken is returned when a token format is invalid
	ErrInvalidToken = errors.New("invalid token format")
	// ErrTokenExpired is returned when a token has expired
	ErrTokenExpired = errors.New("token has expired")
)

// generateAPIToken generates a new API token with the format "logchef_<userID>_" + 32 random characters
func generateAPIToken(userID models.UserID) (string, error) {
	// Generate 16 random bytes (32 hex characters)
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	// Convert to hex and create token with user ID embedded
	randomPart := hex.EncodeToString(bytes)
	token := fmt.Sprintf("%s%d_%s", TokenPrefix, userID, randomPart)

	return token, nil
}

// hashAPIToken creates a SHA256 hash of the token with the configured secret
func hashAPIToken(token, secret string) string {
	hasher := sha256.New()
	hasher.Write([]byte(token + secret))
	return hex.EncodeToString(hasher.Sum(nil))
}

// validateAPITokenCreation validates parameters for creating a new API token
func validateAPITokenCreation(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "token name is required"}
	}
	if len(name) < 2 || len(name) > 100 {
		return &ValidationError{Field: "name", Message: "token name must be between 2 and 100 characters"}
	}
	return nil
}

// CreateAPIToken creates a new API token for a user
func CreateAPIToken(ctx context.Context, db *sqlite.DB, log *slog.Logger, authCfg *config.AuthConfig, userID models.UserID, name string, expiresAt *time.Time) (*models.CreateAPITokenResponse, error) {
	// Validate input
	if err := validateAPITokenCreation(name); err != nil {
		return nil, err
	}

	// Verify user exists
	_, err := GetUser(ctx, db, userID)
	if err != nil {
		return nil, err
	}

	// Generate token
	token, err := generateAPIToken(userID)
	if err != nil {
		log.Error("failed to generate API token", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Hash token with configured secret for secure storage
	tokenHash := hashAPIToken(token, authCfg.APITokenSecret)

	// Create prefix for display (first 12 characters)
	prefix := token
	if len(prefix) > 12 {
		prefix = prefix[:12] + "..."
	}

	// Convert expiresAt to sql.NullTime
	var sqlExpiresAt sql.NullTime
	if expiresAt != nil {
		sqlExpiresAt = sql.NullTime{Time: *expiresAt, Valid: true}
	}

	// Save to database
	tokenID, err := db.CreateAPIToken(ctx, sqlc.CreateAPITokenParams{
		UserID:    int64(userID),
		Name:      name,
		TokenHash: tokenHash,
		Prefix:    prefix,
		ExpiresAt: sqlExpiresAt,
	})
	if err != nil {
		log.Error("failed to create API token in database", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}

	// Get the created token
	apiToken, err := GetAPIToken(ctx, db, int(tokenID))
	if err != nil {
		log.Error("failed to retrieve created API token", "error", err, "token_id", tokenID)
		return nil, fmt.Errorf("failed to retrieve created token: %w", err)
	}

	log.Info("API token created successfully", "token_id", tokenID, "user_id", userID, "name", name)

	return &models.CreateAPITokenResponse{
		Token:    token,
		APIToken: apiToken,
	}, nil
}

// GetAPIToken retrieves an API token by ID
func GetAPIToken(ctx context.Context, db *sqlite.DB, tokenID int) (*models.APIToken, error) {
	sqlcToken, err := db.GetAPIToken(ctx, int64(tokenID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAPITokenNotFound
		}
		return nil, fmt.Errorf("error getting API token from database: %w", err)
	}

	return convertSQLCAPITokenToModel(sqlcToken), nil
}

// ListAPITokensForUser lists all API tokens for a specific user
func ListAPITokensForUser(ctx context.Context, db *sqlite.DB, userID models.UserID) ([]*models.APIToken, error) {
	sqlcTokens, err := db.ListAPITokensForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("error listing API tokens for user: %w", err)
	}

	tokens := make([]*models.APIToken, len(sqlcTokens))
	for i := range sqlcTokens {
		tokens[i] = convertSQLCAPITokenToModel(sqlcTokens[i])
	}

	return tokens, nil
}

// DeleteAPIToken deletes an API token by ID, ensuring the user owns it
func DeleteAPIToken(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID, tokenID int) error {
	// Verify the token exists and belongs to the user
	token, err := GetAPIToken(ctx, db, tokenID)
	if err != nil {
		return err
	}

	if token.UserID != userID {
		return ErrAPITokenNotFound // Don't reveal that token exists for security
	}

	// Delete the token
	if err := db.DeleteAPIToken(ctx, sqlc.DeleteAPITokenParams{
		ID:     int64(tokenID),
		UserID: int64(userID),
	}); err != nil {
		log.Error("failed to delete API token", "error", err, "token_id", tokenID, "user_id", userID)
		return fmt.Errorf("failed to delete API token: %w", err)
	}

	log.Info("API token deleted successfully", "token_id", tokenID, "user_id", userID)
	return nil
}

// AuthenticateAPIToken authenticates a token and returns the associated user
func AuthenticateAPIToken(ctx context.Context, db *sqlite.DB, log *slog.Logger, authCfg *config.AuthConfig, token string) (*models.User, *models.APIToken, error) {
	// Validate basic token format
	if !hasTokenPrefix(token) {
		return nil, nil, ErrInvalidToken
	}

	// Hash the incoming token for lookup
	tokenHash := hashAPIToken(token, authCfg.APITokenSecret)

	// Direct lookup by token hash (very efficient with unique index)
	sqlcToken, err := db.GetAPITokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Check if token is expired
	if sqlcToken.ExpiresAt.Valid && time.Now().After(sqlcToken.ExpiresAt.Time) {
		return nil, nil, ErrTokenExpired
	}

	// Get associated user
	user, err := GetUser(ctx, db, models.UserID(sqlcToken.UserID))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user for token: %w", err)
	}

	// Check if user is active
	if user.Status != models.UserStatusActive {
		return nil, nil, ErrInvalidToken
	}

	apiToken := convertSQLCAPITokenToModel(sqlcToken)
	//nolint:contextcheck // Background goroutine for async last-used update
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = UpdateAPITokenLastUsed(ctx, db, int(sqlcToken.ID))
	}()

	return user, apiToken, nil
}

// UpdateAPITokenLastUsed updates the last used timestamp for an API token
func UpdateAPITokenLastUsed(ctx context.Context, db *sqlite.DB, tokenID int) error {
	return db.UpdateAPITokenLastUsed(ctx, int64(tokenID))
}

// CleanupExpiredTokens removes all expired API tokens
func CleanupExpiredTokens(ctx context.Context, db *sqlite.DB, log *slog.Logger) error {
	if err := db.DeleteExpiredAPITokens(ctx); err != nil {
		log.Error("failed to cleanup expired API tokens", "error", err)
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	return nil
}

// Helper functions

func hasTokenPrefix(token string) bool {
	if len(token) < len(TokenPrefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token[:len(TokenPrefix)]), []byte(TokenPrefix)) == 1
}

func convertSQLCAPITokenToModel(sqlcToken sqlc.ApiToken) *models.APIToken {
	token := &models.APIToken{
		ID:     int(sqlcToken.ID),
		UserID: models.UserID(sqlcToken.UserID),
		Name:   sqlcToken.Name,
		Prefix: sqlcToken.Prefix,
		Timestamps: models.Timestamps{
			CreatedAt: sqlcToken.CreatedAt,
			UpdatedAt: sqlcToken.UpdatedAt,
		},
	}

	if sqlcToken.LastUsedAt.Valid {
		token.LastUsedAt = &sqlcToken.LastUsedAt.Time
	}

	if sqlcToken.ExpiresAt.Valid {
		token.ExpiresAt = &sqlcToken.ExpiresAt.Time
	}

	return token
}
