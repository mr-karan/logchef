package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Session methods

// CreateSession creates a new session
func (db *DB) CreateSession(ctx context.Context, session *models.Session) error {
	db.log.Debug("creating session", "session_id", session.ID, "user_id", session.UserID)

	err := db.queries.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:        string(session.ID),
		UserID:    int64(session.UserID),
		ExpiresAt: session.ExpiresAt,
		CreatedAt: time.Now(),
	})
	if err != nil {
		db.log.Error("failed to create session",
			"error", err,
			"session_id", session.ID,
			"user_id", session.UserID,
		)
		return fmt.Errorf("error creating session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID
func (db *DB) GetSession(ctx context.Context, sessionID models.SessionID) (*models.Session, error) {
	db.log.Debug("getting session", "session_id", sessionID)

	sqlcSession, err := db.queries.GetSession(ctx, string(sessionID))
	if err != nil {
		db.log.Error("failed to get session",
			"error", err,
			"session_id", sessionID,
		)
		return nil, handleNotFoundError(err, "error getting session")
	}

	session := &models.Session{
		ID:        models.SessionID(sqlcSession.ID),
		UserID:    models.UserID(sqlcSession.UserID),
		ExpiresAt: sqlcSession.ExpiresAt,
		CreatedAt: sqlcSession.CreatedAt,
	}

	return session, nil
}

// DeleteSession deletes a session by ID
func (db *DB) DeleteSession(ctx context.Context, sessionID models.SessionID) error {
	db.log.Debug("deleting session", "session_id", sessionID)

	err := db.queries.DeleteSession(ctx, string(sessionID))
	if err != nil {
		db.log.Error("failed to delete session",
			"error", err,
			"session_id", sessionID,
		)
		return fmt.Errorf("error deleting session: %w", err)
	}

	return nil
}

// DeleteUserSessions deletes all sessions for a user
func (db *DB) DeleteUserSessions(ctx context.Context, userID models.UserID) error {
	db.log.Debug("deleting user sessions", "user_id", userID)

	err := db.queries.DeleteUserSessions(ctx, int64(userID))
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
func (db *DB) CountUserSessions(ctx context.Context, userID models.UserID) (int, error) {
	db.log.Debug("counting user sessions", "user_id", userID)

	count, err := db.queries.CountUserSessions(ctx, sqlc.CountUserSessionsParams{
		UserID:    int64(userID),
		ExpiresAt: time.Now(),
	})
	if err != nil {
		db.log.Error("failed to count user sessions",
			"error", err,
			"user_id", userID,
		)
		return 0, fmt.Errorf("error counting user sessions: %w", err)
	}

	db.log.Debug("user sessions counted", "user_id", userID, "count", int(count))
	return int(count), nil
}
