package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// alertEvaluationTimeout bounds a single alert's evaluation wall-clock time. It
// sits above the per-query timeout (models.DefaultQueryTimeoutSeconds) with
// headroom for result processing, so a query's own timeout normally fires
// first; this deadline is the backstop that frees the job runner's slot when a
// source wedges.
const alertEvaluationTimeout = (models.DefaultQueryTimeoutSeconds + 30) * time.Second

// kickTimeout bounds the write that makes the delivery drain due now.
const kickTimeout = 2 * time.Second

// Kicker makes a job due now. *jobs.Runner is one; it also wakes its local
// loop.
type Kicker interface {
	Kick(ctx context.Context, kind jobs.Kind, key string) error
}

// Options encapsulates the dependencies required to run the alerting manager.
type Options struct {
	Config      config.AlertsConfig
	DB          store.Store
	Datasources *datasource.Service
	Logger      *slog.Logger
	// Senders send destination tests. Notifications go through the delivery
	// queue and its drain.
	Senders map[models.DestinationType]notify.Sender
	// Jobs kicks the delivery drain after a commit that enqueued rows. Nil
	// makes the drain due through the store, which a replica that runs jobs
	// picks up at its next poll.
	Jobs Kicker
}

// Manager evaluates alert rules, as the handler of the alert.evaluate job,
// and enqueues their notifications.
type Manager struct {
	cfg        config.AlertsConfig
	db         store.Store
	datasource *datasource.Service
	log        *slog.Logger
	senders    map[models.DestinationType]notify.Sender
	jobs       Kicker
}

// NewManager constructs a new alert manager instance.
func NewManager(opts Options) *Manager {
	return &Manager{
		cfg:        opts.Config,
		db:         opts.DB,
		datasource: opts.Datasources,
		log:        opts.Logger.With("component", "alert_manager"),
		senders:    opts.Senders,
		jobs:       opts.Jobs,
	}
}

// kickDrain makes the delivery drain due now, so enqueued rows go out without
// waiting for its interval. A failure only delays them.
func (m *Manager) kickDrain(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kickTimeout)
	defer cancel()
	var err error
	if m.jobs != nil {
		err = m.jobs.Kick(ctx, notify.DeliverKind, notify.DrainKey)
	} else {
		err = m.db.KickJob(ctx, string(notify.DeliverKind), notify.DrainKey)
	}
	if err != nil {
		m.log.Warn("failed to kick the delivery drain; it runs at its interval", "error", err)
	}
}

// ManualResolve closes the open episode of one group, or of every group when
// groupKey is nil. When the rule notifies on resolve, it enqueues one resolved
// notification per closed group that was announced and that no silence mutes
// now, in the same transaction. It returns models.ErrNotFound when nothing is
// open.
func (m *Manager) ManualResolve(ctx context.Context, alertID models.AlertID, groupKey *string, message string) error {
	alert, err := m.db.GetAlert(ctx, alertID)
	if err != nil {
		return fmt.Errorf("failed to get alert: %w", err)
	}
	silences, err := m.db.ListAlertSilences(ctx, alert.SourceID)
	if err != nil {
		return fmt.Errorf("failed to list alert silences: %w", err)
	}
	var mc *messageContext
	if alert.NotifyOnResolve {
		mc = m.newMessageContext(ctx, alert)
	}
	now := time.Now().UTC()
	out := &outbox{alert: alert}
	// Close exactly the groups that were read, in one transaction, so every
	// closed group gets its resolved notification. A group that opens after
	// the read stays open.
	err = m.db.WithTx(ctx, func(tx store.StoreOps) error {
		open, err := tx.ListOpenAlertHistory(ctx, alertID)
		if err != nil {
			return err
		}
		entries := newestPerGroup(open, groupKey)
		if len(entries) == 0 {
			return models.ErrNotFound
		}
		instances, err := tx.ListAlertInstances(ctx, alertID)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			announced := announcedEpisode(instances, entry)
			if err := tx.ResolveAlertGroup(ctx, alertID, entry.GroupKey, message); err != nil {
				return err
			}
			if !alert.NotifyOnResolve || !announced {
				continue
			}
			if silenced := m.silencedBy(alert, silences, entry.Group, now); len(silenced) > 0 {
				m.log.Debug("silenced manual resolve", "alert_id", alertID, "group_key", entry.GroupKey, "silences", silenced)
				continue
			}
			resolved := *entry
			resolved.Message = message
			resolved.ResolvedAt = &now
			resolved.Status = models.AlertStatusResolved
			value := float64(0)
			if entry.Value != nil {
				value = *entry.Value
			}
			n := mc.resolvedNotification(&resolved, value, map[string]string{"resolved_by": "manual"})
			if err := out.add(ctx, tx, n, entry, "manual"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	if out.enqueued > 0 {
		m.kickDrain(ctx)
	}
	return nil
}

// announcedEpisode reports whether the open episode entry was announced: its
// instance is notified. An episode without its instance predates instances
// and counts as announced, as currentInstance adopts it.
func announcedEpisode(instances []*models.AlertInstance, entry *models.AlertHistoryEntry) bool {
	for _, instance := range instances {
		if instance.GroupKey == entry.GroupKey && instance.HistoryID != nil && *instance.HistoryID == entry.ID {
			return instance.Notified
		}
	}
	return true
}

// newestPerGroup keeps the newest open row of each group, in the order of
// open (newest first), and only the given group when groupKey is set.
func newestPerGroup(open []*models.AlertHistoryEntry, groupKey *string) []*models.AlertHistoryEntry {
	seen := make(map[string]struct{}, len(open))
	entries := make([]*models.AlertHistoryEntry, 0, len(open))
	for _, entry := range open {
		if groupKey != nil && entry.GroupKey != *groupKey {
			continue
		}
		if _, ok := seen[entry.GroupKey]; ok {
			continue
		}
		seen[entry.GroupKey] = struct{}{}
		entries = append(entries, entry)
	}
	return entries
}
