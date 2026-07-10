package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateSession inserts a new session record.
func (s *Store) CreateSession(ctx context.Context, session *models.Session) error {
	err := s.q.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:        string(session.ID),
		UserID:    int64(session.UserID),
		ExpiresAt: ts(session.ExpiresAt),
		CreatedAt: ts(time.Now()),
	})
	if err != nil {
		s.log.Error("failed to create session record in db", "error", err, "session_id", session.ID, "user_id", session.UserID)
		return fmt.Errorf("error creating session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by ID. Returns models.ErrNotFound if absent.
func (s *Store) GetSession(ctx context.Context, sessionID models.SessionID) (*models.Session, error) {
	row, err := s.q.GetSession(ctx, string(sessionID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting session id %s: %w", sessionID, err)
	}
	return &models.Session{
		ID:        models.SessionID(row.ID),
		UserID:    models.UserID(row.UserID),
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// DeleteSession removes a session by ID (no error if it did not exist).
func (s *Store) DeleteSession(ctx context.Context, sessionID models.SessionID) error {
	if err := s.q.DeleteSession(ctx, string(sessionID)); err != nil {
		s.log.Error("failed to delete session record from db", "error", err, "session_id", sessionID)
		return fmt.Errorf("error deleting session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes all sessions for a user.
func (s *Store) DeleteUserSessions(ctx context.Context, userID models.UserID) error {
	if err := s.q.DeleteUserSessions(ctx, int64(userID)); err != nil {
		s.log.Error("failed to delete user sessions from db", "error", err, "user_id", userID)
		return fmt.Errorf("error deleting user sessions: %w", err)
	}
	return nil
}

// CountUserSessions counts a user's currently active (non-expired) sessions.
func (s *Store) CountUserSessions(ctx context.Context, userID models.UserID) (int, error) {
	count, err := s.q.CountUserSessions(ctx, sqlc.CountUserSessionsParams{
		UserID:    int64(userID),
		ExpiresAt: ts(time.Now()),
	})
	if err != nil {
		s.log.Error("failed to count user sessions in db", "error", err, "user_id", userID)
		return 0, fmt.Errorf("error counting user sessions: %w", err)
	}
	return int(count), nil
}

// DeleteExpiredSessions removes all sessions whose expiry is at or before the
// given time. Used by the periodic session sweeper.
func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	if err := s.q.DeleteExpiredSessions(ctx, ts(before)); err != nil {
		s.log.Error("failed to delete expired sessions from db", "error", err)
		return fmt.Errorf("error deleting expired sessions: %w", err)
	}
	return nil
}
