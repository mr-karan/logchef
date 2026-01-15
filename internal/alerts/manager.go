package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// Options encapsulates the dependencies required to run the alerting manager.
type Options struct {
	Config     config.AlertsConfig
	DB         *sqlite.DB
	ClickHouse *clickhouse.Manager
	Logger     *slog.Logger
	Sender     AlertSender
}

// Manager coordinates alert evaluation and dispatches notifications when thresholds are met.
type Manager struct {
	cfg        config.AlertsConfig
	db         *sqlite.DB
	clickhouse *clickhouse.Manager
	log        *slog.Logger
	sender     AlertSender

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewManager constructs a new alert manager instance.
func NewManager(opts Options) *Manager {
	sender := opts.Sender
	if sender == nil {
		sender = noopSender{}
	}
	return &Manager{
		cfg:        opts.Config,
		db:         opts.DB,
		clickhouse: opts.ClickHouse,
		log:        opts.Logger.With("component", "alert_manager"),
		sender:     sender,
		stop:       make(chan struct{}),
	}
}

// Start launches the evaluation loop. It is a no-op when alerting is disabled.
func (m *Manager) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		m.log.Debug("alerting disabled")
		return
	}
	interval := m.cfg.EvaluationInterval
	if interval <= 0 {
		interval = time.Minute
	}
	m.log.Debug("starting alert manager", "interval", interval)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run an initial evaluation so alerts fire soon after startup.
		m.evaluateCycle(ctx)

		for {
			select {
			case <-ticker.C:
				m.evaluateCycle(ctx)
			case <-m.stop:
				m.log.Debug("alert manager stopping")
				return
			case <-ctx.Done():
				m.log.Debug("alert manager context cancelled")
				return
			}
		}
	}()
}

// Stop signals the manager to stop evaluating alerts.
func (m *Manager) Stop() {
	close(m.stop)
	m.wg.Wait()
}

func (m *Manager) evaluateCycle(ctx context.Context) {
	alerts, err := m.db.ListActiveAlertsDue(ctx)
	if err != nil {
		m.log.Error("failed to fetch alerts for evaluation", "error", err)
		return
	}
	if len(alerts) == 0 {
		return
	}

	for _, alert := range alerts {
		if err := m.evaluateAlert(ctx, alert); err != nil {
			m.log.Error("alert evaluation failed", "alert_id", alert.ID, "error", err)
		}
	}
}

