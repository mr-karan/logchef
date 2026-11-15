package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// InsertAuditEvent stores one event. It truncates e.OccurredAt to
// models.AuditTimePrecision in UTC, the value that a read returns.
func (db *DB) InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error {
	if err := e.Validate(); err != nil {
		return err
	}
	e.OccurredAt = e.OccurredAt.UTC().Truncate(models.AuditTimePrecision)
	err := db.writeQueries.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		ID:           e.ID,
		OccurredAt:   formatSortableTime(e.OccurredAt),
		Action:       e.Action,
		Outcome:      string(e.Outcome),
		Reason:       e.Reason,
		ActorType:    string(e.ActorType),
		ActorID:      auditNullID(e.ActorID),
		ActorLabel:   e.ActorLabel,
		AuthMethod:   string(e.AuthMethod),
		CredentialID: e.CredentialID,
		ClientID:     e.ClientID,
		TargetType:   e.TargetType,
		TargetID:     e.TargetID,
		TargetLabel:  e.TargetLabel,
		TeamID:       auditNullID(e.TeamID),
		SourceID:     auditNullID(e.SourceID),
		Ip:           e.IP,
		UserAgent:    e.UserAgent,
		HttpMethod:   e.HTTPMethod,
		Route:        e.Route,
		Status:       int64(e.Status),
		Changes:      string(models.AuditJSONObject(e.Changes)),
		Detail:       string(models.AuditJSONObject(e.Detail)),
	})
	if err != nil {
		return fmt.Errorf("error inserting audit event: %w", translateBusy(err))
	}
	return nil
}

