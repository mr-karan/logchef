package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// alertEvaluationTimeout bounds a single alert's evaluation wall-clock time. It
// sits above the per-query timeout (models.DefaultQueryTimeoutSeconds) with
// headroom for result processing, so a query's own timeout normally fires
// first; this deadline is the backstop that stops a wedged source from blocking
// the sequential evaluation loop.
const alertEvaluationTimeout = (models.DefaultQueryTimeoutSeconds + 30) * time.Second

const maxDeliveryAttempts = 3

// Options encapsulates the dependencies required to run the alerting manager.
type Options struct {
	Config      config.AlertsConfig
	DB          store.Store
	Datasources *datasource.Service
	Logger      *slog.Logger
	Sender      AlertSender
}

// Manager coordinates alert evaluation and dispatches notifications when thresholds are met.
type Manager struct {
	cfg        config.AlertsConfig
	db         store.Store
	datasource *datasource.Service
	log        *slog.Logger
	sender     AlertSender

	// evalTimeout bounds a single alert's evaluation; evalFn is the function
	// invoked per alert. Both are seams so the per-alert timeout isolation can
	// be exercised in tests without a live datasource/store.
	evalTimeout time.Duration
	evalFn      func(context.Context, *models.Alert) error

	lifecycleMu sync.Mutex
	stopped     bool
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewManager constructs a new alert manager instance.
func NewManager(opts Options) *Manager {
	sender := opts.Sender
	if sender == nil {
		sender = noopSender{}
	}
	m := &Manager{
		cfg:         opts.Config,
		db:          opts.DB,
		datasource:  opts.Datasources,
		log:         opts.Logger.With("component", "alert_manager"),
		sender:      sender,
		evalTimeout: alertEvaluationTimeout,
	}
	m.evalFn = m.evaluateAlert
	return m
}

// Start launches the evaluation loop. It is a no-op when alerting is disabled.
func (m *Manager) Start(ctx context.Context) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.stopped || m.cancel != nil {
		return
	}
	if !m.cfg.Enabled {
		m.log.Debug("alerting disabled")
		return
	}
	interval := m.cfg.EvaluationInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.log.Debug("starting alert manager", "interval", interval)

	m.wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run an initial evaluation so alerts fire soon after startup.
		m.evaluateCycle(ctx)

		for {
			select {
			case <-ticker.C:
				m.evaluateCycle(ctx)
			case <-ctx.Done():
				m.log.Debug("alert manager context cancelled")
				return
			}
		}
	})
}

// Stop signals the manager to stop evaluating alerts.
func (m *Manager) Stop() {
	m.lifecycleMu.Lock()
	m.stopped = true
	if m.cancel != nil {
		m.cancel()
	}
	m.lifecycleMu.Unlock()
	m.wg.Wait()
}

func (m *Manager) evaluateCycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	alerts, err := m.db.ListActiveAlertsDue(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		m.log.Error("failed to fetch alerts for evaluation", "error", err)
		return
	}
	if len(alerts) == 0 {
		return
	}

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}
		if err := m.evaluateAlertWithTimeout(ctx, alert); err != nil && !errors.Is(err, context.Canceled) {
			m.log.Error("alert evaluation failed", "alert_id", alert.ID, "error", err)
		}
	}
}

// evaluateAlertWithTimeout bounds a single alert's evaluation with its own
// deadline. The manager runs on the process-lifetime context, so without this
// a source whose endpoint wedges (stalled socket) would block the sequential
// loop forever and freeze alerting org-wide. A per-alert deadline isolates that
// failure to the one slow alert; the loop then advances to the rest.
func (m *Manager) evaluateAlertWithTimeout(ctx context.Context, alert *models.Alert) error {
	timeout := m.evalTimeout
	if timeout <= 0 {
		timeout = alertEvaluationTimeout
	}
	alertCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.evalFn(alertCtx, alert)
}

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

	value, err := util.ExtractFirstNumeric(result)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to extract alert result: %w", err))
		return fmt.Errorf("failed to extract alert result: %w", err)
	}

	triggered := util.CompareAlertThreshold(value, alert.ThresholdValue, alert.ThresholdOperator)

	m.log.Debug("alert evaluation complete",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"value", value,
		"threshold", alert.ThresholdValue,
		"operator", alert.ThresholdOperator,
		"triggered", triggered)

	if triggered {
		return m.handleTriggered(ctx, alert, value)
	}
	return m.handleResolved(ctx, alert, value)
}

