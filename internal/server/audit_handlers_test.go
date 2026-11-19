package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// seedAuditPages stores n source.update events, newest at end, in one
// transaction. Every third event shares the time of the one before it, so
// that the keyset cursor must break ties by ID.
func seedAuditPages(t *testing.T, db store.Store, n int, end time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	at := end.Add(-time.Duration(n) * time.Second)
	err := db.WithTx(context.Background(), func(tx store.StoreOps) error {
		for i := range n {
			if i%3 != 0 {
				at = at.Add(time.Second)
			}
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			e := &models.AuditEvent{
				ID: id.String(), OccurredAt: at, Action: "source.update",
				Outcome: models.AuditOutcomeSuccess, ActorType: models.AuditActorUser,
				AuthMethod: models.AuditAuthSession, TargetType: "source", TargetID: strconv.Itoa(i),
			}
			if err := tx.InsertAuditEvent(context.Background(), e); err != nil {
				return err
			}
			ids = append(ids, e.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func (e *auditRouteEnv) doBearer(path, token string) auditResp {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := e.s.app.Test(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		e.t.Fatal(err)
	}
	return auditResp{status: resp.StatusCode, header: resp.Header, body: body.Bytes()}
}

func decodeAuditPage(t *testing.T, resp auditResp) auditEventsResponse {
	t.Helper()
	var envelope struct {
		Data auditEventsResponse `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &envelope); err != nil {
		t.Fatalf("decode %s: %v", resp.body, err)
	}
	return envelope.Data
}

func TestAuditEventsRequireAdminAndScope(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	member := mkTestUser(t, e.db, "member@example.com", models.UserRoleMember)
	seedAuditPages(t, e.db, 3, time.Now())

	for _, path := range []string{"/api/v1/admin/audit-events", "/api/v1/admin/audit-events/export"} {
		wantStatus(t, e.do(http.MethodGet, path, "", sessionCookie(t, e.db, member)), http.StatusForbidden)
		wantStatus(t, e.doBearer(path, apiToken(t, e.s, e.admin, core.ReadOnlyTokenScopes()...)), http.StatusForbidden)
		wantStatus(t, e.doBearer(path, apiToken(t, e.s, member, models.TokenScopeAuditRead)), http.StatusForbidden)
		wantStatus(t, e.doBearer(path, apiToken(t, e.s, e.admin, models.TokenScopeAuditRead)), http.StatusOK)
		wantStatus(t, e.doBearer(path, apiToken(t, e.s, e.admin, models.TokenScopeAll)), http.StatusOK)
		wantStatus(t, e.do(http.MethodGet, path, "", e.cookie), http.StatusOK)
	}
}

func TestAuditEventsCursorWalkReturnsEveryRowOnce(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	seeded := seedAuditPages(t, e.db, 250, time.Now())

	for _, order := range []string{"desc", "asc"} {
		var got []string
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > 10 {
				t.Fatalf("%s: the cursor walk did not end", order)
			}
			q := url.Values{"order": {order}, "limit": {"40"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			resp := e.do(http.MethodGet, "/api/v1/admin/audit-events?"+q.Encode(), "", e.cookie)
			wantStatus(t, resp, http.StatusOK)
			page := decodeAuditPage(t, resp)
			got = append(got, auditPageIDs(page)...)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		want := slices.Clone(seeded)
		if order == "desc" {
			slices.Reverse(want)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s walk returned %d ids, want the %d seeded ids once each in order", order, len(got), len(want))
		}
	}

	// The list writes no event of its own.
	if n := len(e.events()); n != len(seeded) {
		t.Errorf("stored %d events after listing, want %d", n, len(seeded))
	}
}

func TestAuditEventsFilterAndDefaultWindow(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	now := time.Now()
	old := seedAuditPages(t, e.db, 2, now.Add(-10*24*time.Hour))
	recent := seedAuditPages(t, e.db, 2, now)

	page := decodeAuditPage(t, e.do(http.MethodGet, "/api/v1/admin/audit-events?order=asc", "", e.cookie))
	if got := auditPageIDs(page); !slices.Equal(got, recent) {
		t.Errorf("default window returned %v, want only the recent events %v", got, recent)
	}

	from := now.Add(-11 * 24 * time.Hour).UTC().Format(time.RFC3339)
	page = decodeAuditPage(t, e.do(http.MethodGet, "/api/v1/admin/audit-events?order=asc&action=source.&from="+from, "", e.cookie))
	if got := auditPageIDs(page); !slices.Equal(got, append(slices.Clone(old), recent...)) {
		t.Errorf("from and the source. prefix returned %v", got)
	}
	page = decodeAuditPage(t, e.do(http.MethodGet, "/api/v1/admin/audit-events?action=user.update&from="+from, "", e.cookie))
	if len(page.Events) != 0 {
		t.Errorf("user.update matched %d source events", len(page.Events))
	}
	page = decodeAuditPage(t, e.do(http.MethodGet, "/api/v1/admin/audit-events?target_type=source&target_id=1", "", e.cookie))
	if got := auditPageIDs(page); !slices.Equal(got, recent[1:]) {
		t.Errorf("target filter returned %v, want %v", got, recent[1:])
	}
}

func auditPageIDs(page auditEventsResponse) []string {
	ids := make([]string, 0, len(page.Events))
	for i := range page.Events {
		ids = append(ids, page.Events[i].ID)
	}
	return ids
}

func TestAuditEventsRejectBadParameters(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	for _, query := range []string{
		"from=yesterday",
		"from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z",
		"action=source.explode",
		"action=nothing.",
		"actor_id=abc",
		"team_id=0",
		"auth_method=password",
		"target_id=7",
		"outcome=maybe",
		"order=sideways",
		"limit=0",
		"limit=ten",
		"cursor=%25%25",
		"cursor=bm8tc2VwYXJhdG9y",
	} {
		resp := e.do(http.MethodGet, "/api/v1/admin/audit-events?"+query, "", e.cookie)
		if resp.status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400: %s", query, resp.status, resp.body)
		}
	}
	page := decodeAuditPage(t, e.do(http.MethodGet, "/api/v1/admin/audit-events?limit=100000", "", e.cookie))
	if page.Events == nil {
		t.Error("a limit above the maximum was not clamped")
	}
}

func TestAuditEventsExportStreamsAndRecords(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	seeded := seedAuditPages(t, e.db, auditExportPageSize+5, time.Now().Add(-time.Minute))

	// A future to and no action filter: the export's own event must not be
	// in the file.
	to := url.QueryEscape(time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339))
	resp := e.do(http.MethodGet, "/api/v1/admin/audit-events/export?order=asc&outcome=success&to="+to, "", e.cookie)
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.header.Get("Content-Type"); ct != "application/x-ndjson; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	var got []string
	sc := bufio.NewScanner(bytes.NewReader(resp.body))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var ev models.AuditEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line %q: %v", sc.Bytes(), err)
		}
		got = append(got, ev.ID)
	}
	if !slices.Equal(got, seeded) {
		t.Fatalf("export returned %d events, want the %d seeded events in order", len(got), len(seeded))
	}

	events, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Action: "audit_event.export", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("stored %d audit_event.export events, want 1", len(events))
	}
	ev := events[0]
	checkActorAndTarget(t, ev, "audit_event.export", e.admin, "audit_event", "")
	detail := detailOf(t, ev)
	if detail["rows"] != float64(len(seeded)) || detail["outcome"] != "success" || detail["from"] == nil || detail["to"] == nil {
		t.Errorf("export detail = %v", detail)
	}
}

func TestAuditEventsExportRejectsTooManyRows(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	seedAuditPages(t, e.db, auditExportMaxRows+1, time.Now().Add(-time.Minute))

	resp := e.do(http.MethodGet, "/api/v1/admin/audit-events/export", "", e.cookie)
	wantStatus(t, resp, http.StatusBadRequest)
	if !bytes.Contains(resp.body, []byte("Narrow the time range")) {
		t.Errorf("body %s does not ask for a narrower range", resp.body)
	}
	events, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Action: "audit_event.export", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("a refused export recorded an event")
	}
}
