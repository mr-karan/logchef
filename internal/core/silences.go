package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrInvalidSilence wraps a validation failure; the message names the field.
	ErrInvalidSilence = errors.New("invalid alert silence")
	// ErrSilenceNotFound means the silence does not exist or its source is not
	// visible to the caller.
	ErrSilenceNotFound = errors.New("alert silence not found")
	// ErrSilenceForbidden means the caller may see the silence but not edit it.
	ErrSilenceForbidden = errors.New("only the creator of a silence or a global admin can change it")
)

// ListSilencesForPrincipal returns the silences of a source the principal can
// see, with Active computed at now. It returns ErrSourceAccessDenied when the
// source is not visible.
func ListSilencesForPrincipal(ctx context.Context, db store.StoreOps, log *slog.Logger, p access.Principal, sourceID models.SourceID, now time.Time) ([]*models.AlertSilence, error) {
	if err := requireSourceVisible(ctx, db, p, sourceID); err != nil {
		return nil, err
	}
	list, err := db.ListAlertSilences(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert silences: %w", err)
	}
	for _, silence := range list {
		markActive(log, silence, now)
	}
	return list, nil
}

// CreateSilenceForPrincipal creates a silence on a source the principal can
// see. Anyone who sees the source and holds alerts:write may silence it, so
// an on-call engineer can mute a rule they did not write.
func CreateSilenceForPrincipal(ctx context.Context, db store.Store, log *slog.Logger, p access.Principal, req *models.AlertSilenceRequest, now time.Time) (*models.AlertSilence, error) {
	if err := p.Require(models.TokenScopeAlertsWrite); err != nil {
		return nil, err
	}
	if req == nil || req.SourceID <= 0 {
		return nil, fmt.Errorf("%w: source_id is required", ErrInvalidSilence)
	}
	if err := requireSourceVisible(ctx, db, p, req.SourceID); err != nil {
		return nil, err
	}
	silence := &models.AlertSilence{SourceID: req.SourceID}
	if err := applySilenceRequest(silence, req, now); err != nil {
		return nil, err
	}
	owner := p.User.ID
	silence.CreatedBy = &owner
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		// The source lock makes the count and the insert one step, so two
		// requests cannot both pass the per-source limit.
		if _, err := tx.GetSourceForUpdate(ctx, silence.SourceID); err != nil {
			return err
		}
		if err := checkSilenceRule(ctx, tx, silence); err != nil {
			return err
		}
		count, err := tx.CountAlertSilences(ctx, silence.SourceID)
		if err != nil {
			return err
		}
		if count >= models.MaxSilencesPerSource {
			return fmt.Errorf("%w: a source has at most %d silences; delete an old one first", ErrInvalidSilence, models.MaxSilencesPerSource)
		}
		return tx.CreateAlertSilence(ctx, silence)
	})
	if err != nil {
		return nil, err
	}
	markActive(log, silence, now)
	return silence, nil
}

// UpdateSilenceForPrincipal replaces a silence's rule, matchers, comment and
// schedule. The creator and global admins may change it; it keeps its source.
// It returns the silence before and after the change.
func UpdateSilenceForPrincipal(ctx context.Context, db store.Store, log *slog.Logger, p access.Principal, id models.AlertSilenceID, req *models.AlertSilenceRequest, now time.Time) (before, after *models.AlertSilence, err error) {
	before, err = editableSilence(ctx, db, p, id)
	if err != nil {
		return nil, nil, err
	}
	if req == nil {
		return nil, nil, fmt.Errorf("%w: request body is required", ErrInvalidSilence)
	}
	if req.SourceID != 0 && req.SourceID != before.SourceID {
		return nil, nil, fmt.Errorf("%w: a silence keeps its source; create a new silence for another source", ErrInvalidSilence)
	}
	after = &models.AlertSilence{ID: before.ID, SourceID: before.SourceID, CreatedBy: before.CreatedBy}
	if err := applySilenceRequest(after, req, now); err != nil {
		return nil, nil, err
	}
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := checkSilenceRule(ctx, tx, after); err != nil {
			return err
		}
		return tx.UpdateAlertSilence(ctx, after)
	})
	if models.IsNotFound(err) {
		return nil, nil, ErrSilenceNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	markActive(log, after, now)
	return before, after, nil
}

// DeleteSilenceForPrincipal deletes a silence the principal may change and
// returns it.
func DeleteSilenceForPrincipal(ctx context.Context, db store.Store, p access.Principal, id models.AlertSilenceID) (*models.AlertSilence, error) {
	silence, err := editableSilence(ctx, db, p, id)
	if err != nil {
		return nil, err
	}
	if err := db.DeleteAlertSilence(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrSilenceNotFound
		}
		return nil, fmt.Errorf("failed to delete alert silence: %w", err)
	}
	return silence, nil
}

// editableSilence loads a silence for a change: a silence whose source the
// principal cannot see is not found, and one the principal did not create is
// forbidden unless the principal is a global admin.
func editableSilence(ctx context.Context, db store.StoreOps, p access.Principal, id models.AlertSilenceID) (*models.AlertSilence, error) {
	if err := p.Require(models.TokenScopeAlertsWrite); err != nil {
		return nil, err
	}
	silence, err := db.GetAlertSilence(ctx, id)
	if models.IsNotFound(err) {
		return nil, ErrSilenceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load alert silence: %w", err)
	}
	if err := requireSourceVisible(ctx, db, p, silence.SourceID); err != nil {
		if errors.Is(err, ErrSourceAccessDenied) {
			return nil, ErrSilenceNotFound
		}
		return nil, err
	}
	if p.User.Role != models.UserRoleAdmin && (silence.CreatedBy == nil || *silence.CreatedBy != p.User.ID) {
		return nil, ErrSilenceForbidden
	}
	return silence, nil
}

