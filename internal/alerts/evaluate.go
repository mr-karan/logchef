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
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// evaluateAlert runs the rule's query and commits the resulting transition.
// Every write is guarded by alert.EvalSeq, read with the rule, so an edit or
// manual resolve after the rule was loaded makes the evaluation write nothing.
// The next cycle evaluates the new rule.
func (m *Manager) evaluateAlert(ctx context.Context, alert *models.Alert) error {
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
		Language:        alert.QueryLanguage,
		Query:           query,
		LookbackSeconds: alert.LookbackSeconds,
		QueryTimeout:    &timeout,
	})
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("alert query failed: %w", err))
		return fmt.Errorf("alert query failed: %w", err)
	}

	samples, err := ungroupedSamples(result)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to extract alert result: %w", err))
		return fmt.Errorf("failed to extract alert result: %w", err)
	}
	return m.commitEvaluation(ctx, alert, alert.EvalSeq, samples)
}

// ungroupedSamples reads the first column of the first row. No rows is no sample.
func ungroupedSamples(result *models.QueryResult) ([]Sample, error) {
	if result == nil || len(result.Logs) == 0 {
		return nil, nil
	}
	value, err := util.ExtractFirstNumeric(result)
	if err != nil {
		return nil, err
	}
	return []Sample{{Value: value}}, nil
}

func (m *Manager) commitEvaluation(ctx context.Context, alert *models.Alert, seq int64, samples []Sample) error {
	instances, err := m.db.ListAlertInstances(ctx, alert.ID)
	if err != nil {
		return fmt.Errorf("failed to list alert instances: %w", err)
	}
	open, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil {
		if !models.IsNotFound(err) {
			return fmt.Errorf("failed to check existing alert history: %w", err)
		}
		open = nil
	}
	current := currentInstance(instances, open)
	var tracked []Instance
	if current != nil {
		tracked = []Instance{{GroupKey: current.GroupKey, Group: current.Group, State: current.State}}
	}

	// An ungrouped rule tracks at most the one group "", so there is one change.
	change := Transition(Rule{Operator: alert.ThresholdOperator, Threshold: alert.ThresholdValue}, tracked, samples)[0]

	m.log.Debug("alert evaluation complete",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"value", change.Value,
		"threshold", alert.ThresholdValue,
		"operator", alert.ThresholdOperator,
		"from", change.From,
		"to", change.To)

	switch {
	case change.To == models.AlertInstanceStateFiring:
		return m.fire(ctx, alert, seq, current, open, change)
	case change.Closes():
		return m.resolve(ctx, alert, seq, open, change.Value)
	default:
		if err := m.commit(ctx, alert.ID, seq, func(tx store.StoreOps) error {
			return tx.MarkAlertEvaluated(ctx, alert.ID)
		}); err != nil && !models.IsConflict(err) {
			return fmt.Errorf("failed to mark alert evaluated: %w", err)
		}
		return nil
	}
}

// currentInstance returns the ungrouped rule's firing instance. The open
// history row is the episode. A row without a matching instance is adopted
// with notified unset, so the row's delivery bookkeeping decides retries as it
// did before instances existed.
func currentInstance(instances []*models.AlertInstance, open *models.AlertHistoryEntry) *models.AlertInstance {
	if open == nil {
		return nil
	}
	for _, instance := range instances {
		if instance.GroupKey == "" && instance.HistoryID != nil && *instance.HistoryID == open.ID {
			return instance
		}
	}
	historyID := open.ID
	return &models.AlertInstance{
		AlertID:    open.AlertID,
		State:      models.AlertInstanceStateFiring,
		Value:      open.Value,
		StateSince: open.TriggeredAt,
		HistoryID:  &historyID,
	}
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

// commit runs fn in one transaction behind the eval_seq guard. It returns
// models.ErrConflict, and writes nothing, when a person edited or resolved the
// rule after seq was read.
func (m *Manager) commit(ctx context.Context, alertID models.AlertID, seq int64, fn func(tx store.StoreOps) error) error {
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.AdvanceAlertEvalSeq(ctx, alertID, seq); err != nil {
			return err
		}
		return fn(tx)
	})
	if models.IsConflict(err) {
		m.log.Debug("alert changed during evaluation; result discarded", "alert_id", alertID)
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
		_, err := tx.InsertAlertHistory(ctx, alert.ID, models.AlertStatusError, nil, errorMessage, errorPayload)
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
	if pruneErr := m.db.PruneAlertHistory(ctx, alert.ID, m.cfg.HistoryLimit); pruneErr != nil {
		m.log.Warn("failed to prune alert history after error", "alert_id", alert.ID, "error", pruneErr)
	}
}

