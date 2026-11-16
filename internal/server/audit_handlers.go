package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	auditListDefaultLimit = 100
	auditListMaxLimit     = 500
	auditDefaultWindow    = 7 * 24 * time.Hour
	auditExportMaxRows    = 100_000
	auditExportPageSize   = 1000
	auditExportPageWait   = 30 * time.Second
)

var auditAuthMethods = []models.AuditAuthMethod{
	models.AuditAuthSession, models.AuditAuthAPIToken, models.AuditAuthOAuthAPI,
	models.AuditAuthOAuthMCP, models.AuditAuthAssistant, models.AuditAuthProvisioning,
	models.AuditAuthStartup, models.AuditAuthScheduler, models.AuditAuthNone,
}

type auditEventsResponse struct {
	Events     []models.AuditEvent `json:"events"`
	NextCursor string              `json:"next_cursor"`
}

// handleListAuditEvents returns one page of audit events.
// URL: GET /api/v1/admin/audit-events
// Requires: admin (requireAuth + requireAdmin) and audit:read token scope.
// @Summary List audit events
// @Description Returns one page of audit events, newest first unless order is asc. Pass next_cursor as cursor for the next page.
// @Tags admin
// @Produce json
// @Param from query string false "RFC 3339 start, inclusive. Default: 7 days before to"
// @Param to query string false "RFC 3339 end, exclusive"
// @Param action query string false "Exact action, or a resource with a trailing dot such as source."
// @Param actor_id query int false "Actor user ID"
// @Param auth_method query string false "Auth method"
// @Param credential_id query string false "Credential ID"
// @Param target_type query string false "Target type"
// @Param target_id query string false "Target ID, needs target_type"
// @Param team_id query int false "Team ID"
// @Param source_id query int false "Source ID"
// @Param outcome query string false "success or failure"
// @Param order query string false "desc (default) or asc"
// @Param limit query int false "Page size, default 100, maximum 500"
// @Param cursor query string false "next_cursor of the previous page"
// @Success 200 {object} auditEventsResponse
// @Router /admin/audit-events [get]
func (s *Server) handleListAuditEvents(c fiber.Ctx) error {
	f, err := parseAuditFilter(c, time.Now())
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	f.Limit, err = parseAuditLimit(c.Query("limit"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	if raw := c.Query("cursor"); raw != "" {
		if f.Cursor, err = decodeAuditCursor(raw); err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
	}

	// One extra row says whether another page exists.
	pageSize := f.Limit
	f.Limit++
	events, err := s.store.ListAuditEvents(c.Context(), f)
	if err != nil {
		s.log.Error("failed to list audit events", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list audit events", models.DatabaseErrorType)
	}
	resp := auditEventsResponse{Events: events}
	if len(events) > pageSize {
		resp.Events = events[:pageSize]
		resp.NextCursor = encodeAuditCursor(resp.Events[pageSize-1])
	}
	return SendSuccess(c, fiber.StatusOK, resp)
}

// handleExportAuditEvents streams every audit event that matches the filters
// as NDJSON, and records audit_event.export.
// URL: GET /api/v1/admin/audit-events/export
// Requires: admin (requireAuth + requireAdmin) and audit:read token scope.
// @Summary Export audit events
// @Description Streams the matching audit events as NDJSON, one event per line. Takes the list filters without limit and cursor. More than 100,000 matching events is a 400.
// @Tags admin
// @Produce application/x-ndjson
// @Param from query string false "RFC 3339 start, inclusive. Default: 7 days before to"
// @Param to query string false "RFC 3339 end, exclusive. Default: now"
// @Param action query string false "Exact action, or a resource with a trailing dot such as source."
// @Param actor_id query int false "Actor user ID"
// @Param auth_method query string false "Auth method"
// @Param credential_id query string false "Credential ID"
// @Param target_type query string false "Target type"
// @Param target_id query string false "Target ID, needs target_type"
// @Param team_id query int false "Team ID"
// @Param source_id query int false "Source ID"
// @Param outcome query string false "success or failure"
// @Param order query string false "desc (default) or asc"
// @Success 200 {string} string "NDJSON audit events"
// @Router /admin/audit-events/export [get]
func (s *Server) handleExportAuditEvents(c fiber.Ctx) error {
	now := time.Now()
	f, err := parseAuditFilter(c, now)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	// End at now at the latest, so that the count and the stream read the
	// same events and the stream never holds this export's own event.
	if f.To.IsZero() || f.To.After(now) {
		f.To = now
	}

	ctx := c.Context()
	rows, err := s.store.CountAuditEvents(ctx, f, auditExportMaxRows+1)
	if err != nil {
		s.log.Error("failed to count audit events for export", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to count audit events", models.DatabaseErrorType)
	}
	if rows > auditExportMaxRows {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("More than %d audit events match. Narrow the time range.", auditExportMaxRows),
			models.ValidationErrorType)
	}

	// Read the first page before the status is sent, so that a store failure
	// is a 500 and not a truncated file.
	f.Limit = auditExportPageSize
	first, err := s.store.ListAuditEvents(ctx, f)
	if err != nil {
		s.log.Error("failed to list audit events for export", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list audit events", models.DatabaseErrorType)
	}

	auditTarget(c, audit.Target{Type: "audit_event"})
	auditExportDetail(c, f, rows)

	c.Status(fiber.StatusOK)
	c.Set(fiber.HeaderContentType, "application/x-ndjson; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition,
		fmt.Sprintf("attachment; filename=%q", "logchef-audit-"+now.UTC().Format("20060102-150405")+".ndjson"))
	// The stream writer runs after the handler returns, so it must not touch c.
	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
		if err := s.writeAuditExport(ctx, w, f, first); err != nil {
			s.log.Error("audit export stopped", "error", err)
			line, _ := json.Marshal(map[string]string{"type": "error", "error": "Failed to read audit events"})
			_, _ = w.Write(append(line, '\n'))
		}
		_ = w.Flush()
	})
	return nil
}

// writeAuditExport writes page and every later page of f, one event per line,
// and stops at auditExportMaxRows even if events arrived after the count.
func (s *Server) writeAuditExport(ctx context.Context, w *bufio.Writer, f models.AuditFilter, page []models.AuditEvent) error {
	enc := json.NewEncoder(w)
	written := 0
	for {
		page = page[:min(len(page), auditExportMaxRows-written)]
		for i := range page {
			if err := enc.Encode(&page[i]); err != nil {
				return err
			}
		}
		written += len(page)
		if len(page) < f.Limit || written == auditExportMaxRows {
			return nil
		}
		if err := w.Flush(); err != nil {
			return err
		}
		last := page[len(page)-1]
		f.Cursor = &models.AuditCursor{OccurredAt: last.OccurredAt, ID: last.ID}
		pageCtx, cancel := context.WithTimeout(ctx, auditExportPageWait)
		var err error
		page, err = s.store.ListAuditEvents(pageCtx, f)
		cancel()
		if err != nil {
			return err
		}
	}
}

// auditExportDetail stages the filters and the row count of an export. The
// credential filter goes in as cred_id: it is the ID that every event stores
// in the clear, and the serializer redacts any key that says "credential".
func auditExportDetail(c fiber.Ctx, f models.AuditFilter, rows int) {
	auditDetail(c, "from", f.From)
	auditDetail(c, "to", f.To)
	auditDetail(c, "rows", rows)
	for key, value := range map[string]string{
		"action":      f.Action + f.ActionPrefix,
		"auth_method": string(f.AuthMethod),
		"cred_id":     f.CredentialID,
		"target_type": f.TargetType,
		"target_id":   f.TargetID,
		"outcome":     string(f.Outcome),
	} {
		if value != "" {
			auditDetail(c, key, value)
		}
	}
	if f.ActorID != nil {
		auditDetail(c, "actor_id", *f.ActorID)
	}
	if f.TeamID != nil {
		auditDetail(c, "team_id", *f.TeamID)
	}
	if f.SourceID != nil {
		auditDetail(c, "source_id", *f.SourceID)
	}
}

// parseAuditFilter reads the filters that the list and the export share.
// Without from, the range starts seven days before to, or before now.
func parseAuditFilter(c fiber.Ctx, now time.Time) (models.AuditFilter, error) {
	var f models.AuditFilter
	var err error
	if f.From, f.To, err = parseAuditRange(c, now); err != nil {
		return f, err
	}
	if f.Action, f.ActionPrefix, err = parseAuditAction(c.Query("action")); err != nil {
		return f, err
	}
	if f.ActorID, err = parseAuditID[models.UserID](c, "actor_id"); err != nil {
		return f, err
	}
	if f.TeamID, err = parseAuditID[models.TeamID](c, "team_id"); err != nil {
		return f, err
	}
	if f.SourceID, err = parseAuditID[models.SourceID](c, "source_id"); err != nil {
		return f, err
	}

	if raw := c.Query("auth_method"); raw != "" {
		f.AuthMethod = models.AuditAuthMethod(raw)
		if !slices.Contains(auditAuthMethods, f.AuthMethod) {
			return f, fmt.Errorf("unknown auth_method %q", raw)
		}
	}
	f.CredentialID = c.Query("credential_id")
	f.TargetType, f.TargetID = c.Query("target_type"), c.Query("target_id")
	if f.TargetID != "" && f.TargetType == "" {
		return f, errors.New("target_id needs target_type")
	}

	switch outcome := models.AuditOutcome(c.Query("outcome")); outcome {
	case "", models.AuditOutcomeSuccess, models.AuditOutcomeFailure:
		f.Outcome = outcome
	default:
		return f, errors.New("outcome must be success or failure")
	}
	switch c.Query("order") {
	case "", "desc":
	case "asc":
		f.Ascending = true
	default:
		return f, errors.New("order must be asc or desc")
	}
	return f, nil
}

func parseAuditRange(c fiber.Ctx, now time.Time) (from, to time.Time, err error) {
	if from, err = parseAuditTime(c, "from"); err != nil {
		return from, to, err
	}
	if to, err = parseAuditTime(c, "to"); err != nil {
		return from, to, err
	}
	if from.IsZero() {
		end := now
		if !to.IsZero() {
			end = to
		}
		from = end.Add(-auditDefaultWindow)
	}
	if !to.IsZero() && !from.Before(to) {
		return from, to, errors.New("from must be before to")
	}
	return from, to, nil
}

// parseAuditAction reads an exact action, or a resource with a trailing dot
// as a prefix. Both must name registered actions.
func parseAuditAction(raw string) (action, prefix string, err error) {
	if raw == "" {
		return "", "", nil
	}
	if resource, ok := strings.CutSuffix(raw, "."); ok {
		if !audit.HasResource(resource) {
			return "", "", fmt.Errorf("unknown audit resource %q", resource)
		}
		return "", raw, nil
	}
	if _, ok := audit.Lookup(raw); !ok {
		return "", "", fmt.Errorf("unknown audit action %q", raw)
	}
	return raw, "", nil
}

func parseAuditTime(c fiber.Ctx, key string) (time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC 3339 time", key)
	}
	return t, nil
}

func parseAuditID[T ~int](c fiber.Ctx, key string) (*T, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", key)
	}
	id := T(n)
	return &id, nil
}

// parseAuditLimit returns the page size. A limit above the maximum is
// clamped to it.
func parseAuditLimit(raw string) (int, error) {
	if raw == "" {
		return auditListDefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New("limit must be a positive integer")
	}
	return min(n, auditListMaxLimit), nil
}

// The cursor is opaque to clients: the last event's time in Unix
// milliseconds and its ID. A UUID holds no '.', so the first '.' separates
// them.
func encodeAuditCursor(e models.AuditEvent) string {
	raw := strconv.FormatInt(e.OccurredAt.UnixMilli(), 10) + "." + e.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeAuditCursor(s string) (*models.AuditCursor, error) {
	errBad := errors.New("cursor is not a next_cursor from this API")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errBad
	}
	millis, id, ok := strings.Cut(string(raw), ".")
	if !ok || id == "" {
		return nil, errBad
	}
	ms, err := strconv.ParseInt(millis, 10, 64)
	if err != nil {
		return nil, errBad
	}
	return &models.AuditCursor{OccurredAt: time.UnixMilli(ms).UTC(), ID: id}, nil
}