func (m *Manager) recordEvaluationError(ctx context.Context, alert *models.Alert, evalErr error) {
	if alert == nil || evalErr == nil || errors.Is(evalErr, context.Canceled) {
		return
	}

	// Failed evaluations still count toward the alert's evaluation frequency.
	if err := m.db.TouchAlertEvaluated(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert evaluated after error", "alert_id", alert.ID, "error", err)
	}

	errorMessage := fmt.Sprintf("Evaluation failed: %v", evalErr)
	errorPayload := map[string]any{
		"error":          evalErr.Error(),
		"query_language": string(alert.QueryLanguage),
		"editor_mode":    string(alert.EditorMode),
		"query":          alert.Query,
		"status":         string(models.AlertStatusError),
	}

	_, err := m.db.InsertAlertHistory(ctx, alert.ID, models.AlertStatusError, nil, errorMessage, errorPayload)
	if err != nil {
		m.log.Error("failed to insert error history entry", "alert_id", alert.ID, "error", err)
		return
	}

	// Prune old history entries to prevent unbounded growth from repeated errors
	if pruneErr := m.db.PruneAlertHistory(ctx, alert.ID, m.cfg.HistoryLimit); pruneErr != nil {
		m.log.Warn("failed to prune alert history after error", "alert_id", alert.ID, "error", pruneErr)
	}
}

