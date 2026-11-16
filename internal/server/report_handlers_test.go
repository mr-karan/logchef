package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

type reportRouteEnv struct {
	t     *testing.T
	s     *Server
	db    *sqlite.DB
	owner *models.User
	admin *models.User
	other *models.User
	sq    *models.SavedQuery
}

const reportTestSecret = "0123456789abcdef0123456789abcdef"

func newReportRouteEnv(t *testing.T, enabled bool) *reportRouteEnv {
	t.Helper()
	db := newServerTestDB(t)
	cfg := &config.Config{
		Auth:    config.AuthConfig{SessionDuration: time.Hour, MaxConcurrentSessions: 5, APITokenSecret: reportTestSecret},
		Reports: config.ReportsConfig{Enabled: enabled, QueryBudget: 5 * time.Minute, MaxConcurrentRuns: 2},
		Query:   config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, MaxResponseBytes: 1 << 20, DefaultTimeoutSeconds: 10, MaxTimeoutSeconds: 60},
	}
	e := &reportRouteEnv{t: t, db: db, s: newServerForTest(t, cfg, db, nil)}
	e.owner = mkTestUser(t, db, "owner@example.com", models.UserRoleMember)
	e.admin = mkTestUser(t, db, "admin@example.com", models.UserRoleAdmin)
	e.other = mkTestUser(t, db, "other@example.com", models.UserRoleMember)
	_, src := mkTestTeam(t, db, "payments", e.owner)
	content := fmt.Sprintf(`{"version":1,"sourceId":%d,"limit":50,"content":"SELECT status FROM logs WHERE host = {{host}}","variables":[{"name":"host","type":"text","label":"Host","inputType":"input","value":""}]}`, src.ID)
	sq, err := db.CreateSavedQuery(context.Background(), src.ID, nil, "5xx", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, content, &e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.sq = sq
	return e
}

func (e *reportRouteEnv) body(name string) string {
	return fmt.Sprintf(`{"name":%q,"source":{"kind":"saved_query","saved_query_id":%d},"schedule":{"frequency":"daily","hour":9,"minute":0},"timezone":"Asia/Kolkata","time_range":"24h","variables":{"host":"secret-host-literal"}}`, name, e.sq.ID)
}

type reportResp struct {
	status int
	header http.Header
	body   string
}

// do sends a request as user, with a session, or with token when set.
func (e *reportRouteEnv) do(user *models.User, token, method, path, body string) reportResp {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.AddCookie(sessionCookie(e.t, e.db, user))
	}
	resp, err := e.s.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return reportResp{status: resp.StatusCode, header: resp.Header, body: string(data)}
}

func (e *reportRouteEnv) expect(user *models.User, method, path, body string, want int) reportResp {
	e.t.Helper()
	got := e.do(user, "", method, path, body)
	if got.status != want {
		e.t.Fatalf("%s %s = %d %s, want %d", method, path, got.status, got.body, want)
	}
	return got
}