// fire handles a firing evaluation. prevHistory and current are nil when the
// change opens an episode. An open episode notifies again only while its
// instance is not notified and its delivery bookkeeping asks for a retry.
func (m *Manager) fire(ctx context.Context, alert *models.Alert, seq int64, current *models.AlertInstance, prevHistory *models.AlertHistoryEntry, change Change) error {
	value := change.Value
	instance := firingInstance(alert.ID, current, change)
	deliveryAttempts, shouldRetryDelivery := deliveryRetryState(prevHistory)
	if prevHistory != nil && (current.Notified || !shouldRetryDelivery) {
		return m.suppressDuplicateDelivery(ctx, alert.ID, seq, instance, prevHistory, deliveryAttempts)
	}

	m.log.Info("alert triggered",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"severity", alert.Severity,
		"value", value,
		"threshold", alert.ThresholdValue,
		"operator", alert.ThresholdOperator)

	labels, annotations := m.buildAlertMetadata(ctx, alert, models.AlertStatusTriggered, value)

	message := fmt.Sprintf("alert %s triggered with value %.4f", alert.Name, value)

	notificationHistory := prevHistory
	if notificationHistory == nil {
		notificationHistory = &models.AlertHistoryEntry{TriggeredAt: time.Now().UTC(), Message: message}
	}
	notification := m.buildNotification(ctx, alert, notificationHistory, labels, annotations, models.AlertStatusTriggered, value)
	if shouldRetryDelivery {
		notification.deliveryTargets = pendingDeliveryTargets(prevHistory.Payload)
		notification.attemptedTargets = storedDeliveryTargets(prevHistory.Payload, "delivery_attempted")
	}
	selectedTargets := notification.targetIDs()
	attemptedIDs := notification.attemptedTargetIDs()
	deliveryAttempts++
	historyPayload := make(map[string]any)
	if prevHistory != nil {
		maps.Copy(historyPayload, prevHistory.Payload)
	}
	maps.Copy(historyPayload, map[string]any{
		"labels":                     copyStringMap(labels),
		"annotations":                copyStringMap(annotations),
		"status":                     string(models.AlertStatusTriggered),
		"delivery_failed":            false,
		"delivery_in_progress":       true,
		"delivery_deadline_at":       time.Now().UTC().Add(deliveryAttemptTimeout).Format(time.RFC3339Nano),
		"delivery_attempted":         attemptedIDs,
		"delivery_attempts":          deliveryAttempts,
		"delivery_pending":           selectedTargets,
		"delivery_retries_exhausted": deliveryAttempts >= maxDeliveryAttempts,
	})

	history, err := m.reserveTriggeredHistory(ctx, alert.ID, seq, instance, prevHistory, value, message, historyPayload)
	if err != nil {
		if !models.IsNotFound(err) && !models.IsConflict(err) {
			m.log.Error("failed to reserve alert delivery attempt", "alert_id", alert.ID, "error", err)
		}
		return nil
	}

	notification.TriggeredAt = history.TriggeredAt
	results := m.sender.Send(ctx, notification)
	deliveryErr := results.Err()
	// Removing every pending target leaves delivery failed until a current target succeeds.
	if shouldRetryDelivery && len(selectedTargets) == 0 && len(results) == 0 {
		deliveryErr = errors.New("no pending notification targets remain")
	}
	historyPayload["delivery_in_progress"] = false
	delete(historyPayload, "delivery_deadline_at")
	historyPayload["delivery_pending"] = results.failedTargets()
	historyPayload["delivery_retries_exhausted"] = deliveryErr != nil && deliveryAttempts >= maxDeliveryAttempts
	delete(historyPayload, "delivery_error")
	historyPayload["delivery_failed"] = deliveryErr != nil
	if deliveryErr != nil {
		historyPayload["delivery_error"] = deliveryErr.Error()
		m.log.Warn("failed to send alert notifications", "alert_id", alert.ID, "error", deliveryErr)
		if deliveryAttempts == maxDeliveryAttempts {
			m.log.Error("alert delivery retries exhausted", "alert_id", alert.ID, "history_id", history.ID, "attempts", deliveryAttempts)
		}
	} else {
		m.log.Debug("alert notifications sent", "alert_id", alert.ID, "alert_name", alert.Name)
	}
	m.recordDeliveryOutcome(ctx, alert.ID, history.ID, historyPayload, deliveryErr == nil || deliveryAttempts >= maxDeliveryAttempts)
	return nil
}