func (m *Manager) handleTriggered(ctx context.Context, alert *models.Alert, value float64) error {
	prevHistory, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil && !models.IsNotFound(err) {
		return fmt.Errorf("failed to check existing alert history: %w", err)
	}
	alreadyActive := err == nil && prevHistory != nil

	deliveryAttempts, shouldRetryDelivery := deliveryRetryState(prevHistory)
	if alreadyActive && !shouldRetryDelivery {
		return m.suppressDuplicateDelivery(ctx, alert.ID, prevHistory, deliveryAttempts)
	}

	m.log.Info("alert triggered",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"severity", alert.Severity,
		"value", value,
		"threshold", alert.ThresholdValue,
		"operator", alert.ThresholdOperator)

	labels, annotations := m.buildAlertMetadata(ctx, alert, models.AlertStatusTriggered, value)

	valueCopy := value
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
		"delivery_attempted":         attemptedIDs,
		"delivery_attempts":          deliveryAttempts,
		"delivery_pending":           selectedTargets,
		"delivery_retries_exhausted": deliveryAttempts >= maxDeliveryAttempts,
	})

	history, err := m.reserveTriggeredHistory(ctx, alert.ID, prevHistory, valueCopy, message, historyPayload)
	if err != nil {
		if !models.IsNotFound(err) {
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
	bookkeepingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := m.db.UpdateAlertHistoryPayload(bookkeepingCtx, history.ID, historyPayload); err != nil {
		if models.IsNotFound(err) {
			m.log.Debug("alert history resolved during delivery", "alert_id", alert.ID, "history_id", history.ID)
		} else {
			m.log.Error("failed to update alert history payload after delivery", "alert_id", alert.ID, "history_id", history.ID, "error", err)
		}
	}

	return nil
}

func (m *Manager) suppressDuplicateDelivery(ctx context.Context, alertID models.AlertID, history *models.AlertHistoryEntry, attempts int) error {
	if attempts >= maxDeliveryAttempts && history.Payload["delivery_in_progress"] == true {
		history.Payload["delivery_in_progress"] = false
		history.Payload["delivery_failed"] = true
		history.Payload["delivery_retries_exhausted"] = true
		history.Payload["delivery_error"] = "delivery interrupted"
		if err := m.db.UpdateAlertHistoryPayload(ctx, history.ID, history.Payload); err != nil && !models.IsNotFound(err) {
			m.log.Error("failed to record interrupted alert delivery", "alert_id", alertID, "history_id", history.ID, "error", err)
		}
	}
	if err := m.db.TouchAlertEvaluated(ctx, alertID); err != nil {
		return err
	}
	m.log.Debug("suppressing duplicate alert notification", "alert_id", alertID, "delivery_attempts", attempts)
	return nil
}

func (m *Manager) reserveTriggeredHistory(ctx context.Context, alertID models.AlertID, history *models.AlertHistoryEntry, value float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	newHistory := history == nil
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if newHistory {
			var err error
			history, err = tx.InsertAlertHistory(ctx, alertID, models.AlertStatusTriggered, &value, message, payload)
			if err != nil {
				return err
			}
		} else if err := tx.UpdateAlertHistoryPayload(ctx, history.ID, payload); err != nil {
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

func (m *Manager) handleResolved(ctx context.Context, alert *models.Alert, value float64) error {
	if err := m.db.MarkAlertEvaluated(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert evaluated", "alert_id", alert.ID, "error", err)
	}

	entry, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch unresolved alert history: %w", err)
	}

	m.log.Info("alert resolved",
		"alert_id", alert.ID,
		"alert_name", alert.Name,
		"value", value,
		"threshold", alert.ThresholdValue)

	message := fmt.Sprintf("alert %s resolved with value %.4f", alert.Name, value)
	if err := m.db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		if models.IsNotFound(err) {
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

func (m *Manager) buildAlertMetadata(ctx context.Context, alert *models.Alert, status models.AlertStatus, value float64) (labels, annotations map[string]string) {
	labels = copyStringMap(alert.Labels)
	if labels == nil {
		labels = make(map[string]string, 8)
	}
	labels["alertname"] = alert.Name
	labels["alert_id"] = strconv.FormatInt(int64(alert.ID), 10)
	labels["severity"] = string(alert.Severity)

	if source, err := m.db.GetSource(ctx, alert.SourceID); err == nil && source != nil {
		labels["source"] = source.Name
		labels["source_id"] = strconv.FormatInt(int64(alert.SourceID), 10)
	} else {
		// Fallback to just ID if fetch fails
		labels["source_id"] = strconv.FormatInt(int64(alert.SourceID), 10)
		m.log.Warn("failed to fetch source name for alert metadata", "source_id", alert.SourceID, "error", err)
	}

	annotations = copyStringMap(alert.Annotations)
	if annotations == nil {
		annotations = make(map[string]string, 8)
	}
	if desc := strings.TrimSpace(alert.Description); desc != "" {
		annotations["description"] = desc
	}
	if query := strings.TrimSpace(alert.Query); query != "" {
		annotations["query"] = query
	}
	if alert.ConditionJSON != "" {
		annotations["condition_json"] = alert.ConditionJSON
	}
	annotations["threshold"] = fmt.Sprintf("%s %.4f", alert.ThresholdOperator, alert.ThresholdValue)
	annotations["value"] = strconv.FormatFloat(value, 'f', 4, 64)
	annotations["status"] = string(status)
	annotations["frequency_seconds"] = strconv.Itoa(alert.FrequencySeconds)
	if alert.LookbackSeconds > 0 {
		annotations["lookback_seconds"] = strconv.Itoa(alert.LookbackSeconds)
	}
	return labels, annotations
}

func (m *Manager) sendNotification(ctx context.Context, alert *models.Alert, history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) error {
	if m.sender == nil || history == nil {
		return nil
	}

	notification := m.buildNotification(ctx, alert, history, labels, annotations, status, value)
	return m.sender.Send(ctx, notification).Err()
}

func (m *Manager) buildNotification(ctx context.Context, alert *models.Alert, history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) AlertNotification {
	recipientEmails, missingRecipients, recipientTargets := m.resolveRecipientEmails(ctx, alert)
	sourceName := labels["source"]

	return AlertNotification{
		AlertID:                 alert.ID,
		AlertName:               alert.Name,
		Description:             strings.TrimSpace(alert.Description),
		Status:                  status,
		Severity:                alert.Severity,
		SourceID:                alert.SourceID,
		SourceName:              sourceName,
		Value:                   value,
		ThresholdOp:             alert.ThresholdOperator,
		ThresholdValue:          alert.ThresholdValue,
		FrequencySecs:           alert.FrequencySeconds,
		LookbackSecs:            alert.LookbackSeconds,
		Query:                   strings.TrimSpace(alert.Query),
		ConditionJSON:           strings.TrimSpace(alert.ConditionJSON),
		Labels:                  copyStringMap(labels),
		Annotations:             copyStringMap(annotations),
		TriggeredAt:             history.TriggeredAt,
		ResolvedAt:              history.ResolvedAt,
		GeneratorURL:            m.generatorURL(ctx, alert),
		Message:                 history.Message,
		RecipientUserIDs:        append([]models.UserID(nil), alert.RecipientUserIDs...),
		RecipientEmails:         recipientEmails,
		recipientTargets:        recipientTargets,
		MissingRecipientUserIDs: missingRecipients,
		WebhookURLs:             append([]string(nil), alert.WebhookURLs...),
	}
}

func (m *Manager) resolveRecipientEmails(ctx context.Context, alert *models.Alert) (recipientEmails []string, missingRecipients []models.UserID, targets map[string]DeliveryTargetID) {
	if alert == nil || len(alert.RecipientUserIDs) == 0 {
		return nil, nil, nil
	}

	// Recipients are users (not team-scoped). Resolve each by id directly.
	seen := make(map[string]struct{}, len(alert.RecipientUserIDs))
	emails := make([]string, 0, len(alert.RecipientUserIDs))
	targets = make(map[string]DeliveryTargetID)
	missing := make([]models.UserID, 0)
	for _, userID := range alert.RecipientUserIDs {
		user, err := m.db.GetUser(ctx, userID)
		if err != nil || user == nil {
			missing = append(missing, userID)
			continue
		}
		email := strings.TrimSpace(user.Email)
		if email == "" {
			missing = append(missing, userID)
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
		targets[email] = emailUserTarget(userID)
	}

	return emails, missing, targets
}

func (m *Manager) generatorURL(ctx context.Context, alert *models.Alert) string {
	if trimmed := strings.TrimSpace(alert.GeneratorURL); trimmed != "" {
		return trimmed
	}

	base := strings.TrimSpace(m.db.GetSettingWithDefault(ctx, "alerts.frontend_url", ""))
	if base == "" {
		base = strings.TrimSpace(m.db.GetSettingWithDefault(ctx, "alerts.external_url", ""))
	}
	if base == "" {
		return ""
	}

	base = strings.TrimSuffix(base, "/")
	return fmt.Sprintf("%s/logs/alerts/%d?source=%d", base, alert.ID, alert.SourceID)
}

type noopSender struct{}

func (noopSender) Send(_ context.Context, _ AlertNotification) DeliveryResults {
	return nil
}

func (m *Manager) ManualResolve(ctx context.Context, alertID models.AlertID, message string) error {
	alert, err := m.db.GetAlert(ctx, alertID)
	if err != nil {
		return fmt.Errorf("failed to get alert: %w", err)
	}

	entry, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alertID)
	if err != nil {
		if models.IsNotFound(err) {
			return fmt.Errorf("no active alert to resolve")
		}
		return fmt.Errorf("failed to find unresolved alert history: %w", err)
	}

	// Update the history entry in the database
	if err := m.db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		if models.IsNotFound(err) {
			return fmt.Errorf("alert history entry not found")
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}

	now := time.Now().UTC()
	entry.Message = message
	entry.ResolvedAt = &now
	entry.Status = models.AlertStatusResolved

	// Get the current value if available, otherwise use 0
	value := float64(0)
	if entry.Value != nil {
		value = *entry.Value
	}

	labels, annotations := m.buildAlertMetadata(ctx, alert, models.AlertStatusResolved, value)
	if annotations == nil {
		annotations = make(map[string]string, 2)
	}
	annotations["resolved_at"] = now.Format(time.RFC3339Nano)
	annotations["resolved_by"] = "manual"

	if sendErr := m.sendNotification(ctx, alert, entry, labels, annotations, models.AlertStatusResolved, value); sendErr != nil {
		m.log.Warn("failed to send manual resolution notifications", "alert_id", alertID, "error", sendErr)
	} else {
		m.log.Debug("manual resolution notifications sent", "alert_id", alertID)
	}

	return nil
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}
