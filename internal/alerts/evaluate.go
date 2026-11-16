package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// evaluateAlert runs the rule's query over the lookback that ends at at, and
// commits the resulting transition.
// Every write is guarded by alert.EvalSeq, read with the rule, so an edit or
// manual resolve after the rule was loaded makes the evaluation write nothing.
// The next cycle evaluates the new rule.
func (m *Manager) evaluateAlert(ctx context.Context, alert *models.Alert, at time.Time) error {
	if alert == nil {
		return nil
	}

	query := strings.TrimSpace(alert.Query)
	if query == "" {
		m.log.Warn("alert query is empty; skipping evaluation", "alert_id", alert.ID, "query_language", alert.QueryLanguage, "editor_mode", alert.EditorMode)
		return nil
	}

	if m.datasource == nil {
		err := fmt.Errorf("datasource service is not configured")
		m.recordEvaluationError(ctx, alert, err)
		return err
	}

	timeout := models.DefaultQueryTimeoutSeconds
	result, err := m.datasource.EvaluateAlert(ctx, alert.SourceID, datasource.AlertQueryRequest{
		At:              at.UTC(),
		Language:        alert.QueryLanguage,
		Query:           query,
		LookbackSeconds: alert.LookbackSeconds,
		QueryTimeout:    &timeout,
	})
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("alert query failed: %w", err))
		return fmt.Errorf("alert query failed: %w", err)
	}

	samples, err := SamplesFromResult(result, alert.GroupBy)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to extract alert result: %w", err))
		return fmt.Errorf("failed to extract alert result: %w", err)
	}
	return m.commitEvaluation(ctx, alert, alert.EvalSeq, samples)
}

// episode is the open state of one group: its newest open history row and
// its instance.
type episode struct {
	instance *models.AlertInstance
	history  *models.AlertHistoryEntry
}

// openEpisodes pairs each group's newest open history row with its instance.
// open is newest first.
func openEpisodes(instances []*models.AlertInstance, open []*models.AlertHistoryEntry) map[string]episode {
	byKey := make(map[string]*models.AlertInstance, len(instances))
	for _, instance := range instances {
		byKey[instance.GroupKey] = instance
	}
	episodes := make(map[string]episode, len(open))
	for _, entry := range open {
		if _, ok := episodes[entry.GroupKey]; ok {
			continue
		}
		episodes[entry.GroupKey] = episode{instance: currentInstance(byKey[entry.GroupKey], entry), history: entry}
	}
	return episodes
}

// currentInstance returns the firing instance of an open episode. A row
// without a matching instance is adopted with notified unset, so the row's
// delivery bookkeeping decides retries as it did before instances existed.
func currentInstance(instance *models.AlertInstance, open *models.AlertHistoryEntry) *models.AlertInstance {
	if instance != nil && instance.HistoryID != nil && *instance.HistoryID == open.ID {
		return instance
	}
	historyID := open.ID
	return &models.AlertInstance{
		AlertID:    open.AlertID,
		GroupKey:   open.GroupKey,
		Group:      open.Group,
		State:      models.AlertInstanceStateFiring,
		Value:      open.Value,
		StateSince: open.TriggeredAt,
		HistoryID:  &historyID,
	}
}

// fireStep is one group's firing change. A step that does not notify only
// records the evaluation on its instance.
type fireStep struct {
	change   Change
	instance models.AlertInstance
	// history is the open episode, nil until a new one is written.
	history      *models.AlertHistoryEntry
	payload      map[string]any
	message      string
	notify       bool
	attempts     int
	retry        bool
	selected     []string
	notification AlertNotification
}

// resolveStep is one group's closing change.
type resolveStep struct {
	change  Change
	history *models.AlertHistoryEntry
	message string
}

// evaluationPlan is what one evaluation writes, then sends.
type evaluationPlan struct {
	fires      []*fireStep
	resolves   []*resolveStep
	newEpisode bool
}

func (m *Manager) commitEvaluation(ctx context.Context, alert *models.Alert, seq int64, samples []Sample) error {
	var mc *messageContext
	messages := func() *messageContext {
		if mc == nil {
			mc = m.newMessageContext(ctx, alert)
		}
		return mc
	}
	plan, err := m.planEvaluation(ctx, alert, samples, messages)
	if err != nil {
		return err
	}
	err = m.commit(ctx, alert.ID, seq, func(tx store.StoreOps) error { return plan.write(ctx, tx, alert.ID) })
	if err != nil {
		// Nothing was reserved, so nothing is sent; the next tick evaluates again.
		if !models.IsNotFound(err) && !models.IsConflict(err) {
			m.log.Error("failed to commit alert evaluation", "alert_id", alert.ID, "error", err)
		}
		return nil
	}
	if plan.newEpisode {
		if err := m.pruneHistory(ctx, alert.ID); err != nil {
			m.log.Warn("failed to prune alert history", "alert_id", alert.ID, "error", err)
		}
	}
	for _, step := range plan.fires {
		if step.notify {
			m.deliverFiring(ctx, alert, step)
		}
	}
	for _, step := range plan.resolves {
		m.deliverResolved(ctx, alert, step, messages())
	}
	return nil
}

