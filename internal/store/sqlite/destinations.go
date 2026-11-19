package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (db *DB) CreateNotificationDestination(ctx context.Context, d *models.NotificationDestination) error {
	config, err := json.Marshal(d.Config)
	if err != nil {
		return fmt.Errorf("failed to encode destination config: %w", err)
	}
	row, err := db.writeQueries.CreateNotificationDestination(ctx, sqlc.CreateNotificationDestinationParams{
		Name:       d.Name,
		Type:       string(d.Type),
		Scope:      string(d.Scope),
		TeamID:     nullTeamID(d.TeamID),
		ConfigJson: string(config),
		Secret:     d.Secret,
		IsActive:   boolToInt(d.IsActive),
		CreatedBy:  nullUserID(d.CreatedBy),
	})
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", translateBusy(err))
	}
	created, err := destinationFromSQLC(row)
	if err != nil {
		return err
	}
	*d = *created
	return nil
}

func (db *DB) UpdateNotificationDestination(ctx context.Context, d *models.NotificationDestination) error {
	config, err := json.Marshal(d.Config)
	if err != nil {
		return fmt.Errorf("failed to encode destination config: %w", err)
	}
	row, err := db.writeQueries.UpdateNotificationDestination(ctx, sqlc.UpdateNotificationDestinationParams{
		Name:       d.Name,
		Scope:      string(d.Scope),
		TeamID:     nullTeamID(d.TeamID),
		ConfigJson: string(config),
		Secret:     d.Secret,
		IsActive:   boolToInt(d.IsActive),
		ID:         int64(d.ID),
	})
	if err != nil {
		return fmt.Errorf("failed to update destination: %w", translateNotFound(translateBusy(err)))
	}
	updated, err := destinationFromSQLC(row)
	if err != nil {
		return err
	}
	*d = *updated
	return nil
}

func (db *DB) DeleteNotificationDestination(ctx context.Context, id models.DestinationID) error {
	n, err := db.writeQueries.DeleteNotificationDestination(ctx, int64(id))
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return fmt.Errorf("%w: a rule uses this destination", models.ErrConflict)
		}
		return fmt.Errorf("failed to delete destination: %w", translateBusy(err))
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (db *DB) GetNotificationDestination(ctx context.Context, id models.DestinationID) (*models.NotificationDestination, error) {
	row, err := db.readQueries.GetNotificationDestination(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("failed to get destination: %w", translateNotFound(err))
	}
	return destinationFromSQLC(row)
}

func (db *DB) ListNotificationDestinations(ctx context.Context) ([]*models.NotificationDestination, error) {
	rows, err := db.readQueries.ListNotificationDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

func (db *DB) ListUsableNotificationDestinations(ctx context.Context, userID models.UserID) ([]*models.NotificationDestination, error) {
	rows, err := db.readQueries.ListUsableNotificationDestinations(ctx, sql.NullInt64{Int64: int64(userID), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

func (db *DB) CountTeamNotificationDestinations(ctx context.Context, teamID models.TeamID) (int, error) {
	n, err := db.readQueries.CountTeamNotificationDestinations(ctx, sql.NullInt64{Int64: int64(teamID), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("failed to count team destinations: %w", err)
	}
	return int(n), nil
}

func (db *DB) CountAlertsUsingDestination(ctx context.Context, id models.DestinationID) (int, error) {
	n, err := db.readQueries.CountAlertsUsingDestination(ctx, int64(id))
	if err != nil {
		return 0, fmt.Errorf("failed to count rules using destination: %w", err)
	}
	return int(n), nil
}

func (db *DB) SetAlertDestinations(ctx context.Context, alertID models.AlertID, ids []models.DestinationID) error {
	if err := db.writeQueries.DeleteAlertDestinations(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to clear alert destinations: %w", translateBusy(err))
	}
	for _, id := range ids {
		if err := db.writeQueries.AddAlertDestination(ctx, sqlc.AddAlertDestinationParams{AlertID: int64(alertID), DestinationID: int64(id)}); err != nil {
			return fmt.Errorf("failed to link destination %d: %w", id, translateBusy(err))
		}
	}
	return nil
}

func (db *DB) ListAlertDestinations(ctx context.Context, alertID models.AlertID) ([]*models.NotificationDestination, error) {
	rows, err := db.readQueries.ListAlertDestinations(ctx, int64(alertID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alert destinations: %w", err)
	}
	return destinationsFromSQLC(rows)
}

// attachDestinationIDs fills DestinationIDs of the alerts with one query.
func (db *DB) attachDestinationIDs(ctx context.Context, alerts ...*models.Alert) error {
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
	links, err := db.readQueries.ListAlertDestinationLinks(ctx, ids)
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
		IsActive:  row.IsActive == 1,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
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
