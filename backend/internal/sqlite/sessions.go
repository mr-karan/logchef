package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Session methods

// CreateSession creates a new session
func (d *DB) CreateSession(ctx context.Context, session *models.Session) error {
	result, err := d.queries.CreateSession.ExecContext(ctx,
		session.ID,
		session.UserID,
		session.ExpiresAt,
		time.Now(),
	)
	if err != nil {
		d.log.Error("failed to create session",
			"error", err,
			"session_id", session.ID,
			"user_id", session.UserID,
		)
		return fmt.Errorf("error creating session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		d.log.Error("failed to get rows affected",
			"error", err,
			"session_id", session.ID,
		)
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		d.log.Error("unexpected rows affected",
			"rows_affected", rowsAffected,
			"session_id", session.ID,
		)
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetSession retrieves a session by ID
func (d *DB) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	var session models.Session
	err := d.queries.GetSession.GetContext(ctx, &session, sessionID)
	if err != nil {
		d.log.Error("failed to get session",
			"error", err,
			"session_id", sessionID,
		)
		return nil, fmt.Errorf("error getting session: %w", err)
	}
	return &session, nil
}

// DeleteSession deletes a session by ID
func (d *DB) DeleteSession(ctx context.Context, sessionID string) error {
	result, err := d.queries.DeleteSession.ExecContext(ctx, sessionID)
	if err != nil {
		d.log.Error("failed to delete session",
			"error", err,
			"session_id", sessionID,
		)
		return fmt.Errorf("error deleting session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		d.log.Error("failed to get rows affected",
			"error", err,
			"session_id", sessionID,
		)
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		d.log.Error("unexpected rows affected",
			"rows_affected", rowsAffected,
			"session_id", sessionID,
		)
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// DeleteUserSessions deletes all sessions for a user
func (d *DB) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := d.queries.DeleteUserSessions.ExecContext(ctx, userID)
	if err != nil {
		d.log.Error("failed to delete user sessions",
			"error", err,
			"user_id", userID,
		)
		return fmt.Errorf("error deleting user sessions: %w", err)
	}
	return nil
}

// CountUserSessions returns the number of active sessions for a user
func (d *DB) CountUserSessions(ctx context.Context, userID string) (int, error) {
	var count int
	err := d.queries.CountUserSessions.GetContext(ctx, &count, userID, time.Now())
	if err != nil {
		d.log.Error("failed to count user sessions",
			"error", err,
			"user_id", userID,
		)
		return 0, fmt.Errorf("error counting user sessions: %w", err)
	}
	return count, nil
}