func (e *reportRouteEnv) create(name string) int {
	e.t.Helper()
	got := e.expect(e.owner, http.MethodPost, "/api/v1/reports", e.body(name), http.StatusCreated)
	var envelope struct {
		Data struct {
			ID       int  `json:"id"`
			CanEdit  bool `json:"can_edit"`
			Warnings []struct {
				Code string `json:"code"`
			} `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.body), &envelope); err != nil {
		e.t.Fatal(err)
	}
	if !envelope.Data.CanEdit || envelope.Data.ID == 0 {
		e.t.Fatalf("created report = %s", got.body)
	}
	return envelope.Data.ID
}

func TestReportRoutesErrorTable(t *testing.T) {
	e := newReportRouteEnv(t, true)
	id := e.create("Daily 5xx")
	path := fmt.Sprintf("/api/v1/reports/%d", id)

	t.Run("400 names the field", func(t *testing.T) {
		got := e.expect(e.owner, http.MethodPost, "/api/v1/reports", strings.Replace(e.body("x"), `"name":"x"`, `"name":""`, 1), http.StatusBadRequest)
		if !strings.Contains(got.body, "name must be") {
			t.Errorf("body = %s", got.body)
		}
		e.expect(e.owner, http.MethodPost, "/api/v1/reports", strings.Replace(e.body("x"), `"variables"`, `"recipients":[{"kind":"email","email":"oncall@example.com"}],"variables"`, 1), http.StatusBadRequest)
	})
	t.Run("403 when the owner cannot view the source", func(t *testing.T) {
		e.expect(e.other, http.MethodPost, "/api/v1/reports", e.body("theirs"), http.StatusForbidden)
	})
	t.Run("404 for a stranger and a missing report", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			e.expect(e.other, method, path, e.body("mine now"), http.StatusNotFound)
		}
		e.expect(e.other, http.MethodPost, path+"/run", "", http.StatusNotFound)
		e.expect(e.other, http.MethodGet, path+"/runs", "", http.StatusNotFound)
		e.expect(e.owner, http.MethodGet, "/api/v1/reports/99999", "", http.StatusNotFound)
	})
	t.Run("202 then 429", func(t *testing.T) {
		got := e.expect(e.owner, http.MethodPost, path+"/run", `{"recipients":"me"}`, http.StatusAccepted)
		if !strings.Contains(got.body, `"status":"running"`) || !strings.Contains(got.body, `"trigger":"manual"`) {
			t.Errorf("run = %s", got.body)
		}
		got = e.expect(e.owner, http.MethodPost, path+"/run", "", http.StatusTooManyRequests)
		if got.header.Get("Retry-After") != "60" {
			t.Errorf("Retry-After = %q", got.header.Get("Retry-After"))
		}
		e.expect(e.owner, http.MethodPost, path+"/run", `{"recipients":"everyone"}`, http.StatusBadRequest)
	})
	t.Run("409 while a run is in flight", func(t *testing.T) {
		busy := e.create("Busy")
		if _, err := e.db.CreateReportRun(context.Background(), models.ReportRunClaim{
			ReportID: models.ReportID(busy), Trigger: models.ReportTriggerSchedule, ScheduledFor: time.Now().UTC(), HandlerBudget: time.Hour,
		}); err != nil {
			t.Fatal(err)
		}
		e.expect(e.owner, http.MethodPost, fmt.Sprintf("/api/v1/reports/%d/run", busy), "", http.StatusConflict)
	})
	t.Run("an admin may edit and list all, a member may not list all", func(t *testing.T) {
		e.expect(e.admin, http.MethodGet, path, "", http.StatusOK)
		e.expect(e.admin, http.MethodGet, "/api/v1/reports?scope=all", "", http.StatusOK)
		e.expect(e.owner, http.MethodGet, "/api/v1/reports?scope=all", "", http.StatusForbidden)
		got := e.expect(e.owner, http.MethodGet, "/api/v1/reports", "", http.StatusOK)
		if !strings.Contains(got.body, `"name":"Daily 5xx"`) || !strings.Contains(got.body, `"last_run":{`) {
			t.Errorf("list = %s", got.body)
		}
	})
	t.Run("503 when reports are disabled", func(t *testing.T) {
		off := newReportRouteEnv(t, false)
		off.expect(off.owner, http.MethodGet, "/api/v1/reports", "", http.StatusServiceUnavailable)
		off.expect(off.owner, http.MethodPost, "/api/v1/reports", off.body("x"), http.StatusServiceUnavailable)
	})
}

func TestReportRoutesScopes(t *testing.T) {
	e := newReportRouteEnv(t, true)
	id := e.create("Daily 5xx")
	path := fmt.Sprintf("/api/v1/reports/%d", id)
	read := apiToken(t, e.s, e.owner, models.TokenScopeReportsRead)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/reports", http.StatusOK},
		{http.MethodGet, path, http.StatusOK},
		{http.MethodGet, path + "/runs", http.StatusOK},
		{http.MethodPost, "/api/v1/reports", http.StatusForbidden},
		{http.MethodPut, path, http.StatusForbidden},
		{http.MethodDelete, path, http.StatusForbidden},
		{http.MethodPost, path + "/run", http.StatusForbidden},
		{http.MethodPost, path + "/pause", http.StatusForbidden},
		{http.MethodPost, path + "/preview", http.StatusForbidden},
	} {
		if got := e.do(e.owner, read, c.method, c.path, e.body("x")); got.status != c.want {
			t.Errorf("reports:read %s %s = %d, want %d", c.method, c.path, got.status, c.want)
		}
	}
	none := apiToken(t, e.s, e.owner, models.TokenScopeLogsRead)
	if got := e.do(e.owner, none, http.MethodGet, "/api/v1/reports", ""); got.status != http.StatusForbidden {
		t.Errorf("a token without reports:read lists reports: %d", got.status)
	}
}

func TestReportRoutesAudit(t *testing.T) {
	e := newReportRouteEnv(t, true)
	ctx := context.Background()
	events := func() []models.AuditEvent {
		got, err := e.db.ListAuditEvents(ctx, models.AuditFilter{Ascending: true, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	id := e.create("Daily 5xx")
	path := fmt.Sprintf("/api/v1/reports/%d", id)
	e.expect(e.owner, http.MethodPut, path, strings.Replace(e.body("Daily 5xx"), `"time_range":"24h"`, `"time_range":"7d"`, 1), http.StatusOK)
	e.expect(e.owner, http.MethodPost, path+"/pause", "", http.StatusOK)
	e.expect(e.owner, http.MethodPost, path+"/resume", "", http.StatusOK)
	e.expect(e.owner, http.MethodPost, path+"/run", "", http.StatusAccepted)
	if err := e.db.PauseReport(ctx, models.ReportID(id), models.ReportPauseOwnerInactive); err != nil {
		t.Fatal(err)
	}
	e.expect(e.admin, http.MethodPost, path+"/claim", "", http.StatusOK)
	e.expect(e.admin, http.MethodDelete, path, "", http.StatusNoContent)

	got := events()
	want := []string{"report.create", "report.update", "report.pause", "report.resume", "report.claim", "report.delete"}
	if len(got) != len(want) {
		t.Fatalf("stored %d events, want %v: %+v", len(got), want, got)
	}
	for i, ev := range got {
		if ev.Action != want[i] || ev.TargetType != "report" || ev.TargetID != fmt.Sprint(id) || ev.Outcome != models.AuditOutcomeSuccess {
			t.Errorf("event %d = %s %s:%s %s, want %s on report %d", i, ev.Action, ev.TargetType, ev.TargetID, ev.Outcome, want[i], id)
		}
		all := string(ev.Changes) + string(ev.Detail)
		if strings.Contains(all, "secret-host-literal") || strings.Contains(all, "unstaged") {
			t.Errorf("event %s = %s", ev.Action, all)
		}
	}
	for _, c := range []struct {
		i    int
		want []string
	}{
		{0, []string{`"source_kind"`, `"saved_query_id"`, `"schedule.frequency"`, `"timezone"`, `"variable_names"`, `"host"`}},
		{2, []string{`"reason":"user"`}},
		{4, []string{fmt.Sprintf(`"previous_owner_id":%d`, e.owner.ID)}},
	} {
		all := string(got[c.i].Changes) + string(got[c.i].Detail)
		for _, w := range c.want {
			if !strings.Contains(all, w) {
				t.Errorf("%s event = %s, want %s", got[c.i].Action, all, w)
			}
		}
	}
	var changes map[string]json.RawMessage
	if err := json.Unmarshal(got[1].Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes["time_range"] == nil {
		t.Errorf("update changes = %s, want only time_range", got[1].Changes)
	}
}

// A run started over HTTP goes through the server's own wiring to
// delivery: with a datasource that answers and no SMTP settings, it ends
// failed with smtp_not_configured.
func TestReportRunReachesDelivery(t *testing.T) {
	e := newReportRouteEnv(t, true)
	ds := datasource.NewService(e.db, e.s.log)
	ds.Register(&succeedingQueryProvider{sourceType: models.SourceTypeClickHouse})
	e.s.reports.DS = ds
	id := e.create("Daily 5xx")
	e.expect(e.owner, http.MethodPost, fmt.Sprintf("/api/v1/reports/%d/run", id), "", http.StatusAccepted)

	deadline := time.Now().Add(10 * time.Second)
	for {
		runs, err := e.db.ListReportRuns(context.Background(), models.ReportID(id), 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].Status != models.ReportRunRunning {
			run := runs[0]
			if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonSMTPNotConfigured {
				t.Fatalf("run = %s/%s, want failed/smtp_not_configured; panels %+v deliveries %+v", run.Status, run.Reason, run.Panels, run.Deliveries)
			}
			if len(run.Panels) != 1 || run.Panels[0].Status != models.ReportPanelOK {
				t.Errorf("panels = %+v, want the query to have run", run.Panels)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run did not finish: %+v", runs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