// planEvaluation reads the open episodes, runs Transition and decides what
// each changed group writes.
func (m *Manager) planEvaluation(ctx context.Context, alert *models.Alert, samples []Sample, messages func() *messageContext) (*evaluationPlan, error) {
	instances, err := m.db.ListAlertInstances(ctx, alert.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert instances: %w", err)
	}
	open, err := m.db.ListOpenAlertHistory(ctx, alert.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list open alert history: %w", err)
	}
	episodes := openEpisodes(instances, open)
	tracked := make([]Instance, 0, len(episodes))
	for _, entry := range open {
		if ep, ok := episodes[entry.GroupKey]; ok && ep.history == entry {
			tracked = append(tracked, Instance{GroupKey: ep.instance.GroupKey, Group: ep.instance.Group, State: ep.instance.State})
		}
	}

	plan := &evaluationPlan{}
	for _, change := range Transition(Rule{Operator: alert.ThresholdOperator, Threshold: alert.ThresholdValue}, tracked, samples) {
		m.log.Debug("alert evaluation complete",
			"alert_id", alert.ID,
			"alert_name", alert.Name,
			"group_key", change.GroupKey,
			"value", change.Value,
			"threshold", alert.ThresholdValue,
			"operator", alert.ThresholdOperator,
			"from", change.From,
			"to", change.To)
		ep := episodes[change.GroupKey]
		switch {
		case change.To == models.AlertInstanceStateFiring:
			plan.fires = append(plan.fires, m.planFire(alert, ep.instance, ep.history, change, messages))
		case change.Closes():
			plan.resolves = append(plan.resolves, &resolveStep{
				change:  change,
				history: ep.history,
				message: fmt.Sprintf("alert %s resolved with value %.4f", alert.Name, change.Value),
			})
		}
	}
	return plan, nil
}

// write commits the plan inside the evaluation's eval_seq transaction: the
// firing episodes and their delivery reservations, the closed episodes and
// the rule's summary state.
func (plan *evaluationPlan) write(ctx context.Context, tx store.StoreOps, alertID models.AlertID) error {
	for _, step := range plan.fires {
		if err := plan.writeFire(ctx, tx, alertID, step); err != nil {
			return err
		}
	}
	for _, step := range plan.resolves {
		if err := tx.ResolveAlertGroup(ctx, alertID, step.change.GroupKey, step.message); err != nil {
			return err
		}
	}
	if len(plan.fires) > 0 {
		return tx.MarkAlertTriggered(ctx, alertID)
	}
	return tx.MarkAlertEvaluated(ctx, alertID)
}

func (plan *evaluationPlan) writeFire(ctx context.Context, tx store.StoreOps, alertID models.AlertID, step *fireStep) error {
	switch {
	case step.history == nil:
		value := step.change.Value
		history, err := tx.InsertAlertHistory(ctx, alertID, step.change.GroupKey, step.change.Group, models.AlertStatusTriggered, &value, step.message, step.payload)
		if err != nil {
			return err
		}
		step.history = history
		step.instance.StateSince = history.TriggeredAt
		step.instance.HistoryID = &history.ID
		plan.newEpisode = true
	case step.payload != nil:
		if err := tx.UpdateAlertHistoryPayload(ctx, step.history.ID, step.payload); err != nil {
			return err
		}
	}
	return tx.UpsertAlertInstance(ctx, &step.instance)
}

// deliverResolved sends one group's resolved notification after its episode
// closed.
func (m *Manager) deliverResolved(ctx context.Context, alert *models.Alert, step *resolveStep, mc *messageContext) {
	m.log.Info("alert resolved",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"group_key", step.change.GroupKey,
		"value", step.change.Value,
		"threshold", alert.ThresholdValue)
	now := time.Now().UTC()
	entry := step.history
	entry.Message = step.message
	entry.ResolvedAt = &now
	entry.Status = models.AlertStatusResolved
	if entry.Value == nil {
		value := step.change.Value
		entry.Value = &value
	}
	if err := m.sendResolved(ctx, mc, entry, step.change.Value, nil); err != nil {
		m.log.Warn("failed to send resolved alert notifications", "alert_id", alert.ID, "group_key", step.change.GroupKey, "error", err)
		return
	}
	m.log.Debug("resolved alert notifications sent", "alert_id", alert.ID, "alert_name", alert.Name)
}

