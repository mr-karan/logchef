package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/alertjson"
	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateAlertSilence inserts a silence and fills its ID and timestamps.
func (s *Store) CreateAlertSilence(ctx context.Context, silence *models.AlertSilence) error {
	matchers, weekly, err := alertjson.EncodeSilence(silence)
	if err != nil {
		return err
	}
	params := sqlc.CreateAlertSilenceParams{
		SourceID:     int64(silence.SourceID),
		AlertID:      alertIDInt8(silence.AlertID),
		MatchersJson: matchers,
		Comment:      silence.Comment,
		ScheduleKind: string(silence.ScheduleKind),
		StartsAt:     tsFromPtr(silence.StartsAt),
		EndsAt:       tsFromPtr(silence.EndsAt),
		WeeklyJson:   text(weekly),
	}
	if silence.CreatedBy != nil {
		params.CreatedBy = int8Val(int64(*silence.CreatedBy))
	}
	row, err := s.q.CreateAlertSilence(ctx, params)
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
func (s *Store) GetAlertSilence(ctx context.Context, id models.AlertSilenceID) (*models.AlertSilence, error) {
	row, err := s.q.GetAlertSilence(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert silence: %w", err)
	}
	return alertSilenceFromSQLC(row)
}

// ListAlertSilences returns the silences of one source.
func (s *Store) ListAlertSilences(ctx context.Context, sourceID models.SourceID) ([]*models.AlertSilence, error) {
	rows, err := s.q.ListAlertSilencesBySource(ctx, int64(sourceID))
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
func (s *Store) CountAlertSilences(ctx context.Context, sourceID models.SourceID) (int, error) {
	n, err := s.q.CountAlertSilencesBySource(ctx, int64(sourceID))
	if err != nil {
		return 0, fmt.Errorf("failed to count alert silences: %w", err)
	}
	return int(n), nil
}

// UpdateAlertSilence writes a silence's rule, matchers, comment and schedule.
func (s *Store) UpdateAlertSilence(ctx context.Context, silence *models.AlertSilence) error {
	matchers, weekly, err := alertjson.EncodeSilence(silence)
	if err != nil {
		return err
	}
	row, err := s.q.UpdateAlertSilence(ctx, sqlc.UpdateAlertSilenceParams{
		AlertID:      alertIDInt8(silence.AlertID),
		MatchersJson: matchers,
		Comment:      silence.Comment,
		ScheduleKind: string(silence.ScheduleKind),
		StartsAt:     tsFromPtr(silence.StartsAt),
		EndsAt:       tsFromPtr(silence.EndsAt),
		WeeklyJson:   text(weekly),
		ID:           int64(silence.ID),
	})
	if err != nil {
		if notFound(err) {
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
func (s *Store) DeleteAlertSilence(ctx context.Context, id models.AlertSilenceID) error {
	if _, err := s.q.DeleteAlertSilence(ctx, int64(id)); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to delete alert silence: %w", err)
	}
	return nil
}

func alertIDInt8(id *models.AlertID) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return int8Val(int64(*id))
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func alertSilenceFromSQLC(row sqlc.AlertSilence) (*models.AlertSilence, error) {
	matchers, weekly, err := alertjson.DecodeSilence(row.MatchersJson, textStr(row.WeeklyJson))
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
		StartsAt:     utcPtr(tsPtr(row.StartsAt)),
		EndsAt:       utcPtr(tsPtr(row.EndsAt)),
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}
	if row.AlertID.Valid {
		id := models.AlertID(row.AlertID.Int64)
		silence.AlertID = &id
	}
	if row.CreatedBy.Valid {
		id := models.UserID(row.CreatedBy.Int64)
		silence.CreatedBy = &id
	}
	return silence, nil
}
