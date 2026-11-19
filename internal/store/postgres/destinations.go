package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Store) CreateNotificationDestination(ctx context.Context, d *models.NotificationDestination) error {
	config, err := json.Marshal(d.Config)
	if err != nil {
		return fmt.Errorf("failed to encode destination config: %w", err)
	}
	row, err := s.q.CreateNotificationDestination(ctx, sqlc.CreateNotificationDestinationParams{
		Name:       d.Name,
		Type:       string(d.Type),
		Scope:      string(d.Scope),
		TeamID:     nullTeamID(d.TeamID),
		ConfigJson: string(config),
		Secret:     d.Secret,
		IsActive:   d.IsActive,
		CreatedBy:  nullUserID(d.CreatedBy),
	})
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	created, err := destinationFromSQLC(row)
	if err != nil {
		return err
	}
	*d = *created
	return nil
}

func (s *Store) UpdateNotificationDestination(ctx context.Context, d *models.NotificationDestination) error {
	config, err := json.Marshal(d.Config)
	if err != nil {
		return fmt.Errorf("failed to encode destination config: %w", err)
	}
	row, err := s.q.UpdateNotificationDestination(ctx, sqlc.UpdateNotificationDestinationParams{
		Name:       d.Name,
		Scope:      string(d.Scope),
		TeamID:     nullTeamID(d.TeamID),
		ConfigJson: string(config),
		Secret:     d.Secret,
		IsActive:   d.IsActive,
		ID:         int64(d.ID),
	})
	if err != nil {
		return fmt.Errorf("failed to update destination: %w", translateNotFoundErr(err))
	}
	updated, err := destinationFromSQLC(row)
	if err != nil {
		return err
	}
	*d = *updated
	return nil
}

func (s *Store) DeleteNotificationDestination(ctx context.Context, id models.DestinationID) error {
	n, err := s.q.DeleteNotificationDestination(ctx, int64(id))
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: a rule uses this destination", models.ErrConflict)
		}
		return fmt.Errorf("failed to delete destination: %w", err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (s *Store) GetNotificationDestination(ctx context.Context, id models.DestinationID) (*models.NotificationDestination, error) {
	row, err := s.q.GetNotificationDestination(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("failed to get destination: %w", translateNotFoundErr(err))
	}
	return destinationFromSQLC(row)
}

func (s *Store) ListNotificationDestinations(ctx context.Context) ([]*models.NotificationDestination, error) {
	rows, err := s.q.ListNotificationDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

func (s *Store) ListUsableNotificationDestinations(ctx context.Context, userID models.UserID) ([]*models.NotificationDestination, error) {
	rows, err := s.q.ListUsableNotificationDestinations(ctx, pgtype.Int8{Int64: int64(userID), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

func (s *Store) CountTeamNotificationDestinations(ctx context.Context, teamID models.TeamID) (int, error) {
	n, err := s.q.CountTeamNotificationDestinations(ctx, pgtype.Int8{Int64: int64(teamID), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("failed to count team destinations: %w", err)
	}
	return int(n), nil
}

func (s *Store) CountAlertsUsingDestination(ctx context.Context, id models.DestinationID) (int, error) {
	n, err := s.q.CountAlertsUsingDestination(ctx, int64(id))
	if err != nil {
		return 0, fmt.Errorf("failed to count rules using destination: %w", err)
	}
	return int(n), nil
}

func (s *Store) SetAlertDestinations(ctx context.Context, alertID models.AlertID, ids []models.DestinationID) error {
	if err := s.q.DeleteAlertDestinations(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to clear alert destinations: %w", err)
	}
	for _, id := range ids {
		if err := s.q.AddAlertDestination(ctx, sqlc.AddAlertDestinationParams{AlertID: int64(alertID), DestinationID: int64(id)}); err != nil {
			return fmt.Errorf("failed to link destination %d: %w", id, err)
		}
	}
	return nil
}

func (s *Store) ListAlertDestinations(ctx context.Context, alertID models.AlertID) ([]*models.NotificationDestination, error) {
	rows, err := s.q.ListAlertDestinations(ctx, int64(alertID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alert destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

// attachDestinationIDs fills DestinationIDs of the alerts with one query.
func (s *Store) attachDestinationIDs(ctx context.Context, alerts ...*models.Alert) error {
	if len(alerts) == 0 {
		return nil
	}
	ids := make([]int64, len(alerts))
	byID := make(map[int64]*models.Alert, len(alerts))
	for i, alert := range alerts {
		ids[i] = int64(alert.ID)
		byID[ids[i]] = alert
		alert.DestinationIDs = []models.DestinationID{}
	}
	links, err := s.q.ListAlertDestinationLinks(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to list alert destinations: %w", err)
	}
	for _, link := range links {
		alert := byID[link.AlertID]
		alert.DestinationIDs = append(alert.DestinationIDs, models.DestinationID(link.DestinationID))
	}
	return nil
}

func destinationsFromSQLC(rows []sqlc.NotificationDestination) ([]*models.NotificationDestination, error) {
	out := make([]*models.NotificationDestination, 0, len(rows))
	for i := range rows {
		d, err := destinationFromSQLC(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func destinationFromSQLC(row sqlc.NotificationDestination) (*models.NotificationDestination, error) {
	d := &models.NotificationDestination{
		ID:        models.DestinationID(row.ID),
		Name:      row.Name,
		Type:      models.DestinationType(row.Type),
		Scope:     models.DestinationScope(row.Scope),
		Secret:    row.Secret,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if err := json.Unmarshal([]byte(row.ConfigJson), &d.Config); err != nil {
		return nil, fmt.Errorf("failed to decode config of destination %d: %w", row.ID, err)
	}
	if row.TeamID.Valid {
		teamID := models.TeamID(row.TeamID.Int64)
		d.TeamID = &teamID
	}
	if row.CreatedBy.Valid {
		createdBy := models.UserID(row.CreatedBy.Int64)
		d.CreatedBy = &createdBy
	}
	d.HasSecret = d.Secret != ""
	d.SecretHint = models.DestinationSecretHint(d.Type, d.Secret)
	return d, nil
}

func nullTeamID(id *models.TeamID) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*id), Valid: true}
}

func nullUserID(id *models.UserID) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*id), Valid: true}
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func translateNotFoundErr(err error) error {
	if notFound(err) {
		return models.ErrNotFound
	}
	return err
}