// planFire decides what one firing group writes and whether it notifies. An
// open episode (prev and current set) notifies again only while its instance
// is not notified and its delivery bookkeeping asks for a retry.
func (m *Manager) planFire(alert *models.Alert, current *models.AlertInstance, prev *models.AlertHistoryEntry, change Change, messages func() *messageContext) *fireStep {
	step := &fireStep{change: change, instance: firingInstance(alert.ID, current, change), history: prev}
	attempts, retry := deliveryRetryState(prev)
	if prev != nil && (current.Notified || !retry) {
		step.instance.Notified = true
		if attempts >= maxDeliveryAttempts && prev.Payload["delivery_in_progress"] == true {
			prev.Payload["delivery_in_progress"] = false
			delete(prev.Payload, "delivery_deadline_at")
			prev.Payload["delivery_failed"] = true
			prev.Payload["delivery_retries_exhausted"] = true
			prev.Payload["delivery_error"] = "delivery interrupted"
			step.payload = prev.Payload
		}
		m.log.Debug("suppressing duplicate alert notification", "alert_id", alert.ID, "group_key", change.GroupKey, "delivery_attempts", attempts)
		return step
	}

	value := change.Value
	m.log.Info("alert triggered",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"group_key", change.GroupKey,
		"severity", alert.Severity,
		"value", value,
		"threshold", alert.ThresholdValue,
		"operator", alert.ThresholdOperator)

	mc := messages()
	labels, annotations := mc.metadata(models.AlertStatusTriggered, value, change.Group)
	step.message = fmt.Sprintf("alert %s triggered with value %.4f", alert.Name, value)
	notificationHistory := prev
	if notificationHistory == nil {
		notificationHistory = &models.AlertHistoryEntry{TriggeredAt: time.Now().UTC(), Message: step.message, GroupKey: change.GroupKey, Group: change.Group}
	}
	notification := mc.notification(notificationHistory, labels, annotations, models.AlertStatusTriggered, value)
	if retry {
		notification.deliveryTargets = pendingDeliveryTargets(prev.Payload)
		notification.attemptedTargets = storedDeliveryTargets(prev.Payload, "delivery_attempted")
	}
	selected := notification.targetIDs()
	attempted := notification.attemptedTargetIDs()
	attempts++
	payload := make(map[string]any)
	if prev != nil {
		maps.Copy(payload, prev.Payload)
	}
	maps.Copy(payload, map[string]any{
		"labels":                     copyStringMap(labels),
		"annotations":                copyStringMap(annotations),
		"status":                     string(models.AlertStatusTriggered),
		"delivery_failed":            false,
		"delivery_in_progress":       true,
		"delivery_deadline_at":       time.Now().UTC().Add(deliveryAttemptTimeout).Format(time.RFC3339Nano),
		"delivery_attempted":         attempted,
		"delivery_attempts":          attempts,
		"delivery_pending":           selected,
		"delivery_retries_exhausted": attempts >= maxDeliveryAttempts,
	})
	step.notify = true
	step.attempts = attempts
	step.retry = retry
	step.selected = selected
	step.payload = payload
	step.notification = notification
	return step
}

// deliverFiring sends one group's firing notification after its reservation
// committed, then records the outcome.
func (m *Manager) deliverFiring(ctx context.Context, alert *models.Alert, step *fireStep) {
	step.notification.TriggeredAt = step.history.TriggeredAt
	results := m.deliver(ctx, step.notification)
	deliveryErr := results.Err()
	// Removing every pending target leaves delivery failed until a current target succeeds.
	if step.retry && len(step.selected) == 0 && len(results) == 0 {
		deliveryErr = errors.New("no pending notification targets remain")
	}
	payload := step.payload
	payload["delivery_in_progress"] = false
	delete(payload, "delivery_deadline_at")
	payload["delivery_pending"] = results.failedTargets()
	payload["delivery_retries_exhausted"] = deliveryErr != nil && step.attempts >= maxDeliveryAttempts
	delete(payload, "delivery_error")
	payload["delivery_failed"] = deliveryErr != nil
	if deliveryErr != nil {
		payload["delivery_error"] = deliveryErr.Error()
		m.log.Warn("failed to send alert notifications", "alert_id", alert.ID, "group_key", step.change.GroupKey, "error", deliveryErr)
		if step.attempts == maxDeliveryAttempts {
			m.log.Error("alert delivery retries exhausted", "alert_id", alert.ID, "history_id", step.history.ID, "attempts", step.attempts)
		}
	} else {
		m.log.Debug("alert notifications sent", "alert_id", alert.ID, "alert_name", alert.Name)
	}
	m.recordDeliveryOutcome(ctx, alert.ID, step.change.GroupKey, step.history.ID, payload, deliveryErr == nil || step.attempts >= maxDeliveryAttempts)
}

