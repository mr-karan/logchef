package alerts

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// EvaluateKind is the job that evaluates one rule. Its key is the alert id.
const EvaluateKind jobs.Kind = "alert.evaluate"

// MinFrequencySeconds is the shortest schedule a rule's job runs on. New and
// edited rules must use at least this; an older rule below it keeps its stored
// value and runs at this floor.
const MinFrequencySeconds = 10

// EvaluationJob is the job definition of a rule. Callers upsert it in the
// transaction that writes the rule, so the rule and its job change together.
func EvaluationJob(alert *models.Alert) jobs.Spec {
	seconds := max(alert.FrequencySeconds, MinFrequencySeconds)
	return jobs.Spec{
		Kind:     EvaluateKind,
		Key:      evaluationJobKey(alert.ID),
		Schedule: jobs.Every(time.Duration(seconds) * time.Second),
		Misfire:  jobs.RunOnce,
		Enabled:  alert.IsActive,
	}
}

// DeleteEvaluationJob removes a rule's job in the caller's transaction.
func DeleteEvaluationJob(ctx context.Context, tx store.JobStore, id models.AlertID) error {
	return jobs.Delete(ctx, tx, EvaluateKind, evaluationJobKey(id))
}

func evaluationJobKey(id models.AlertID) string {
	return strconv.FormatInt(int64(id), 10)
}

// EvaluationKind is the alert.evaluate registration for a job runner.
func (m *Manager) EvaluationKind() jobs.KindSpec {
	return jobs.KindSpec{
		Kind:        EvaluateKind,
		Handler:     m.runEvaluation,
		Timeout:     alertEvaluationTimeout,
		Concurrency: m.cfg.MaxConcurrentEvaluations,
	}
}

// runEvaluation evaluates the rule named by the job's key. Every write of the
// evaluation commits behind jobs.Hold, so a run whose claim was taken over
// writes nothing.
func (m *Manager) runEvaluation(ctx context.Context, job jobs.Job) error {
	id, err := strconv.ParseInt(job.Key, 10, 64)
	if err != nil {
		return fmt.Errorf("alert job key %q is not an alert id", job.Key)
	}
	alert, err := m.db.GetAlert(ctx, models.AlertID(id))
	if models.IsNotFound(err) {
		return jobs.ErrGone
	}
	if err != nil {
		return fmt.Errorf("failed to load alert %d: %w", id, err)
	}
	if !alert.IsActive {
		// Paused after the claim; the job is disabled in the same write.
		return nil
	}
	// The window ends at the due occurrence, not at the claim, so a late
	// claim does not shift it.
	return m.evaluateAlert(withClaim(ctx, job), alert, job.At)
}

type claimKey struct{}

// withClaim marks ctx as an evaluation that the job runner claimed.
func withClaim(ctx context.Context, job jobs.Job) context.Context {
	return context.WithValue(ctx, claimKey{}, job)
}

// holdClaim proves inside tx that the evaluation's claim is still current.
// An evaluation outside the runner, such as a test driving one tick, has no
// claim to hold.
func holdClaim(ctx context.Context, tx store.JobStore) error {
	job, ok := ctx.Value(claimKey{}).(jobs.Job)
	if !ok {
		return nil
	}
	return jobs.Hold(ctx, tx, job)
}
