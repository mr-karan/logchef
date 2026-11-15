package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Store) UpsertJob(ctx context.Context, def models.JobDefinition) error {
	err := s.q.UpsertJob(ctx, sqlc.UpsertJobParams{
		Kind:         def.Kind,
		Key:          def.Key,
		ScheduleKind: def.ScheduleKind,
		ScheduleExpr: def.ScheduleExpr,
		Timezone:     def.Timezone,
		Misfire:      def.Misfire,
		GraceSeconds: def.GraceSeconds,
		Enabled:      def.Enabled,
	})
	if err != nil {
		return fmt.Errorf("error upserting job %s/%s: %w", def.Kind, def.Key, err)
	}
	return nil
}

func (s *Store) DeleteJob(ctx context.Context, kind, key string) error {
	if err := s.q.DeleteJob(ctx, sqlc.DeleteJobParams{Kind: kind, Key: key}); err != nil {
		return fmt.Errorf("error deleting job %s/%s: %w", kind, key, err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, kind, key string) (*models.ScheduledJob, error) {
	row, err := s.q.GetJob(ctx, sqlc.GetJobParams{Kind: kind, Key: key})
	if err != nil {
		if notFound(err) {
			return nil, fmt.Errorf("error getting job %s/%s: %w", kind, key, models.ErrNotFound)
		}
		return nil, fmt.Errorf("error getting job %s/%s: %w", kind, key, err)
	}
	return jobToModel(&row), nil
}

func (s *Store) KickJob(ctx context.Context, kind, key string) error {
	n, err := s.q.KickJob(ctx, sqlc.KickJobParams{Kind: kind, Key: key})
	if err != nil {
		return fmt.Errorf("error kicking job %s/%s: %w", kind, key, err)
	}
	if n == 0 {
		return fmt.Errorf("error kicking job %s/%s: %w", kind, key, models.ErrNotFound)
	}
	return nil
}

func (s *Store) ClaimJobs(ctx context.Context, kind, owner string, lease time.Duration, limit int) ([]*models.ScheduledJob, error) {
	if limit < 1 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("error claiming %s jobs: limit %d is out of range", kind, limit)
	}
	rows, err := s.q.ClaimJobs(ctx, sqlc.ClaimJobsParams{
		Owner:        text(owner),
		LeaseSeconds: lease.Seconds(),
		Kind:         kind,
		MaxJobs:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error claiming %s jobs: %w", kind, err)
	}
	jobs := make([]*models.ScheduledJob, len(rows))
	for i := range rows {
		jobs[i] = jobToModel(&rows[i])
	}
	return jobs, nil
}

func (s *Store) HoldJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := s.q.HoldJob(ctx, sqlc.HoldJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error holding job %d: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) FinishJob(ctx context.Context, id, token int64, res models.JobRunResult) (bool, error) {
	n, err := s.q.FinishJob(ctx, sqlc.FinishJobParams{
		ScheduleKind: res.ScheduleKind,
		ScheduleExpr: res.ScheduleExpr,
		Timezone:     res.Timezone,
		NextRunAt:    ts(res.NextRunAt),
		LastStatus:   text(res.Status),
		LastError:    text(res.Error),
		ID:           id,
		LeaseToken:   token,
	})
	if err != nil {
		return false, fmt.Errorf("error finishing job %d: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) ReleaseJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := s.q.ReleaseJob(ctx, sqlc.ReleaseJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error releasing job %d: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) DeleteClaimedJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := s.q.DeleteClaimedJob(ctx, sqlc.DeleteClaimedJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error deleting job %d: %w", id, err)
	}
	return n == 1, nil
}

func jobToModel(r *sqlc.Job) *models.ScheduledJob {
	return &models.ScheduledJob{
		JobDefinition: models.JobDefinition{
			Kind:         r.Kind,
			Key:          r.Key,
			ScheduleKind: r.ScheduleKind,
			ScheduleExpr: r.ScheduleExpr,
			Timezone:     r.Timezone,
			Misfire:      r.Misfire,
			GraceSeconds: r.GraceSeconds,
			Enabled:      r.Enabled,
		},
		ID:             r.ID,
		NextRunAt:      r.NextRunAt.Time,
		LeaseOwner:     textStr(r.LeaseOwner),
		LeaseToken:     r.LeaseToken,
		LeaseExpiresAt: tsPtr(r.LeaseExpiresAt),
		LastStartedAt:  tsPtr(r.LastStartedAt),
		LastFinishedAt: tsPtr(r.LastFinishedAt),
		LastStatus:     textStr(r.LastStatus),
		LastError:      textStr(r.LastError),
		CreatedAt:      r.CreatedAt.Time,
		UpdatedAt:      r.UpdatedAt.Time,
	}
}