func (m *Manager) evaluateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return nil
	}

	if alert.QueryType != "" && alert.QueryType != models.AlertQueryTypeSQL {
		m.log.Warn("unsupported alert query type; skipping evaluation", "alert_id", alert.ID, "query_type", alert.QueryType)
		return nil
	}

	query := strings.TrimSpace(alert.Query)
	if query == "" {
		m.log.Warn("alert query is empty; skipping evaluation", "alert_id", alert.ID)
		return nil
	}

	client, err := m.clickhouse.GetConnection(alert.SourceID)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to obtain ClickHouse connection: %w", err))
		return fmt.Errorf("failed to obtain ClickHouse connection: %w", err)
	}

	timeout := models.DefaultQueryTimeoutSeconds
	result, err := client.QueryWithTimeout(ctx, query, &timeout)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("alert query failed: %w", err))
		return fmt.Errorf("alert query failed: %w", err)
	}

	value, err := util.ExtractFirstNumeric(result)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to extract alert result: %w", err))
		return fmt.Errorf("failed to extract alert result: %w", err)
	}

	triggered := compareThreshold(value, alert.ThresholdValue, alert.ThresholdOperator)

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
	if alert == nil || evalErr == nil {
		return
	}

	// Update last_evaluated_at even on error so the alert respects its frequency_seconds
	// instead of being re-evaluated every cycle
	if err := m.db.MarkAlertEvaluated(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert evaluated after error", "alert_id", alert.ID, "error", err)
	}

	errorMessage := fmt.Sprintf("Evaluation failed: %v", evalErr)
	errorPayload := map[string]any{
		"error":      evalErr.Error(),
		"query_type": string(alert.QueryType),
		"query":      alert.Query,
		"status":     string(models.AlertStatusError),
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
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqlite.ErrNotFound) {
		m.log.Warn("failed to check existing alert history", "alert_id", alert.ID, "error", err)
	}
	alreadyActive := err == nil && prevHistory != nil

	// Check if previous delivery failed - if so, we should retry
	shouldRetryDelivery := false
	if alreadyActive && prevHistory.Payload != nil {
		if deliveryFailed, ok := prevHistory.Payload["delivery_failed"].(bool); ok && deliveryFailed {
			m.log.Debug("retrying alert delivery", "alert_id", alert.ID, "history_id", prevHistory.ID)
			shouldRetryDelivery = true
		}
	}

	if markErr := m.db.MarkAlertTriggered(ctx, alert.ID); markErr != nil {
		m.log.Error("failed to mark alert triggered", "alert_id", alert.ID, "error", markErr)
	}

	// If already active and delivery succeeded previously, suppress duplicate notification
	if alreadyActive && !shouldRetryDelivery {
		m.log.Debug("alert already active with successful delivery, suppressing duplicate alert notification", "alert_id", alert.ID)
		return nil
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

	var deliveryErr error
	var history *models.AlertHistoryEntry
	if shouldRetryDelivery && prevHistory != nil {
		// Retry on existing history entry - update it with new attempt
		history = prevHistory
		deliveryErr = m.sendNotification(ctx, alert, history, labels, annotations, models.AlertStatusTriggered, value)
	} else {
		// Create new history entry
		now := time.Now().UTC()
		history = &models.AlertHistoryEntry{
			AlertID:     alert.ID,
			Status:      models.AlertStatusTriggered,
			TriggeredAt: now,
			Value:       &valueCopy,
			Message:     message,
		}
		deliveryErr = m.sendNotification(ctx, alert, history, labels, annotations, models.AlertStatusTriggered, value)
	}

	// Record history with delivery status
	historyPayload := map[string]any{
		"labels":          copyStringMap(labels),
		"annotations":     copyStringMap(annotations),
		"status":          string(models.AlertStatusTriggered),
		"delivery_failed": deliveryErr != nil,
	}
	if deliveryErr != nil {
		historyPayload["delivery_error"] = deliveryErr.Error()
		m.log.Warn("failed to send alert notifications", "alert_id", alert.ID, "error", deliveryErr)
	} else {
		m.log.Debug("alert notifications sent", "alert_id", alert.ID, "alert_name", alert.Name)
	}

	if !shouldRetryDelivery {
		// Insert new history entry
		_, insertErr := m.db.InsertAlertHistory(ctx, alert.ID, models.AlertStatusTriggered, &valueCopy, message, historyPayload)
		if insertErr != nil {
			m.log.Error("failed to insert alert history", "alert_id", alert.ID, "error", insertErr)
		} else {
			if pruneErr := m.db.PruneAlertHistory(ctx, alert.ID, m.cfg.HistoryLimit); pruneErr != nil {
				m.log.Warn("failed to prune alert history", "alert_id", alert.ID, "error", pruneErr)
			}
		}
	} else {
		// Update existing history entry to reflect the retry outcome
		if updateErr := m.db.UpdateAlertHistoryPayload(ctx, history.ID, historyPayload); updateErr != nil {
			m.log.Error("failed to update alert history payload after retry", "alert_id", alert.ID, "history_id", history.ID, "error", updateErr)
		}
	}

	return nil
}

func (m *Manager) handleResolved(ctx context.Context, alert *models.Alert, value float64) error {
	if err := m.db.MarkAlertEvaluated(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert evaluated", "alert_id", alert.ID, "error", err)
	}

	entry, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
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
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
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

	if team, err := m.db.GetTeam(ctx, alert.TeamID); err == nil && team != nil {
		labels["team"] = team.Name
		labels["team_id"] = strconv.FormatInt(int64(alert.TeamID), 10)
	} else {
		labels["team_id"] = strconv.FormatInt(int64(alert.TeamID), 10)
		m.log.Warn("failed to fetch team name for alert metadata", "team_id", alert.TeamID, "error", err)
	}

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
	return m.sender.Send(ctx, notification)
}

