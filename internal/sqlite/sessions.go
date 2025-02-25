package sqlite

import (
	"context"
	"fmt"
	"time"

	"logchef/pkg/models"
)

// Session methods

// CreateSession creates a new session
func (db *DB) CreateSession(ctx context.Context, session *models.Session) error {
	db.log.Debug("creating session", "session_id", session.ID, "user_id", session.UserID)

	result, err := db.queries.CreateSession.ExecContext(ctx,
		session.ID,
		session.UserID,
		session.ExpiresAt,
		time.Now(),
	)
	if err != nil {
		db.log.Error("failed to create session",
			"error", err,
			"session_id", session.ID,
			"user_id", session.UserID,
		)
		return fmt.Errorf("error creating session: %w", err)
	}

	if err := checkRowsAffected(result, "create session"); err != nil {
		db.log.Error("unexpected rows affected",
			"error", err,
			"session_id", session.ID,
		)
		return err
	}

	return nil
}

// GetSession retrieves a session by ID
func (db *DB) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	db.log.Debug("getting session", "session_id", sessionID)

	var session models.Session
	err := db.queries.GetSession.GetContext(ctx, &session, sessionID)
	if err != nil {
		db.log.Error("failed to get session",
			"error", err,
			"session_id", sessionID,
		)
		return nil, handleNotFoundError(err, "error getting session")
	}
	return &session, nil
}

// DeleteSession deletes a session by ID
func (db *DB) DeleteSession(ctx context.Context, sessionID string) error {
	db.log.Debug("deleting session", "session_id", sessionID)

	result, err := db.queries.DeleteSession.ExecContext(ctx, sessionID)
	if err != nil {
		db.log.Error("failed to delete session",
			"error", err,
			"session_id", sessionID,
		)
		return fmt.Errorf("error deleting session: %w", err)
	}

	if err := checkRowsAffected(result, "delete session"); err != nil {
		db.log.Error("unexpected rows affected",
			"error", err,
			"session_id", sessionID,
		)
		return err
	}

	return nil
}

// DeleteUserSessions deletes all sessions for a user
func (db *DB) DeleteUserSessions(ctx context.Context, userID string) error {
	db.log.Debug("deleting user sessions", "user_id", userID)

	_, err := db.queries.DeleteUserSessions.ExecContext(ctx, userID)
	if err != nil {
		db.log.Error("failed to delete user sessions",
			"error", err,
			"user_id", userID,
		)
		return fmt.Errorf("error deleting user sessions: %w", err)
	}
	return nil
}

// CountUserSessions returns the number of active sessions for a user
func (db *DB) CountUserSessions(ctx context.Context, userID string) (int, error) {
	db.log.Debug("counting user sessions", "user_id", userID)

	var count int
	err := db.queries.CountUserSessions.GetContext(ctx, &count, userID, time.Now())
	if err != nil {
		db.log.Error("failed to count user sessions",
			"error", err,
			"user_id", userID,
		)
		return 0, fmt.Errorf("error counting user sessions: %w", err)
	}

	db.log.Debug("user sessions counted", "user_id", userID, "count", count)
	return count, nil
}