// checkSilenceRule refuses a rule that is not on the silence's source.
func checkSilenceRule(ctx context.Context, tx store.StoreOps, silence *models.AlertSilence) error {
	if silence.AlertID == nil {
		return nil
	}
	alert, err := tx.GetAlert(ctx, *silence.AlertID)
	if models.IsNotFound(err) || (err == nil && alert.SourceID != silence.SourceID) {
		return fmt.Errorf("%w: alert_id %d names no rule of this source", ErrInvalidSilence, *silence.AlertID)
	}
	return err
}

func markActive(log *slog.Logger, silence *models.AlertSilence, now time.Time) {
	active, err := alerts.ActiveAt(silence, now)
	if err != nil {
		log.Error("alert silence cannot be evaluated", "silence_id", silence.ID, "error", err)
	}
	silence.Active = active
}

// applySilenceRequest validates req and copies it onto silence.
func applySilenceRequest(silence *models.AlertSilence, req *models.AlertSilenceRequest, now time.Time) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidSilence, fmt.Sprintf(format, args...))
	}
	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		return invalid("comment is required: say why the alerts are silenced")
	}
	if utf8.RuneCountInString(comment) > models.MaxSilenceCommentChars {
		return invalid("comment takes at most %d characters", models.MaxSilenceCommentChars)
	}
	if req.AlertID != nil && *req.AlertID <= 0 {
		return invalid("alert_id must be a positive id or absent")
	}
	matchers, err := normalizeMatchers(req.Matchers)
	if err != nil {
		return invalid("%s", err)
	}
	if req.EndsAt != nil && !req.EndsAt.After(now) {
		return invalid("ends_at is in the past")
	}

	silence.AlertID = req.AlertID
	silence.Matchers = matchers
	silence.Comment = comment
	silence.ScheduleKind = req.ScheduleKind
	silence.StartsAt = utcPtr(req.StartsAt)
	silence.EndsAt = utcPtr(req.EndsAt)
	silence.Weekly = nil

	switch req.ScheduleKind {
	case models.SilenceOnce:
		if req.StartsAt == nil || req.EndsAt == nil {
			return invalid("starts_at and ends_at are required for a once silence")
		}
		if !req.EndsAt.After(*req.StartsAt) {
			return invalid("ends_at must be after starts_at")
		}
		if req.EndsAt.Sub(*req.StartsAt) > models.MaxSilenceOnceDuration {
			return invalid("a once silence lasts at most 30 days; use a weekly silence for a recurring window")
		}
		if req.Weekly != nil {
			return invalid("weekly applies to weekly silences only")
		}
	case models.SilenceWeekly:
		if req.StartsAt != nil {
			return invalid("starts_at applies to once silences only; a weekly silence starts with its first window")
		}
		weekly, err := normalizeWeekly(req.Weekly)
		if err != nil {
			return invalid("%s", err)
		}
		silence.Weekly = weekly
	default:
		return invalid("schedule_kind must be once or weekly")
	}
	return nil
}

func normalizeMatchers(in map[string]string) (map[string]string, error) {
	if len(in) > models.MaxSilenceMatchers {
		return nil, fmt.Errorf("matchers takes at most %d labels", models.MaxSilenceMatchers)
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("matchers has a blank label name")
		}
		if utf8.RuneCountInString(key) > models.MaxSilenceMatcherChars || utf8.RuneCountInString(value) > models.MaxSilenceMatcherChars {
			return nil, fmt.Errorf("matcher %q is longer than %d characters", key, models.MaxSilenceMatcherChars)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("matchers names label %q twice", key)
		}
		out[key] = value
	}
	return out, nil
}

func normalizeWeekly(in *models.SilenceWeeklyWindow) (*models.SilenceWeeklyWindow, error) {
	if in == nil {
		return nil, fmt.Errorf("weekly is required for a weekly silence")
	}
	if len(in.Weekdays) == 0 {
		return nil, fmt.Errorf("weekly.weekdays needs at least one day, 0 (Sunday) to 6 (Saturday)")
	}
	days := slices.Clone(in.Weekdays)
	slices.Sort(days)
	for i, day := range days {
		if day < 0 || day > 6 {
			return nil, fmt.Errorf("weekly.weekdays has %d; days run from 0 (Sunday) to 6 (Saturday)", day)
		}
		if i > 0 && days[i-1] == day {
			return nil, fmt.Errorf("weekly.weekdays names day %d twice", day)
		}
	}
	if _, _, err := alerts.ParseClock(in.Start); err != nil {
		return nil, fmt.Errorf("weekly.%w", err)
	}
	if in.DurationMinutes <= 0 || in.DurationMinutes > models.MaxSilenceWindowMinutes {
		return nil, fmt.Errorf("weekly.duration_minutes must be between 1 and %d", models.MaxSilenceWindowMinutes)
	}
	if strings.TrimSpace(in.Timezone) == "" {
		return nil, fmt.Errorf("weekly.timezone is required, for example UTC or Asia/Kolkata")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, fmt.Errorf("weekly.timezone %q is not an IANA time zone", in.Timezone)
	}
	return &models.SilenceWeeklyWindow{Weekdays: days, Start: in.Start, DurationMinutes: in.DurationMinutes, Timezone: in.Timezone}, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