// recordDeliveryOutcome writes the attempt's result. A settled episode, sent or
// out of attempts, marks its instance notified in the same transaction.
func (m *Manager) recordDeliveryOutcome(ctx context.Context, alertID models.AlertID, historyID int64, payload map[string]any, settled bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryBookkeepingTimeout)
	defer cancel()
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.UpdateAlertHistoryPayload(ctx, historyID, payload); err != nil {
			return err
		}
		if !settled {
			return nil
		}
		return tx.MarkAlertInstanceNotified(ctx, alertID, "", historyID)
	})
	switch {
	case err == nil:
	case models.IsNotFound(err):
		m.log.Debug("alert history resolved during delivery", "alert_id", alertID, "history_id", historyID)
	default:
		m.log.Error("failed to update alert history payload after delivery", "alert_id", alertID, "history_id", historyID, "error", err)
	}
}

// suppressDuplicateDelivery records a firing evaluation of an episode that
// owes no notification, and marks its instance notified.
func (m *Manager) suppressDuplicateDelivery(ctx context.Context, alertID models.AlertID, seq int64, instance models.AlertInstance, history *models.AlertHistoryEntry, attempts int) error {
	if attempts >= maxDeliveryAttempts && history.Payload["delivery_in_progress"] == true {
		history.Payload["delivery_in_progress"] = false
		delete(history.Payload, "delivery_deadline_at")
		history.Payload["delivery_failed"] = true
		history.Payload["delivery_retries_exhausted"] = true
		history.Payload["delivery_error"] = "delivery interrupted"
		if err := m.db.UpdateAlertHistoryPayload(ctx, history.ID, history.Payload); err != nil && !models.IsNotFound(err) {
			m.log.Error("failed to record interrupted alert delivery", "alert_id", alertID, "history_id", history.ID, "error", err)
		}
	}
	instance.Notified = true
	if err := m.commit(ctx, alertID, seq, func(tx store.StoreOps) error {
		if err := tx.UpsertAlertInstance(ctx, &instance); err != nil {
			return err
		}
		return tx.TouchAlertEvaluated(ctx, alertID)
	}); err != nil && !models.IsConflict(err) {
		return err
	}
	m.log.Debug("suppressing duplicate alert notification", "alert_id", alertID, "delivery_attempts", attempts)
	return nil
}

// reserveTriggeredHistory writes the episode, its instance and the delivery
// reservation before the send, so a send never happens without a record.
func (m *Manager) reserveTriggeredHistory(ctx context.Context, alertID models.AlertID, seq int64, instance models.AlertInstance, history *models.AlertHistoryEntry, value float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	newHistory := history == nil
	err := m.commit(ctx, alertID, seq, func(tx store.StoreOps) error {
		if newHistory {
			var err error
			history, err = tx.InsertAlertHistory(ctx, alertID, models.AlertStatusTriggered, &value, message, payload)
			if err != nil {
				return err
			}
			instance.StateSince = history.TriggeredAt
			instance.HistoryID = &history.ID
		} else if err := tx.UpdateAlertHistoryPayload(ctx, history.ID, payload); err != nil {
			return err
		}
		if err := tx.UpsertAlertInstance(ctx, &instance); err != nil {
			return err
		}
		return tx.MarkAlertTriggered(ctx, alertID)
	})
	if err != nil {
		return nil, err
	}
	if newHistory {
		if err := m.db.PruneAlertHistory(ctx, alertID, m.cfg.HistoryLimit); err != nil {
			m.log.Warn("failed to prune alert history", "alert_id", alertID, "error", err)
		}
	}
	return history, nil
}

// resolve closes the open episode and sends the resolved notification.
func (m *Manager) resolve(ctx context.Context, alert *models.Alert, seq int64, entry *models.AlertHistoryEntry, value float64) error {
	m.log.Info("alert resolved",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"value", value,
		"threshold", alert.ThresholdValue)

	message := fmt.Sprintf("alert %s resolved with value %.4f", alert.Name, value)
	if err := m.commit(ctx, alert.ID, seq, func(tx store.StoreOps) error {
		if err := tx.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
			return err
		}
		return tx.MarkAlertEvaluated(ctx, alert.ID)
	}); err != nil {
		if models.IsNotFound(err) || models.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}

	now := time.Now().UTC()
	entry.Message = message
	entry.ResolvedAt = &now
	entry.Status = models.AlertStatusResolved
	if entry.Value == nil {
		entry.Value = &value
	}

	labels, annotations := m.buildAlertMetadata(ctx, alert, models.AlertStatusResolved, value)
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	annotations["resolved_at"] = now.Format(time.RFC3339Nano)

	if sendErr := m.sendNotification(ctx, alert, entry, labels, annotations, models.AlertStatusResolved, value); sendErr != nil {
		m.log.Warn("failed to send resolved alert notifications", "alert_id", alert.ID, "error", sendErr)
	} else {
		m.log.Debug("resolved alert notifications sent", "alert_id", alert.ID, "alert_name", alert.Name)
	}
	return nil
}
