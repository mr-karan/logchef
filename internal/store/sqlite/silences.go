package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/alertjson"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateAlertSilence inserts a silence and fills its ID and timestamps.
func (db *DB) CreateAlertSilence(ctx context.Context, silence *models.AlertSilence) error {
	matchers, weekly, err := alertjson.EncodeSilence(silence)
	if err != nil {
		return err
	}
	params := sqlc.CreateAlertSilenceParams{
		SourceID:     int64(silence.SourceID),
		AlertID:      nullAlertID(silence.AlertID),
		MatchersJson: matchers,
		Comment:      silence.Comment,
		ScheduleKind: string(silence.ScheduleKind),
		StartsAt:     nullUTC(silence.StartsAt),
		EndsAt:       nullUTC(silence.EndsAt),
		WeeklyJson:   nullString(weekly),
	}
	if silence.CreatedBy != nil {
		params.CreatedBy = sql.NullInt64{Int64: int64(*silence.CreatedBy), Valid: true}
	}
	row, err := db.writeQueries.CreateAlertSilence(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to insert alert silence: %w", err)
	}
	created, err := alertSilenceFromSQLC(row)
	if err != nil {
		return err
	}
	*silence = *created
	return nil
}

// GetAlertSilence returns one silence.
func (db *DB) GetAlertSilence(ctx context.Context, id models.AlertSilenceID) (*models.AlertSilence, error) {
	row, err := db.readQueries.GetAlertSilence(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert silence: %w", err)
	}
	return alertSilenceFromSQLC(row)
}

// ListAlertSilences returns the silences of one source.
func (db *DB) ListAlertSilences(ctx context.Context, sourceID models.SourceID) ([]*models.AlertSilence, error) {
	rows, err := db.readQueries.ListAlertSilencesBySource(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alert silences: %w", err)
	}
	out := make([]*models.AlertSilence, 0, len(rows))
	for i := range rows {
		silence, err := alertSilenceFromSQLC(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, silence)
	}
	return out, nil
}

// CountAlertSilences counts the silences of one source.
func (db *DB) CountAlertSilences(ctx context.Context, sourceID models.SourceID) (int, error) {
	n, err := db.writeQueries.CountAlertSilencesBySource(ctx, int64(sourceID))
	if err != nil {
		return 0, fmt.Errorf("failed to count alert silences: %w", err)
	}
	return int(n), nil
}

// UpdateAlertSilence writes a silence's rule, matchers, comment and schedule.
func (db *DB) UpdateAlertSilence(ctx context.Context, silence *models.AlertSilence) error {
	matchers, weekly, err := alertjson.EncodeSilence(silence)
	if err != nil {
		return err
	}
	row, err := db.writeQueries.UpdateAlertSilence(ctx, sqlc.UpdateAlertSilenceParams{
		AlertID:      nullAlertID(silence.AlertID),
		MatchersJson: matchers,
		Comment:      silence.Comment,
		ScheduleKind: string(silence.ScheduleKind),
		StartsAt:     nullUTC(silence.StartsAt),
		EndsAt:       nullUTC(silence.EndsAt),
		WeeklyJson:   nullString(weekly),
		ID:           int64(silence.ID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert silence: %w", err)
	}
	updated, err := alertSilenceFromSQLC(row)
	if err != nil {
		return err
	}
	*silence = *updated
	return nil
}

// DeleteAlertSilence removes a silence.
func (db *DB) DeleteAlertSilence(ctx context.Context, id models.AlertSilenceID) error {
	if _, err := db.writeQueries.DeleteAlertSilence(ctx, int64(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to delete alert silence: %w", err)
	}
	return nil
}

func nullAlertID(id *models.AlertID) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func nullUTC(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func alertSilenceFromSQLC(row sqlc.AlertSilence) (*models.AlertSilence, error) {
	matchers, weekly, err := alertjson.DecodeSilence(row.MatchersJson, row.WeeklyJson.String)
	if err != nil {
		return nil, err
	}
	silence := &models.AlertSilence{
		ID:           models.AlertSilenceID(row.ID),
		SourceID:     models.SourceID(row.SourceID),
		Matchers:     matchers,
		Comment:      row.Comment,
		ScheduleKind: models.SilenceScheduleKind(row.ScheduleKind),
		Weekly:       weekly,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.AlertID.Valid {
		id := models.AlertID(row.AlertID.Int64)
		silence.AlertID = &id
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time.UTC()
		silence.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time.UTC()
		silence.EndsAt = &t
	}
	if row.CreatedBy.Valid {
		id := models.UserID(row.CreatedBy.Int64)
		silence.CreatedBy = &id
	}
	return silence, nil
}
