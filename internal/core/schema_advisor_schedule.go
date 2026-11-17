package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// SchemaAdviceRefreshKind is the job that refreshes one source's schema
// advisor report every week (RFC 0010 U13). Its key is the source id. The
// job exists only while the source's scheduled refresh is on.
const SchemaAdviceRefreshKind jobs.Kind = "schema_advisor.refresh"

const (
	// schemaAdviceRefreshCron is Monday 03:00 UTC.
	schemaAdviceRefreshCron = "0 3 * * 1"
	// schemaAdviceJobTimeout covers the run's own 30 s deadline and the
	// store reads and writes around it.
	schemaAdviceJobTimeout = 45 * time.Second
)

// SchemaAdviceJobKind is the schema_advisor.refresh registration for a job
// runner. A process runs one scheduled analysis at a time.
func SchemaAdviceJobKind(db store.Store, ds *datasource.Service) jobs.KindSpec {
	return jobs.KindSpec{
		Kind:        SchemaAdviceRefreshKind,
		Handler:     func(ctx context.Context, job jobs.Job) error { return refreshScheduledSchemaAdvice(ctx, db, ds, job) },
		Timeout:     schemaAdviceJobTimeout,
		Concurrency: 1,
	}
}

// SetSchemaAdviceSchedule turns the weekly refresh of a source's report on
// or off, and returns whether it was on before. The principal must be a
// global admin and hold sources:write.
func SetSchemaAdviceSchedule(ctx context.Context, db store.Store, ds *datasource.Service, p access.Principal, sourceID models.SourceID, enabled bool) (before bool, err error) {
	if err := p.RequireGlobalAdmin(); err != nil {
		return false, err
	}
	if err := p.Require(models.TokenScopeSourcesWrite); err != nil {
		return false, err
	}
	if _, err := schemaAdviceSource(ctx, ds, sourceID); err != nil {
		return false, err
	}
	key := strconv.FormatInt(int64(sourceID), 10)
	err = withSchemaSourceLock(ctx, db, sourceID, func(tx store.StoreOps) error {
		if before, err = schemaAdviceScheduled(ctx, tx, sourceID); err != nil {
			return err
		}
		if !enabled {
			return jobs.Delete(ctx, tx, SchemaAdviceRefreshKind, key)
		}
		schedule, err := jobs.Cron(schemaAdviceRefreshCron, "UTC")
		if err != nil {
			return err
		}
		return jobs.Upsert(ctx, tx, jobs.Spec{Kind: SchemaAdviceRefreshKind, Key: key, Schedule: schedule, Misfire: jobs.RunOnce, Enabled: true})
	})
	return before, err
}

func schemaAdviceScheduled(ctx context.Context, db store.JobStore, sourceID models.SourceID) (bool, error) {
	_, err := db.GetJob(ctx, string(SchemaAdviceRefreshKind), strconv.FormatInt(int64(sourceID), 10))
	if errors.Is(err, models.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// refreshScheduledSchemaAdvice analyzes the source that the job names with
// the default window, as of the tick job.At.
func refreshScheduledSchemaAdvice(ctx context.Context, db store.Store, ds *datasource.Service, job jobs.Job) error {
	sourceID, err := ParseSourceID(job.Key)
	if err != nil {
		return fmt.Errorf("schema advice job key %q: %w", job.Key, err)
	}
	source, err := schemaAdviceSource(ctx, ds, sourceID)
	if errors.Is(err, ErrSourceNotFound) {
		return jobs.ErrGone
	}
	if err != nil {
		return err
	}
	_, err = analyzeSchema(ctx, db, ds, source, schemaAnalysis{windowDays: DefaultSchemaAdviceWindowDays, at: job.At, claim: &job})
	if errors.Is(err, jobs.ErrLeaseLost) {
		// A later claim owns the tick and writes its own report.
		return nil
	}
	return err
}
