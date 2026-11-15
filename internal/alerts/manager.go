package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// alertEvaluationTimeout bounds a single alert's evaluation wall-clock time. It
// sits above the per-query timeout (models.DefaultQueryTimeoutSeconds) with
// headroom for result processing, so a query's own timeout normally fires
// first; this deadline is the backstop that stops a wedged source from blocking
// the sequential evaluation loop.
const alertEvaluationTimeout = (models.DefaultQueryTimeoutSeconds + 30) * time.Second

const maxDeliveryAttempts = 3

const deliveryBookkeepingTimeout = 2 * time.Second

const deliveryAttemptTimeout = alertEvaluationTimeout + smtpDialTimeout + deliveryBookkeepingTimeout

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
	if m.stopped || m.cancel != nil {
		m.lifecycleMu.Unlock()
		return
	}
	if !m.cfg.Enabled {
		m.lifecycleMu.Unlock()
		m.log.Debug("alerting disabled")
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	interval := m.cfg.EvaluationInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.wg.Add(1)
	m.lifecycleMu.Unlock()

	// One manager owns delivery reservations, as required by docs/src/content/docs/operations/database-backends.md.
	recoveryCtx, cancel := context.WithTimeout(ctx, alertEvaluationTimeout)
	err := m.db.RecoverAlertDeliveries(recoveryCtx)
	cancel()
	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Error("failed to recover interrupted alert deliveries", "error", err)
	}

	m.log.Debug("starting alert manager", "interval", interval)

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
			case <-ctx.Done():
				m.log.Debug("alert manager context cancelled")
				return
			}
		}
	}()
}

// Stop signals the manager to stop evaluating alerts.
func (m *Manager) Stop() {
	_ = m.StopWithContext(context.Background())
}

// StopWithContext cancels evaluation and waits within the shutdown budget.
func (m *Manager) StopWithContext(ctx context.Context) error {
	m.lifecycleMu.Lock()
	m.stopped = true
	if m.cancel != nil {
		m.cancel()
	}
	m.lifecycleMu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) evaluateCycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	alerts, err := m.db.ListActiveAlertsDue(ctx)
	if err != nil {
		m.log.Log(ctx, evaluationLogLevel(ctx, err), "failed to fetch alerts for evaluation", "error", err)
		return
	}
	if len(alerts) == 0 {
		return
	}

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}
		if err := m.evaluateAlertWithTimeout(ctx, alert); err != nil {
			m.log.Log(ctx, evaluationLogLevel(ctx, err), "alert evaluation failed", "alert_id", alert.ID, "error", err)
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
		return fmt.Errorf("failed to find unresolved alert history: %w", err)
	}

	// Update the history entry in the database
	if err := m.db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
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