// ListAuditEvents returns one page of the events that match f.
func (db *DB) ListAuditEvents(ctx context.Context, f models.AuditFilter) ([]models.AuditEvent, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	m := auditFilterArgs(f)
	var cursorAt, cursorID string
	if f.Cursor != nil {
		cursorAt, cursorID = formatSortableTime(f.Cursor.OccurredAt), f.Cursor.ID
	}
	var rows []sqlc.AuditEvent
	var err error
	if f.Ascending {
		rows, err = db.readQueries.ListAuditEventsAsc(ctx, sqlc.ListAuditEventsAscParams{
			FromAt: m.from, ToAt: m.to, Action: f.Action, ActionPrefix: f.ActionPrefix,
			ActorID: m.actorID, AuthMethod: string(f.AuthMethod), CredentialID: f.CredentialID,
			TargetType: f.TargetType, TargetID: f.TargetID, TeamID: m.teamID, SourceID: m.sourceID,
			Outcome: string(f.Outcome), CursorID: cursorID, CursorAt: cursorAt, RowLimit: int64(f.Limit),
		})
	} else {
		rows, err = db.readQueries.ListAuditEventsDesc(ctx, sqlc.ListAuditEventsDescParams{
			FromAt: m.from, ToAt: m.to, Action: f.Action, ActionPrefix: f.ActionPrefix,
			ActorID: m.actorID, AuthMethod: string(f.AuthMethod), CredentialID: f.CredentialID,
			TargetType: f.TargetType, TargetID: f.TargetID, TeamID: m.teamID, SourceID: m.sourceID,
			Outcome: string(f.Outcome), CursorID: cursorID, CursorAt: cursorAt, RowLimit: int64(f.Limit),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("error listing audit events: %w", err)
	}
	events := make([]models.AuditEvent, 0, len(rows))
	for i := range rows {
		events = append(events, auditEventFromSQLC(rows[i]))
	}
	return events, nil
}

// CountAuditEvents counts the events that match f, up to maxRows. It ignores
// f.Cursor, f.Ascending and f.Limit.
func (db *DB) CountAuditEvents(ctx context.Context, f models.AuditFilter, maxRows int) (int, error) {
	if maxRows <= 0 {
		return 0, errors.New("audit count: maxRows must be positive")
	}
	m := auditFilterArgs(f)
	n, err := db.readQueries.CountAuditEvents(ctx, sqlc.CountAuditEventsParams{
		FromAt: m.from, ToAt: m.to, Action: f.Action, ActionPrefix: f.ActionPrefix,
		ActorID: m.actorID, AuthMethod: string(f.AuthMethod), CredentialID: f.CredentialID,
		TargetType: f.TargetType, TargetID: f.TargetID, TeamID: m.teamID, SourceID: m.sourceID,
		Outcome: string(f.Outcome), MaxRows: int64(maxRows),
	})
	if err != nil {
		return 0, fmt.Errorf("error counting audit events: %w", err)
	}
	return int(n), nil
}

// PruneAuditEvents deletes up to batch of the oldest events that are older
// than before, or that are not among the keepRows newest. A zero before or
// keepRows disables that rule. It returns the number of deleted rows.
func (db *DB) PruneAuditEvents(ctx context.Context, before time.Time, keepRows, batch int) (int, error) {
	if batch <= 0 || keepRows < 0 {
		return 0, errors.New("audit prune: batch must be positive and keepRows not negative")
	}
	args := sqlc.PruneAuditEventsParams{BatchSize: int64(batch)}
	if !before.IsZero() {
		args.Before = formatAuditBound(before)
	}
	if keepRows > 0 {
		bound, err := db.readQueries.GetAuditPruneBoundary(ctx, int64(keepRows))
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return 0, fmt.Errorf("error finding audit prune boundary: %w", err)
		default:
			args.BoundAt, args.BoundID = bound.OccurredAtText, bound.ID
		}
	}
	if args.Before == "" && args.BoundID == "" {
		return 0, nil
	}
	n, err := db.writeQueries.PruneAuditEvents(ctx, args)
	if err != nil {
		return 0, fmt.Errorf("error pruning audit events: %w", translateBusy(err))
	}
	return int(n), nil
}

type auditSQLiteArgs struct {
	from, to                  string
	actorID, teamID, sourceID sql.NullInt64
}

func auditFilterArgs(f models.AuditFilter) auditSQLiteArgs {
	var a auditSQLiteArgs
	if !f.From.IsZero() {
		a.from = formatAuditBound(f.From)
	}
	if !f.To.IsZero() {
		a.to = formatAuditBound(f.To)
	}
	a.actorID, a.teamID, a.sourceID = auditNullID(f.ActorID), auditNullID(f.TeamID), auditNullID(f.SourceID)
	return a
}

// formatAuditBound formats a range bound for comparison with occurred_at.
// Stored times are whole milliseconds, so for a bound t, occurred_at >= t and
// occurred_at < t hold exactly when they hold for t rounded up to the next
// millisecond. Truncating t instead would move a sub-millisecond bound.
func formatAuditBound(t time.Time) string {
	if r := t.Truncate(models.AuditTimePrecision); !r.Equal(t) {
		t = r.Add(models.AuditTimePrecision)
	}
	return formatSortableTime(t)
}

func auditNullID[T ~int](id *T) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func auditIDPtr[T ~int](v sql.NullInt64) *T {
	if !v.Valid {
		return nil
	}
	id := T(v.Int64)
	return &id
}

func auditEventFromSQLC(r sqlc.AuditEvent) models.AuditEvent {
	return models.AuditEvent{
		ID:           r.ID,
		OccurredAt:   r.OccurredAt.UTC(),
		Action:       r.Action,
		Outcome:      models.AuditOutcome(r.Outcome),
		Reason:       r.Reason,
		ActorType:    models.AuditActorType(r.ActorType),
		ActorID:      auditIDPtr[models.UserID](r.ActorID),
		ActorLabel:   r.ActorLabel,
		AuthMethod:   models.AuditAuthMethod(r.AuthMethod),
		CredentialID: r.CredentialID,
		ClientID:     r.ClientID,
		TargetType:   r.TargetType,
		TargetID:     r.TargetID,
		TargetLabel:  r.TargetLabel,
		TeamID:       auditIDPtr[models.TeamID](r.TeamID),
		SourceID:     auditIDPtr[models.SourceID](r.SourceID),
		IP:           r.Ip,
		UserAgent:    r.UserAgent,
		HTTPMethod:   r.HttpMethod,
		Route:        r.Route,
		Status:       int(r.Status),
		Changes:      []byte(r.Changes),
		Detail:       []byte(r.Detail),
	}
}