// firingInstance is the instance after a firing evaluation. A new episode
// gets its start and history id when the row is written.
func firingInstance(alertID models.AlertID, current *models.AlertInstance, change Change) models.AlertInstance {
	instance := models.AlertInstance{AlertID: alertID, GroupKey: change.GroupKey, Group: change.Group}
	if current != nil {
		instance = *current
	}
	instance.State = models.AlertInstanceStateFiring
	value := change.Value
	instance.Value = &value
	if change.Sampled {
		now := time.Now().UTC()
		instance.LastSeenAt = &now
	}
	return instance
}

// commit runs fn in one transaction behind the job claim and the eval_seq
// guard. It writes nothing and returns models.ErrConflict when a later claim
// owns the rule's job, or when a person edited or resolved the rule after seq
// was read. Callers treat both as a discarded result.
func (m *Manager) commit(ctx context.Context, alertID models.AlertID, seq int64, fn func(tx store.StoreOps) error) error {
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := holdClaim(ctx, tx); err != nil {
			return err
		}
		if err := tx.AdvanceAlertEvalSeq(ctx, alertID, seq); err != nil {
			return err
		}
		return fn(tx)
	})
	switch {
	case errors.Is(err, jobs.ErrLeaseLost):
		m.log.Warn("a later claim owns this alert's evaluation; result discarded", "alert_id", alertID)
		return models.ErrConflict
	case models.IsConflict(err):
		m.log.Debug("alert changed during evaluation; result discarded", "alert_id", alertID)
	}
	return err
}

// pruneHistory deletes the oldest closed history rows behind the job claim. A
// run whose claim was taken over leaves pruning to the newer run.
func (m *Manager) pruneHistory(ctx context.Context, alertID models.AlertID) error {
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := holdClaim(ctx, tx); err != nil {
			return err
		}
		return tx.PruneAlertHistory(ctx, alertID, m.cfg.HistoryLimit)
	})
	if errors.Is(err, jobs.ErrLeaseLost) {
		return nil
	}
	return err
}

func evaluationLogLevel(ctx context.Context, err error) slog.Level {
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
		return slog.LevelDebug
	}
	return slog.LevelError
}

func (m *Manager) recordEvaluationError(ctx context.Context, alert *models.Alert, evalErr error) {
	if alert == nil || evalErr == nil {
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(evalErr, context.Canceled) {
		return
	}
	// Evaluation deadlines must not prevent recording genuine failures.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	errorMessage := fmt.Sprintf("Evaluation failed: %v", evalErr)
	errorPayload := map[string]any{
		"error":          evalErr.Error(),
		"query_language": string(alert.QueryLanguage),
		"editor_mode":    string(alert.EditorMode),
		"query":          alert.Query,
		"status":         string(models.AlertStatusError),
	}

	// Failed evaluations still count toward the alert's evaluation frequency.
	err := m.commit(ctx, alert.ID, alert.EvalSeq, func(tx store.StoreOps) error {
		if err := tx.TouchAlertEvaluated(ctx, alert.ID); err != nil {
			return err
		}
		_, err := tx.InsertAlertHistory(ctx, alert.ID, "", nil, models.AlertStatusError, nil, errorMessage, errorPayload)
		return err
	})
	if models.IsConflict(err) {
		return
	}
	if err != nil {
		m.log.Log(ctx, evaluationLogLevel(ctx, err), "failed to insert error history entry", "alert_id", alert.ID, "error", err)
		return
	}

	// Prune old history entries to prevent unbounded growth from repeated errors
	if pruneErr := m.pruneHistory(ctx, alert.ID); pruneErr != nil {
		m.log.Warn("failed to prune alert history after error", "alert_id", alert.ID, "error", pruneErr)
	}
}

// recordDeliveryOutcome writes the attempt's result. A settled episode, sent or
// out of attempts, marks its instance notified in the same transaction.
func (m *Manager) recordDeliveryOutcome(ctx context.Context, alertID models.AlertID, groupKey string, historyID int64, payload map[string]any, settled bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryBookkeepingTimeout)
	defer cancel()
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := holdClaim(ctx, tx); err != nil {
			return err
		}
		if err := tx.UpdateAlertHistoryPayload(ctx, historyID, payload); err != nil {
			return err
		}
		if !settled {
			return nil
		}
		return tx.MarkAlertInstanceNotified(ctx, alertID, groupKey, historyID)
	})
	switch {
	case err == nil:
	case models.IsNotFound(err):
		m.log.Debug("alert history resolved during delivery", "alert_id", alertID, "history_id", historyID)
	case errors.Is(err, jobs.ErrLeaseLost):
		m.log.Warn("a later claim owns this alert's evaluation; delivery outcome not recorded", "alert_id", alertID, "history_id", historyID)
	default:
		m.log.Error("failed to update alert history payload after delivery", "alert_id", alertID, "history_id", historyID, "error", err)
	}
}
