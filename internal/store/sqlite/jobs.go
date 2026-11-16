package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// jobTimeLayout matches strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), the form the
// jobs queries compare as text.
const jobTimeLayout = "2006-01-02T15:04:05.000Z"

func (db *DB) UpsertJob(ctx context.Context, def models.JobDefinition, firstRunAt *time.Time) error {
	err := db.writeQueries.UpsertJob(ctx, sqlc.UpsertJobParams{
		Kind:         def.Kind,
		Key:          def.Key,
		ScheduleKind: def.ScheduleKind,
		ScheduleExpr: def.ScheduleExpr,
		Timezone:     def.Timezone,
		Misfire:      def.Misfire,
		GraceSeconds: def.GraceSeconds,
		Enabled:      boolToInt(def.Enabled),
		FirstRunAt:   nullTimeText(firstRunAt),
	})
	if err != nil {
		return fmt.Errorf("error upserting job %s/%s: %w", def.Kind, def.Key, translateBusy(err))
	}
	return nil
}

func (db *DB) DeleteJob(ctx context.Context, kind, key string) error {
	if err := db.writeQueries.DeleteJob(ctx, sqlc.DeleteJobParams{Kind: kind, Key: key}); err != nil {
		return fmt.Errorf("error deleting job %s/%s: %w", kind, key, translateBusy(err))
	}
	return nil
}

func (db *DB) GetJob(ctx context.Context, kind, key string) (*models.ScheduledJob, error) {
	row, err := db.readQueries.GetJob(ctx, sqlc.GetJobParams{Kind: kind, Key: key})
	if err != nil {
		return nil, fmt.Errorf("error getting job %s/%s: %w", kind, key, translateNotFound(err))
	}
	return jobFromSQLC(&row), nil
}

func (db *DB) KickJob(ctx context.Context, kind, key string) error {
	n, err := db.writeQueries.KickJob(ctx, sqlc.KickJobParams{Kind: kind, Key: key})
	if err != nil {
		return fmt.Errorf("error kicking job %s/%s: %w", kind, key, translateBusy(err))
	}
	if n == 0 {
		return fmt.Errorf("error kicking job %s/%s: %w", kind, key, models.ErrNotFound)
	}
	return nil
}

func (db *DB) ClaimJobs(ctx context.Context, kind, owner string, lease time.Duration, limit int) ([]*models.ScheduledJob, error) {
	rows, err := db.writeQueries.ClaimJobs(ctx, sqlc.ClaimJobsParams{
		Owner:         nullString(owner),
		LeaseModifier: fmt.Sprintf("+%.3f seconds", lease.Seconds()),
		Kind:          kind,
		MaxJobs:       int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error claiming %s jobs: %w", kind, translateBusy(err))
	}
	jobs := make([]*models.ScheduledJob, len(rows))
	for i := range rows {
		jobs[i] = jobFromSQLC(&rows[i])
	}
	return jobs, nil
}

func (db *DB) HoldJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := db.writeQueries.HoldJob(ctx, sqlc.HoldJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error holding job %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) FinishJob(ctx context.Context, id, token int64, res models.JobRunResult) (bool, error) {
	n, err := db.writeQueries.FinishJob(ctx, sqlc.FinishJobParams{
		ScheduleKind: res.ScheduleKind,
		ScheduleExpr: res.ScheduleExpr,
		Timezone:     res.Timezone,
		NextRunAt:    res.NextRunAt.UTC().Format(jobTimeLayout),
		LastStatus:   nullString(res.Status),
		LastError:    nullString(res.Error),
		ID:           id,
		LeaseToken:   token,
	})
	if err != nil {
		return false, fmt.Errorf("error finishing job %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) ReleaseJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := db.writeQueries.ReleaseJob(ctx, sqlc.ReleaseJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error releasing job %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) DeleteClaimedJob(ctx context.Context, id, token int64) (bool, error) {
	n, err := db.writeQueries.DeleteClaimedJob(ctx, sqlc.DeleteClaimedJobParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("error deleting job %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func jobFromSQLC(row *sqlc.Job) *models.ScheduledJob {
	return &models.ScheduledJob{
		JobDefinition: models.JobDefinition{
			Kind:         row.Kind,
			Key:          row.Key,
			ScheduleKind: row.ScheduleKind,
			ScheduleExpr: row.ScheduleExpr,
			Timezone:     row.Timezone,
			Misfire:      row.Misfire,
			GraceSeconds: row.GraceSeconds,
			Enabled:      row.Enabled == 1,
		},
		ID:             row.ID,
		NextRunAt:      row.NextRunAt,
		LeaseOwner:     row.LeaseOwner.String,
		LeaseToken:     row.LeaseToken,
		LeaseExpiresAt: jobTimePtr(row.LeaseExpiresAt),
		LastStartedAt:  jobTimePtr(row.LastStartedAt),
		LastFinishedAt: jobTimePtr(row.LastFinishedAt),
		LastStatus:     row.LastStatus.String,
		LastError:      row.LastError.String,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func jobTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