func (m *Manager) buildNotification(ctx context.Context, alert *models.Alert, history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) AlertNotification {
	recipientEmails, missingRecipients, resolutionErr := m.resolveRecipientEmails(ctx, alert)
	teamName := labels["team"]
	sourceName := labels["source"]

	return AlertNotification{
		AlertID:                 alert.ID,
		AlertName:               alert.Name,
		Description:             strings.TrimSpace(alert.Description),
		Status:                  status,
		Severity:                alert.Severity,
		TeamID:                  alert.TeamID,
		TeamName:                teamName,
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
		GeneratorURL:            m.generatorURL(alert),
		Message:                 history.Message,
		RecipientUserIDs:        append([]models.UserID(nil), alert.RecipientUserIDs...),
		RecipientEmails:         recipientEmails,
		MissingRecipientUserIDs: missingRecipients,
		RecipientResolutionErr:  resolutionErr,
		WebhookURLs:             append([]string(nil), alert.WebhookURLs...),
	}
}

func (m *Manager) resolveRecipientEmails(ctx context.Context, alert *models.Alert) (recipientEmails []string, missingRecipients []models.UserID, resolutionErr string) {
	if alert == nil || len(alert.RecipientUserIDs) == 0 {
		return nil, nil, ""
	}

	members, err := m.db.ListTeamMembersWithDetails(ctx, alert.TeamID)
	if err != nil {
		return nil, append([]models.UserID(nil), alert.RecipientUserIDs...), err.Error()
	}

	emailsByID := make(map[models.UserID]string, len(members))
	for _, member := range members {
		email := strings.TrimSpace(member.Email)
		if email == "" {
			continue
		}
		emailsByID[member.UserID] = email
	}

	seen := make(map[string]struct{}, len(alert.RecipientUserIDs))
	emails := make([]string, 0, len(alert.RecipientUserIDs))
	missing := make([]models.UserID, 0)
	for _, userID := range alert.RecipientUserIDs {
		email := emailsByID[userID]
		if email == "" {
			missing = append(missing, userID)
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}

	return emails, missing, ""
}

func (m *Manager) generatorURL(alert *models.Alert) string {
	if trimmed := strings.TrimSpace(alert.GeneratorURL); trimmed != "" {
		return trimmed
	}

	// Use frontend URL if configured, otherwise fall back to external URL
	base := strings.TrimSpace(m.cfg.FrontendURL)
	if base == "" {
		base = strings.TrimSpace(m.cfg.ExternalURL)
	}
	if base == "" {
		return ""
	}

	base = strings.TrimSuffix(base, "/")
	// Frontend format: /logs/alerts/:alertId?team=:teamId&source=:sourceId
	return fmt.Sprintf("%s/logs/alerts/%d?team=%d&source=%d", base, alert.ID, alert.TeamID, alert.SourceID)
}

type noopSender struct{}

func (noopSender) Send(_ context.Context, _ AlertNotification) error {
	return nil
}

func (m *Manager) ManualResolve(ctx context.Context, alertID models.AlertID, message string) error {
	alert, err := m.db.GetAlert(ctx, alertID)
	if err != nil {
		return fmt.Errorf("failed to get alert: %w", err)
	}

	entry, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alertID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return fmt.Errorf("no active alert to resolve")
		}
		return fmt.Errorf("failed to find unresolved alert history: %w", err)
	}

	// Update the history entry in the database
	if err := m.db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
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
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
func compareThreshold(value, threshold float64, operator models.AlertThresholdOperator) bool {
	switch operator {
	case models.AlertThresholdGreaterThan:
		return value > threshold
	case models.AlertThresholdGreaterThanOrEqual:
		return value >= threshold
	case models.AlertThresholdLessThan:
		return value < threshold
	case models.AlertThresholdLessThanOrEqual:
		return value <= threshold
	case models.AlertThresholdEqual:
		return math.Abs(value-threshold) < 1e-9
	case models.AlertThresholdNotEqual:
		return math.Abs(value-threshold) >= 1e-9
	default:
		return false
	}
}
