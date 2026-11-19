package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// InsertAuditEvent stores one event. It truncates e.OccurredAt to
// models.AuditTimePrecision in UTC, the value that a read returns.
func (s *Store) InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error {
	if err := e.Validate(); err != nil {
		return err
	}
	e.OccurredAt = e.OccurredAt.UTC().Truncate(models.AuditTimePrecision)
	err := s.q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		ID:           e.ID,
		OccurredAt:   ts(e.OccurredAt),
		Action:       e.Action,
		Outcome:      string(e.Outcome),
		Reason:       e.Reason,
		ActorType:    string(e.ActorType),
		ActorID:      auditInt8(e.ActorID),
		ActorLabel:   e.ActorLabel,
		AuthMethod:   string(e.AuthMethod),
		CredentialID: e.CredentialID,
		ClientID:     e.ClientID,
		TargetType:   e.TargetType,
		TargetID:     e.TargetID,
		TargetLabel:  e.TargetLabel,
		TeamID:       auditInt8(e.TeamID),
		SourceID:     auditInt8(e.SourceID),
		Ip:           e.IP,
		UserAgent:    e.UserAgent,
		HttpMethod:   e.HTTPMethod,
		Route:        e.Route,
		Status:       int32(e.Status), //nolint:gosec // G115: Validate bounds the HTTP status
		Changes:      models.AuditJSONObject(e.Changes),
		Detail:       models.AuditJSONObject(e.Detail),
	})
	if err != nil {
		return fmt.Errorf("error inserting audit event: %w", err)
	}
	return nil
}

// ListAuditEvents returns one page of the events that match f.
func (s *Store) ListAuditEvents(ctx context.Context, f models.AuditFilter) ([]models.AuditEvent, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	m := auditFilterArgs(f)
	var cursorAt pgtype.Timestamptz
	var cursorID string
	if f.Cursor != nil {
		cursorAt, cursorID = ts(f.Cursor.OccurredAt), f.Cursor.ID
	}
	var rows []sqlc.AuditEvent
	var err error
	if f.Ascending {
		rows, err = s.q.ListAuditEventsAsc(ctx, sqlc.ListAuditEventsAscParams{
			FromAt: m.from, ToAt: m.to, Action: f.Action, ActionPrefix: f.ActionPrefix,
			ActorID: m.actorID, AuthMethod: string(f.AuthMethod), CredentialID: f.CredentialID,
			TargetType: f.TargetType, TargetID: f.TargetID, TeamID: m.teamID, SourceID: m.sourceID,
			Outcome: string(f.Outcome), CursorAt: cursorAt, CursorID: cursorID, RowLimit: int64(f.Limit),
		})
	} else {
		rows, err = s.q.ListAuditEventsDesc(ctx, sqlc.ListAuditEventsDescParams{
			FromAt: m.from, ToAt: m.to, Action: f.Action, ActionPrefix: f.ActionPrefix,
			ActorID: m.actorID, AuthMethod: string(f.AuthMethod), CredentialID: f.CredentialID,
			TargetType: f.TargetType, TargetID: f.TargetID, TeamID: m.teamID, SourceID: m.sourceID,
			Outcome: string(f.Outcome), CursorAt: cursorAt, CursorID: cursorID, RowLimit: int64(f.Limit),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("error listing audit events: %w", err)
	}
	events := make([]models.AuditEvent, 0, len(rows))
	for i := range rows {
		events = append(events, auditEventToModel(rows[i]))
	}
	return events, nil
}

// CountAuditEvents counts the events that match f, up to maxRows. It ignores
// f.Cursor, f.Ascending and f.Limit.
func (s *Store) CountAuditEvents(ctx context.Context, f models.AuditFilter, maxRows int) (int, error) {
	if maxRows <= 0 {
		return 0, errors.New("audit count: maxRows must be positive")
	}
	m := auditFilterArgs(f)
	n, err := s.q.CountAuditEvents(ctx, sqlc.CountAuditEventsParams{
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
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time, keepRows, batch int) (int, error) {
	if batch <= 0 || keepRows < 0 {
		return 0, errors.New("audit prune: batch must be positive and keepRows not negative")
	}
	args := sqlc.PruneAuditEventsParams{BatchSize: int64(batch)}
	if !before.IsZero() {
		args.Before = ts(before)
	}
	if keepRows > 0 {
		bound, err := s.q.GetAuditPruneBoundary(ctx, int64(keepRows))
		switch {
		case notFound(err):
		case err != nil:
			return 0, fmt.Errorf("error finding audit prune boundary: %w", err)
		default:
			args.BoundAt, args.BoundID = bound.OccurredAt, bound.ID
		}
	}
	if !args.Before.Valid && !args.BoundAt.Valid {
		return 0, nil
	}
	n, err := s.q.PruneAuditEvents(ctx, args)
	if err != nil {
		return 0, fmt.Errorf("error pruning audit events: %w", err)
	}
	return int(n), nil
}

type auditPostgresArgs struct {
	from, to                  pgtype.Timestamptz
	actorID, teamID, sourceID pgtype.Int8
}

func auditFilterArgs(f models.AuditFilter) auditPostgresArgs {
	var a auditPostgresArgs
	if !f.From.IsZero() {
		a.from = ts(f.From)
	}
	if !f.To.IsZero() {
		a.to = ts(f.To)
	}
	a.actorID, a.teamID, a.sourceID = auditInt8(f.ActorID), auditInt8(f.TeamID), auditInt8(f.SourceID)
	return a
}

func auditInt8[T ~int](id *T) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return int8Val(int64(*id))
}

func auditIDPtr[T ~int](v pgtype.Int8) *T {
	if !v.Valid {
		return nil
	}
	id := T(v.Int64)
	return &id
}

func auditEventToModel(r sqlc.AuditEvent) models.AuditEvent {
	return models.AuditEvent{
		ID:           r.ID,
		OccurredAt:   r.OccurredAt.Time.UTC(),
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
		Changes:      r.Changes,
		Detail:       r.Detail,
	}
}
