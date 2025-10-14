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
	"github.com/mr-karan/logchef/pkg/models"
)

// Options encapsulates the dependencies required to run the alerting manager.
type Options struct {
	Config     config.AlertsConfig
	DB         *sqlite.DB
	ClickHouse *clickhouse.Manager
	Logger     *slog.Logger
	Notifier   Notifier
}

// Manager coordinates alert evaluation and dispatches notifications when thresholds are met.
type Manager struct {
	cfg        config.AlertsConfig
	db         *sqlite.DB
	clickhouse *clickhouse.Manager
	log        *slog.Logger
	notifier   Notifier

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewManager constructs a new alert manager instance.
func NewManager(opts Options) *Manager {
	notifier := opts.Notifier
	if notifier == nil {
		notifier = NewDefaultNotifier(opts.Logger)
	}
	return &Manager{
		cfg:        opts.Config,
		db:         opts.DB,
		clickhouse: opts.ClickHouse,
		log:        opts.Logger.With("component", "alert_manager"),
		notifier:   notifier,
		stop:       make(chan struct{}),
	}
}

// Start launches the evaluation loop. It is a no-op when alerting is disabled.
func (m *Manager) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		m.log.Info("alerting disabled; manager will not start")
		return
	}
	interval := m.cfg.EvaluationInterval
	if interval <= 0 {
		interval = time.Minute
	}
	m.log.Info("starting alert manager", "interval", interval)

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
				m.log.Info("alert manager stopping")
				return
			case <-ctx.Done():
				m.log.Info("alert manager context cancelled")
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
		m.log.Debug("no alerts due for evaluation")
		return
	}

	for _, alert := range alerts {
		if err := m.evaluateAlert(ctx, alert); err != nil {
			m.log.Error("alert evaluation failed", "alert_id", alert.ID, "error", err)
		}
	}
}

func (m *Manager) evaluateAlert(ctx context.Context, alert *models.Alert) error {
	source, err := m.db.GetSource(ctx, alert.SourceID)
	if err != nil {
		return fmt.Errorf("failed to load source for alert %d: %w", alert.ID, err)
	}

	query, err := m.buildEvaluationQuery(alert, source)
	if err != nil {
		return err
	}

	client, err := m.clickhouse.GetConnection(alert.SourceID)
	if err != nil {
		return fmt.Errorf("failed to obtain ClickHouse connection: %w", err)
	}

	timeout := models.DefaultQueryTimeoutSeconds
	result, err := client.QueryWithTimeout(ctx, query, &timeout)
	if err != nil {
		return fmt.Errorf("alert query failed: %w", err)
	}

	value, err := extractFirstNumeric(result)
	if err != nil {
		return fmt.Errorf("failed to extract alert result: %w", err)
	}

	triggered := compareThreshold(value, alert.ThresholdValue, alert.ThresholdOperator)
	if triggered {
		return m.handleTriggered(ctx, alert, value)
	}
	return m.handleResolved(ctx, alert, value)
}

func (m *Manager) handleTriggered(ctx context.Context, alert *models.Alert, value float64) error {
	// Avoid duplicating history entries if the alert is already active.
	_, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		m.log.Warn("failed to check existing alert history", "alert_id", alert.ID, "error", err)
	}
	alreadyActive := false
	if err == nil {
		alreadyActive = true
	} else if errors.Is(err, sql.ErrNoRows) {
		history := &models.AlertHistoryEntry{
			AlertID:   alert.ID,
			Status:    models.AlertStatusTriggered,
			ValueText: strconv.FormatFloat(value, 'f', 4, 64),
			Channels:  alert.Channels,
			Message:   fmt.Sprintf("alert %s triggered with value %.4f", alert.Name, value),
		}
		if err := m.db.InsertAlertHistory(ctx, history); err != nil {
			m.log.Error("failed to insert alert history", "alert_id", alert.ID, "error", err)
		} else if pruneErr := m.db.PruneAlertHistory(ctx, alert.ID, m.cfg.HistoryLimit); pruneErr != nil {
			m.log.Warn("failed to prune alert history", "alert_id", alert.ID, "error", pruneErr)
		}
	}

	if err := m.db.MarkAlertTriggered(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert triggered", "alert_id", alert.ID, "error", err)
	}

	if alreadyActive {
		m.log.Debug("alert already active, suppressing duplicate notifications", "alert_id", alert.ID)
		return nil
	}

	timeout := m.cfg.NotificationTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	for _, ch := range alert.Channels {
		notifyCtx, cancel := context.WithTimeout(ctx, timeout)
		go func(channel models.AlertChannel) {
			defer cancel()
			if notifyErr := m.notifier.Notify(notifyCtx, alert, channel, NotificationPayload{
				Value:   value,
				Message: fmt.Sprintf("Alert %s triggered", alert.Name),
			}); notifyErr != nil {
				m.log.Warn("notification failed", "alert_id", alert.ID, "channel", channel.Type, "target", channel.Target, "error", notifyErr)
			}
		}(ch)
	}
	return nil
}

func (m *Manager) handleResolved(ctx context.Context, alert *models.Alert, value float64) error {
	if err := m.db.MarkAlertEvaluated(ctx, alert.ID); err != nil {
		m.log.Error("failed to mark alert evaluated", "alert_id", alert.ID, "error", err)
	}

	entry, err := m.db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("failed to fetch unresolved alert history: %w", err)
	}

	message := fmt.Sprintf("alert %s resolved with value %.4f", alert.Name, value)
	if err := m.db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	return nil
}

func (m *Manager) buildEvaluationQuery(alert *models.Alert, source *models.Source) (string, error) {
	switch alert.QueryType {
	case models.AlertQueryTypeSQL:
		return alert.Query, nil
	case models.AlertQueryTypeLogCondition:
		lookback := alert.LookbackSeconds
		if lookback <= 0 {
			lookback = int(m.cfg.DefaultLookback.Seconds())
		}
		condition := strings.TrimSpace(alert.Query)
		if condition == "" {
			return "", fmt.Errorf("log condition alert requires a query filter")
		}
		tsField := source.MetaTSField
		if tsField == "" {
			return "", fmt.Errorf("source missing timestamp field for log condition alerts")
		}
		table := source.GetFullTableName()
		return fmt.Sprintf("SELECT count(*) AS value FROM %s WHERE (%s) AND %s >= now() - INTERVAL %d SECOND", table, condition, tsField, lookback), nil
	default:
		return "", fmt.Errorf("unsupported alert query type %q", alert.QueryType)
	}
}

func extractFirstNumeric(result *models.QueryResult) (float64, error) {
	if result == nil || len(result.Logs) == 0 {
		return 0, fmt.Errorf("query returned no rows")
	}
	row := result.Logs[0]
	if len(result.Columns) == 0 {
		return 0, fmt.Errorf("query returned no columns")
	}
	firstColumn := result.Columns[0].Name
	rawValue, ok := row[firstColumn]
	if !ok {
		for _, v := range row {
			rawValue = v
			ok = true
			break
		}
	}
	if !ok {
		return 0, fmt.Errorf("unable to locate numeric value in query result")
	}
	switch v := rawValue.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse numeric value: %w", err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported result type %T", rawValue)
	}
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
