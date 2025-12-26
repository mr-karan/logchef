package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// Session-specific errors
var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

// ValidateSession checks if a session ID is valid and not expired.
// It returns the session details if valid, or an appropriate error.
func ValidateSession(ctx context.Context, db *sqlite.DB, log *slog.Logger, sessionID models.SessionID) (*models.Session, error) {
	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		// Check if the specific DB error indicates not found
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) { // Assuming db might return models.ErrNotFound too
			log.Warn("session not found in db", "session_id", sessionID)
			return nil, ErrSessionNotFound
		}
		// Log other unexpected DB errors
		log.Error("error retrieving session from db", "error", err, "session_id", sessionID)
		return nil, fmt.Errorf("database error validating session: %w", err)
	}

	// Double check for nil just in case db layer doesn't return ErrNotFound
	if session == nil {
		log.Warn("session not found in db (nil returned)", "session_id", sessionID)
		return nil, ErrSessionNotFound
	}

	// Check for expiration
	if time.Now().After(session.ExpiresAt) {
		log.Info("session expired", "session_id", sessionID, "expires_at", session.ExpiresAt)
		// Attempt to delete the expired session (best effort)
		if delErr := db.DeleteSession(ctx, sessionID); delErr != nil {
			log.Warn("failed to delete expired session", "error", delErr, "session_id", sessionID)
		}
		return nil, ErrSessionExpired
	}

	return session, nil
}

// CreateSession creates a new session for a user, respecting concurrent session limits.
func CreateSession(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID, duration time.Duration, maxConcurrent int) (*models.Session, error) {

	// Check concurrent sessions before creating a new one
	sessionCount, err := db.CountUserSessions(ctx, userID)
	if err != nil {
		log.Error("failed to count user sessions before creation", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to count user sessions: %w", err)
	}

	// If limit is reached or exceeded, delete existing sessions
	if sessionCount >= maxConcurrent {
		log.Info("deleting existing sessions due to max concurrent sessions limit",
			"user_id", userID,
			"current_sessions", sessionCount,
			"max_sessions", maxConcurrent,
		)
		if err := RevokeUserSessions(ctx, db, log, userID); err != nil {
			// Don't necessarily fail the login, but log the error
			log.Error("failed to delete existing user sessions while enforcing limit", "error", err, "user_id", userID)
			return nil, fmt.Errorf("failed to delete existing user sessions: %w", err)
		}
	}

	// Create new session details
	sessionID := uuid.New().String()
	session := &models.Session{
		ID:        models.SessionID(sessionID),
		UserID:    userID,
		ExpiresAt: time.Now().Add(duration),
	}

	// Save session to database
	if err := db.CreateSession(ctx, session); err != nil {
		log.Error("failed to create session in db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	log.Info("new session created successfully", "session_id", session.ID, "user_id", userID)
	return session, nil
}

// RevokeSession deletes a specific session by its ID.
func RevokeSession(ctx context.Context, db *sqlite.DB, log *slog.Logger, sessionID models.SessionID) error {
	log.Info("revoking session", "session_id", sessionID)
	if err := db.DeleteSession(ctx, sessionID); err != nil {
		// Check if it was already not found
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "session not found") { // Adapt based on actual DB error
			log.Warn("attempted to revoke session that was not found", "session_id", sessionID)
			return nil // Not an error if it didn't exist
		}
		log.Error("failed to delete session from db", "error", err, "session_id", sessionID)
		return fmt.Errorf("database error revoking session: %w", err)
	}
	log.Info("session revoked successfully", "session_id", sessionID)
	return nil
}

// RevokeUserSessions deletes all sessions for a specific user.
func RevokeUserSessions(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID) error {
	log.Info("revoking all sessions for user", "user_id", userID)
	if err := db.DeleteUserSessions(ctx, userID); err != nil {
		log.Error("failed to delete user sessions from db", "error", err, "user_id", userID)
		return fmt.Errorf("database error revoking user sessions: %w", err)
	}
	log.Info("all sessions revoked for user", "user_id", userID)
	return nil
}
