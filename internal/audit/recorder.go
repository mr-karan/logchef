package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// insertTimeout bounds one insert. The insert runs detached from the request
// context, so a client disconnect or a shutdown does not lose the event.
const insertTimeout = 2 * time.Second

// Options configures a Recorder.
type Options struct {
	Store  store.AuditStore
	Logger *slog.Logger
	// LogEvents writes each event as one "audit" log line.
	LogEvents bool
	// DemoReadOnly skips the table and writes the log line only: a read-only
	// demo shares one login and refuses changes anyway.
	DemoReadOnly bool
}

// Recorder writes audit events to the metadata store and to the log stream.
type Recorder struct {
	store     store.AuditStore
	log       *slog.Logger
	logEvents bool
	demo      bool
	now       func() time.Time
}

// New returns a Recorder.
func New(opts Options) *Recorder {
	return &Recorder{
		store:     opts.Store,
		log:       opts.Logger,
		logEvents: opts.LogEvents,
		demo:      opts.DemoReadOnly,
		now:       time.Now,
	}
}

// build turns an Event into the stored row. It fails only for an action that
// no constant registers or when no ID can be generated.
func (r *Recorder) build(e Event) (models.AuditEvent, error) {
	if _, ok := registry[e.Action]; !ok {
		return models.AuditEvent{}, fmt.Errorf("audit: unregistered action %q", e.Action)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return models.AuditEvent{}, fmt.Errorf("audit: generate event id: %w", err)
	}
	enc := encodeEvent(e)
	metrics.RecordAuditRedactions(enc.redactions)
	return models.AuditEvent{
		ID:           id.String(),
		OccurredAt:   r.now(),
		Action:       string(e.Action),
		Outcome:      e.Outcome,
		Reason:       e.Reason,
		ActorType:    e.Actor.Type,
		ActorID:      e.Actor.ID,
		ActorLabel:   truncate(e.Actor.Label),
		AuthMethod:   e.Actor.AuthMethod,
		CredentialID: e.Actor.CredentialID,
		ClientID:     e.Actor.ClientID,
		TargetType:   e.Target.Type,
		TargetID:     e.Target.ID,
		TargetLabel:  truncate(e.Target.Label),
		TeamID:       e.Target.TeamID,
		SourceID:     e.Target.SourceID,
		IP:           e.Request.IP,
		UserAgent:    truncateBytes(e.Request.UserAgent, 256),
		HTTPMethod:   e.Request.Method,
		Route:        e.Request.Route,
		Status:       e.Request.Status,
		Changes:      enc.changes,
		Detail:       enc.detail,
	}, nil
}

func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

func (r *Recorder) insert(ctx context.Context, ev *models.AuditEvent) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), insertTimeout)
	defer cancel()
	return r.store.InsertAuditEvent(ctx, ev)
}

// Record inserts the event on the root store, then writes the log line. It
// never returns an error: the change it describes is already committed, so a
// failed insert is logged and counted, and the line says stored false.
func (r *Recorder) Record(ctx context.Context, e Event) {
	ev, err := r.build(e)
	if err != nil {
		r.log.Error("audit event dropped", "action", e.Action, "error", err)
		metrics.RecordAuditEvent("failed")
		return
	}
	if r.demo {
		r.line(ctx, ev, false)
		return
	}
	if err := r.insert(ctx, &ev); err != nil {
		r.log.Error("audit event not stored", "action", ev.Action, "id", ev.ID, "error", err)
		metrics.RecordAuditEvent("failed")
		r.line(ctx, ev, false)
		return
	}
	metrics.RecordAuditEvent("stored")
	r.line(ctx, ev, true)
}

// RecordLimited writes the log line of an event that a rate bound kept out
// of the table, counted as limited. A failed sign-in is the only event that an
// unauthenticated caller can cause, so a credential-stuffing run must not be
// able to fill the table and push real events past audit.max_rows.
func (r *Recorder) RecordLimited(ctx context.Context, e Event) {
	ev, err := r.build(e)
	if err != nil {
		r.log.Error("audit event dropped", "action", e.Action, "error", err)
		metrics.RecordAuditEvent("failed")
		return
	}
	metrics.RecordAuditEvent("limited")
	r.line(ctx, ev, false)
}

// RecordOrFail is Record for an action that must not happen without a stored
// event. It returns the insert error and writes no log line on failure.
func (r *Recorder) RecordOrFail(ctx context.Context, e Event) error {
	ev, err := r.build(e)
	if err != nil {
		metrics.RecordAuditEvent("failed")
		return err
	}
	if r.demo {
		r.line(ctx, ev, false)
		return nil
	}
	if err := r.insert(ctx, &ev); err != nil {
		metrics.RecordAuditEvent("failed")
		return fmt.Errorf("audit: store %s event: %w", ev.Action, err)
	}
	metrics.RecordAuditEvent("stored")
	r.line(ctx, ev, true)
	return nil
}

// Batch inserts events through a WithTx handle and holds their log lines
// until Publish. An insert inside a transaction proves nothing until the
// commit, so a rollback or a failed commit leaves neither rows nor lines.
func (r *Recorder) Batch(tx store.AuditStore) *Batch {
	return &Batch{r: r, tx: tx}
}

// Batch is a set of events inserted in one transaction.
type Batch struct {
	r       *Recorder
	tx      store.AuditStore
	pending []models.AuditEvent
}

// Add inserts the event through the transaction. Return its error from the
// WithTx callback to abort the transaction.
func (b *Batch) Add(ctx context.Context, e Event) error {
	ev, err := b.r.build(e)
	if err != nil {
		return err
	}
	if !b.r.demo {
		ctx, cancel := context.WithTimeout(ctx, insertTimeout)
		defer cancel()
		if err := b.tx.InsertAuditEvent(ctx, &ev); err != nil {
			return fmt.Errorf("audit: store %s event: %w", ev.Action, err)
		}
	}
	b.pending = append(b.pending, ev)
	return nil
}

// Publish writes the log line of each added event. Call it only after WithTx
// returned nil.
func (b *Batch) Publish(ctx context.Context) {
	for i := range b.pending {
		if !b.r.demo {
			metrics.RecordAuditEvent("stored")
		}
		b.r.line(ctx, b.pending[i], !b.r.demo)
	}
	b.pending = nil
}

func (r *Recorder) line(ctx context.Context, ev models.AuditEvent, stored bool) {
	if !r.logEvents {
		return
	}
	r.log.LogAttrs(ctx, slog.LevelInfo, "audit",
		slog.String("id", ev.ID),
		slog.Time("occurred_at", ev.OccurredAt),
		slog.String("action", ev.Action),
		slog.String("outcome", string(ev.Outcome)),
		slog.String("reason", ev.Reason),
		slog.String("actor_type", string(ev.ActorType)),
		slog.Any("actor_id", ev.ActorID),
		slog.String("actor_label", ev.ActorLabel),
		slog.String("auth_method", string(ev.AuthMethod)),
		slog.String("credential_id", ev.CredentialID),
		slog.String("client_id", ev.ClientID),
		slog.String("target_type", ev.TargetType),
		slog.String("target_id", ev.TargetID),
		slog.String("target_label", ev.TargetLabel),
		slog.Any("team_id", ev.TeamID),
		slog.Any("source_id", ev.SourceID),
		slog.String("ip", ev.IP),
		slog.String("user_agent", ev.UserAgent),
		slog.String("http_method", ev.HTTPMethod),
		slog.String("route", ev.Route),
		slog.Int("status", ev.Status),
		slog.Any("changes", ev.Changes),
		slog.Any("detail", ev.Detail),
		slog.Bool("stored", stored),
	)
}
