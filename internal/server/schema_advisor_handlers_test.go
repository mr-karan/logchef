package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// fakeSchemaAdvisor is a ClickHouse provider whose analysis the test
// controls. LoadSource needs only Type and Describe.
type fakeSchemaAdvisor struct {
	datasource.Provider
	advise func(ctx context.Context, source *models.Source, usage advisor.Usage) (*models.SchemaAdvisorReport, error)
}

func (p *fakeSchemaAdvisor) Type() models.SourceType         { return models.SourceTypeClickHouse }
func (p *fakeSchemaAdvisor) Describe() datasource.Descriptor { return datasource.Descriptor{} }
func (p *fakeSchemaAdvisor) AdviseSchema(ctx context.Context, source *models.Source, usage advisor.Usage) (*models.SchemaAdvisorReport, error) {
	return p.advise(ctx, source, usage)
}

type schemaAdviceBody struct {
	Message   string `json:"message"`
	ErrorType string `json:"error_type"`
	Data      struct {
		SourceID    models.SourceID             `json:"source_id"`
		GeneratedAt *time.Time                  `json:"generated_at"`
		GeneratedBy *models.UserID              `json:"generated_by"`
		WindowDays  *int                        `json:"window_days"`
		Stale       bool                        `json:"stale"`
		Report      *models.SchemaAdvisorReport `json:"report"`
	} `json:"data"`
}

func decodeSchemaAdvice(t *testing.T, resp *testResponse) schemaAdviceBody {
	t.Helper()
	var body schemaAdviceBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func advisorPath(id models.SourceID, suffix string) string {
	return "/api/v1/admin/sources/" + itoa(id) + "/advisor" + suffix
}

// TestSchemaAdviceErrors covers each row of the error table of RFC 0010
// with a real ClickHouse provider, except 429, which needs runs that block.
func TestSchemaAdviceErrors(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	ctx := context.Background()
	// No connection was opened for this source, so ClickHouse cannot answer.
	_, chSource := mkTestTeam(t, e.db, "advisor", e.user)
	_, vlSource := e.vlSourceFor("http://127.0.0.1:1")

	get := func(id models.SourceID, session string) *testResponse {
		return e.consentRequest(http.MethodGet, advisorPath(id, ""), session, "", nil)
	}
	refresh := func(id models.SourceID, session, body string) *testResponse {
		return e.consentRequest(http.MethodPost, advisorPath(id, "/refresh"), session, body, sameOriginJSON)
	}
	expect := func(name string, resp *testResponse, status int, errorType models.ErrorType) schemaAdviceBody {
		t.Helper()
		body := decodeSchemaAdvice(t, resp)
		if resp.StatusCode != status || (errorType != "" && body.ErrorType != string(errorType)) {
			t.Fatalf("%s: status %d %q (%s), want %d %q", name, resp.StatusCode, body.ErrorType, body.Message, status, errorType)
		}
		return body
	}

	// Before the first run, GET answers with nulls.
	body := expect("GET before a run", get(chSource.ID, e.session), http.StatusOK, "")
	if body.Data.SourceID != chSource.ID || body.Data.GeneratedAt != nil || body.Data.Report != nil || body.Data.WindowDays != nil {
		t.Fatalf("GET before a run = %+v, want only the source id", body.Data)
	}

	expect("GET unknown source", get(chSource.ID+1000, e.session), http.StatusNotFound, models.NotFoundErrorType)
	expect("refresh unknown source", refresh(chSource.ID+1000, e.session, ""), http.StatusNotFound, models.NotFoundErrorType)

	for name, resp := range map[string]*testResponse{
		"GET VictoriaLogs":     get(vlSource.ID, e.session),
		"refresh VictoriaLogs": refresh(vlSource.ID, e.session, ""),
	} {
		if body := expect(name, resp, http.StatusBadRequest, models.ValidationErrorType); body.Message != "Schema advice is available for ClickHouse sources only" {
			t.Errorf("%s message = %q", name, body.Message)
		}
	}

	for _, reqBody := range []string{`{"window_days": 6}`, `{"window_days": 91}`, `{"window_days": 0}`, `{"window_days": "30"}`, `{`} {
		expect("refresh "+reqBody, refresh(chSource.ID, e.session, reqBody), http.StatusBadRequest, models.ValidationErrorType)
	}

	// A failed run answers 502 and keeps the stored report.
	stored := &models.StoredSchemaAdvisorReport{
		SourceID: chSource.ID, GeneratedAt: time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Second), WindowDays: 30,
		Report: models.SchemaAdvisorReport{Status: models.AdvisorStatusOK, ClickHouseVersion: "26.8.1", Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{}},
	}
	if err := e.db.UpsertSchemaAdvisorReport(ctx, stored); err != nil {
		t.Fatal(err)
	}
	expect("refresh unreachable", refresh(chSource.ID, e.session, `{"window_days": 7}`), http.StatusBadGateway, models.ExternalServiceErrorType)
	body = expect("GET after a failed run", get(chSource.ID, e.session), http.StatusOK, "")
	if body.Data.GeneratedAt == nil || !body.Data.GeneratedAt.Equal(stored.GeneratedAt) || *body.Data.WindowDays != 30 || !body.Data.Stale || body.Data.GeneratedBy != nil {
		t.Fatalf("GET after a failed run = %+v, want the stale stored report", body.Data)
	}

	// Not a global admin, or a token without sources:read.
	_, memberSession := e.newSessionUser("advisor-member@example.com")
	expect("member GET", get(chSource.ID, memberSession), http.StatusForbidden, "")
	expect("member refresh", refresh(chSource.ID, memberSession, ""), http.StatusForbidden, "")
	pat, err := core.CreateAPIToken(ctx, e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "logs-only", nil, []models.TokenScope{models.TokenScopeLogsRead})
	if err != nil {
		t.Fatal(err)
	}
	expect("token GET without sources:read", e.apiGet(advisorPath(chSource.ID, ""), pat.Token), http.StatusForbidden, "")
	req := httptest.NewRequest(http.MethodPost, testIssuer+advisorPath(chSource.ID, "/refresh"), http.NoBody)
	req.Header.Set("Authorization", "Bearer "+pat.Token)
	expect("token refresh without sources:read", e.do(req), http.StatusForbidden, "")
}

// TestSchemaAdviceRefresh runs analyses through a provider the test
// controls: a run stores its report with its author and window, and a third
// concurrent run answers 429. A second refresh of a running source shares
// that run instead of taking a third slot.
func TestSchemaAdviceRefresh(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	ctx := context.Background()
	_, srcA := mkTestTeam(t, e.db, "advisora", e.user)
	_, srcB := mkTestTeam(t, e.db, "advisorb", e.user)
	_, srcC := mkTestTeam(t, e.db, "advisorc", e.user)

	today := time.Now().UTC().Format(time.DateOnly)
	shape := models.FilterShape{{Column: "request_id", Kind: models.MatchEq}}
	for _, inc := range []models.QueryShapeUsage{
		{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "aaaaaaaaaaaaaaaa", Shape: shape, QueryCount: 40, TotalDurationMs: 4000},
		{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: models.ShapeHashUnanalyzed, Shape: models.FilterShape{}, QueryCount: 3},
	} {
		if err := e.db.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: today, SourceID: srcA.ID, Usage: inc}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	usages := map[models.SourceID]advisor.Usage{}
	started := make(chan models.SourceID, 3)
	release := make(chan struct{})
	block := false
	e.srv.datasources.Register(&fakeSchemaAdvisor{advise: func(ctx context.Context, source *models.Source, usage advisor.Usage) (*models.SchemaAdvisorReport, error) {
		mu.Lock()
		usages[source.ID] = usage
		wait := block
		mu.Unlock()
		if wait {
			started <- source.ID
			<-release
		}
		return &models.SchemaAdvisorReport{Status: models.AdvisorStatusPartial, ClickHouseVersion: "26.8.1", Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{}}, nil
	}})

	refresh := func(id models.SourceID, body string) *testResponse {
		return e.consentRequest(http.MethodPost, advisorPath(id, "/refresh"), e.session, body, sameOriginJSON)
	}
	resp := refresh(srcA.ID, `{"window_days": 14}`)
	body := decodeSchemaAdvice(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status %d: %s", resp.StatusCode, body.Message)
	}
	if body.Data.GeneratedBy == nil || *body.Data.GeneratedBy != e.user.ID || *body.Data.WindowDays != 14 || body.Data.Stale ||
		body.Data.Report == nil || body.Data.Report.Status != models.AdvisorStatusPartial {
		t.Fatalf("refresh = %+v, want the new report by the admin over 14 days", body.Data)
	}
	got := usages[srcA.ID]
	if got.Queries != 43 || got.Unanalyzed != 3 || got.Truncated || len(got.Shapes) != 1 || got.Shapes[0].Queries != 40 || got.Shapes[0].DurationMs != 4000 {
		t.Fatalf("usage = %+v, want the seeded rollup", got)
	}
	resp = e.consentRequest(http.MethodGet, advisorPath(srcA.ID, ""), e.session, "", nil)
	if stored := decodeSchemaAdvice(t, resp); stored.Data.GeneratedAt == nil || !stored.Data.GeneratedAt.Equal(*body.Data.GeneratedAt) || *stored.Data.WindowDays != 14 {
		t.Fatalf("GET after refresh = %+v, want the stored refresh", stored.Data)
	}

	// Two runs block; a third source gets 429, a second refresh of a
	// running source waits for that run.
	mu.Lock()
	block = true
	mu.Unlock()
	statuses := make(chan int, 3)
	send := func(id models.SourceID) {
		req := httptest.NewRequest(http.MethodPost, testIssuer+advisorPath(id, "/refresh"), http.NoBody)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
		for k, v := range sameOriginJSON {
			req.Header.Set(k, v)
		}
		r, err := e.srv.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			statuses <- 0
			return
		}
		_ = r.Body.Close()
		statuses <- r.StatusCode
	}
	go send(srcA.ID)
	go send(srcB.ID)
	for range 2 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("two runs did not start")
		}
	}
	go send(srcA.ID) // joins the running analysis of source A
	resp = refresh(srcC.ID, "")
	if body := decodeSchemaAdvice(t, resp); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third run status %d (%s), want 429", resp.StatusCode, body.Message)
	}
	close(release)
	for range 3 {
		if status := <-statuses; status != http.StatusOK {
			t.Errorf("blocked refresh status %d, want 200", status)
		}
	}
	if refused := decodeSchemaAdvice(t, e.consentRequest(http.MethodGet, advisorPath(srcC.ID, ""), e.session, "", nil)); refused.Data.Report != nil {
		t.Error("the refused run of source C stored a report")
	}
}

// A refresh that joins a running analysis of its source gets that run's
// report with the run's window and author, never its own labels on another
// run's findings.
func TestSchemaAdviceJoinedRefreshKeepsRunLabels(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	other, otherSession := e.newSessionUserWithRole("advisor-admin-b@example.com", models.UserRoleAdmin)
	_, src := mkTestTeam(t, e.db, "advisorjoin", e.user)

	var mu sync.Mutex
	runs := 0
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	e.srv.datasources.Register(&fakeSchemaAdvisor{advise: func(context.Context, *models.Source, advisor.Usage) (*models.SchemaAdvisorReport, error) {
		mu.Lock()
		runs++
		run := runs
		mu.Unlock()
		started <- struct{}{}
		<-release
		return &models.SchemaAdvisorReport{Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{}, Notes: []string{fmt.Sprintf("run %d", run)}}, nil
	}})

	type result struct {
		status int
		body   schemaAdviceBody
	}
	send := func(session, body string, out chan<- result) {
		req := httptest.NewRequest(http.MethodPost, testIssuer+advisorPath(src.ID, "/refresh"), strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		for k, v := range sameOriginJSON {
			req.Header.Set(k, v)
		}
		r, err := e.srv.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			out <- result{}
			return
		}
		defer r.Body.Close()
		var decoded schemaAdviceBody
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		out <- result{r.StatusCode, decoded}
	}
	first, joiner := make(chan result, 1), make(chan result, 1)
	go send(e.session, `{"window_days": 14}`, first)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the first run did not start")
	}
	go send(otherSession, `{"window_days": 90}`, joiner)
	// Give the second refresh time to join; if it misses the run, it runs
	// its own and must carry its own labels.
	time.Sleep(300 * time.Millisecond)
	close(release)

	labels := map[string]struct {
		window int
		by     models.UserID
	}{"run 1": {14, e.user.ID}, "run 2": {90, other.ID}}
	for name, ch := range map[string]chan result{"first": first, "joiner": joiner} {
		r := <-ch
		if r.status != http.StatusOK || r.body.Data.Report == nil || len(r.body.Data.Report.Notes) != 1 {
			t.Fatalf("%s refresh: status %d, %+v", name, r.status, r.body.Data)
		}
		want, ok := labels[r.body.Data.Report.Notes[0]]
		if !ok || *r.body.Data.WindowDays != want.window || *r.body.Data.GeneratedBy != want.by {
			t.Errorf("%s refresh got %q labelled window %d by user %d, want window %d by user %d",
				name, r.body.Data.Report.Notes[0], *r.body.Data.WindowDays, *r.body.Data.GeneratedBy, want.window, want.by)
		}
	}
}

// TestSchemaFindingDismissals dismisses and restores a finding through the
// routes: each change stores one audit event on the source with its fields
// staged, GET lists the dismissal, and each row of the error table answers
// with its status and records nothing.
func TestSchemaFindingDismissals(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	ctx := context.Background()
	_, src := mkTestTeam(t, e.db, "dismiss", e.user)
	_, vlSource := e.vlSourceFor("http://127.0.0.1:1")
	const finding = "0123456789abcdef"
	stored := &models.StoredSchemaAdvisorReport{
		SourceID: src.ID, GeneratedAt: time.Now().UTC().Truncate(time.Second), WindowDays: 30,
		Report: models.SchemaAdvisorReport{
			Status: models.AdvisorStatusOK, ClickHouseVersion: "26.8.1", Inputs: []models.AdvisorInput{},
			Findings: []models.SchemaFinding{{ID: finding, Rule: "skip_index_bloom", Impact: models.ImpactHigh, Title: "Add a bloom filter index on request_id", Evidence: []models.AdvisorEvidence{}, DDL: []string{}}},
		},
	}
	if err := e.db.UpsertSchemaAdvisorReport(ctx, stored); err != nil {
		t.Fatal(err)
	}

	dismissalPath := func(id models.SourceID, findingID string) string {
		return advisorPath(id, "/findings/"+findingID+"/dismissal")
	}
	dismiss := func(id models.SourceID, findingID, session, body string) *testResponse {
		return e.consentRequest(http.MethodPut, dismissalPath(id, findingID), session, body, sameOriginJSON)
	}
	restore := func(id models.SourceID, findingID, session string) *testResponse {
		return e.consentRequest(http.MethodDelete, dismissalPath(id, findingID), session, "", sameOriginJSON)
	}
	type dismissalBody struct {
		Message   string `json:"message"`
		ErrorType string `json:"error_type"`
		Data      struct {
			FindingID   string         `json:"finding_id"`
			Reason      string         `json:"reason"`
			DismissedBy *models.UserID `json:"dismissed_by"`
			DismissedAt time.Time      `json:"dismissed_at"`
		} `json:"data"`
	}
	expect := func(name string, resp *testResponse, status int) dismissalBody {
		t.Helper()
		var body dismissalBody
		if status != http.StatusNoContent {
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
		}
		if resp.StatusCode != status {
			t.Fatalf("%s: status %d (%s), want %d", name, resp.StatusCode, body.Message, status)
		}
		return body
	}
	dismissals := func() []models.SchemaAdvisorDismissal {
		t.Helper()
		var body struct {
			Data struct {
				Dismissals []models.SchemaAdvisorDismissal `json:"dismissals"`
			} `json:"data"`
		}
		resp := e.consentRequest(http.MethodGet, advisorPath(src.ID, ""), e.session, "", nil)
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET: status %d, decode error %v", resp.StatusCode, err)
		}
		if body.Data.Dismissals == nil {
			t.Fatal("GET has no dismissals list")
		}
		return body.Data.Dismissals
	}

	if got := dismissals(); len(got) != 0 {
		t.Fatalf("dismissals before any = %+v, want an empty list", got)
	}
	body := expect("dismiss", dismiss(src.ID, finding, e.session, `{"reason":"covered by the replica's index"}`), http.StatusOK)
	if body.Data.FindingID != finding || body.Data.Reason != "covered by the replica's index" || body.Data.DismissedBy == nil || *body.Data.DismissedBy != e.user.ID || body.Data.DismissedAt.IsZero() {
		t.Fatalf("dismiss = %+v", body.Data)
	}
	if got := dismissals(); len(got) != 1 || got[0].FindingID != finding || got[0].Reason != "covered by the replica's index" {
		t.Fatalf("dismissals after dismiss = %+v", got)
	}
	expect("dismiss again without a body", dismiss(src.ID, finding, e.session, ""), http.StatusOK)
	expect("restore", restore(src.ID, finding, e.session), http.StatusNoContent)
	if got := dismissals(); len(got) != 0 {
		t.Fatalf("dismissals after restore = %+v, want none", got)
	}

	// Error rows: none of these may store an event.
	expect("restore a finding that is not dismissed", restore(src.ID, finding, e.session), http.StatusNotFound)
	expect("dismiss a finding the report lacks", dismiss(src.ID, "fedcba9876543210", e.session, ""), http.StatusNotFound)
	expect("dismiss on an unknown source", dismiss(src.ID+1000, finding, e.session, ""), http.StatusNotFound)
	for _, bad := range []string{"0123456789ABCDEF", "0123456789abcde", "0123456789abcdef0", "0123456789abcdeg"} {
		expect("dismiss id "+bad, dismiss(src.ID, bad, e.session, ""), http.StatusBadRequest)
		expect("restore id "+bad, restore(src.ID, bad, e.session), http.StatusBadRequest)
	}
	expect("reason of 501 bytes", dismiss(src.ID, finding, e.session, `{"reason":"`+strings.Repeat("a", 501)+`"}`), http.StatusBadRequest)
	expect("reason of 500 bytes", dismiss(src.ID, finding, e.session, `{"reason":"`+strings.Repeat("a", 500)+`"}`), http.StatusOK)
	expect("restore the 500-byte dismissal", restore(src.ID, finding, e.session), http.StatusNoContent)
	if body := expect("dismiss on VictoriaLogs", dismiss(vlSource.ID, finding, e.session, ""), http.StatusBadRequest); body.Message != "Schema advice is available for ClickHouse sources only" {
		t.Errorf("VictoriaLogs message = %q", body.Message)
	}
	_, memberSession := e.newSessionUser("dismiss-member@example.com")
	expect("member dismiss", dismiss(src.ID, finding, memberSession, ""), http.StatusForbidden)
	expect("member restore", restore(src.ID, finding, memberSession), http.StatusForbidden)
	pat, err := core.CreateAPIToken(ctx, e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "sources-read", nil, []models.TokenScope{models.TokenScopeSourcesRead})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, testIssuer+dismissalPath(src.ID, finding), http.NoBody)
	req.Header.Set("Authorization", "Bearer "+pat.Token)
	expect("token dismiss without sources:write", e.do(req), http.StatusForbidden)

	events, err := e.db.ListAuditEvents(ctx, models.AuditFilter{Ascending: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var got []models.AuditEvent
	for _, ev := range events {
		if strings.HasPrefix(ev.Action, "schema_advisor_dismissal.") {
			got = append(got, ev)
		}
	}
	want := []string{"schema_advisor_dismissal.create", "schema_advisor_dismissal.create", "schema_advisor_dismissal.delete", "schema_advisor_dismissal.create", "schema_advisor_dismissal.delete"}
	if len(got) != len(want) {
		t.Fatalf("stored %d dismissal events, want %v: %+v", len(got), want, got)
	}
	for i, ev := range got {
		detail := string(ev.Detail)
		if ev.Action != want[i] || ev.TargetType != "source" || ev.TargetID != fmt.Sprint(src.ID) || ev.Outcome != models.AuditOutcomeSuccess ||
			!strings.Contains(detail, `"finding_id":"`+finding+`"`) || strings.Contains(detail, "unstaged") {
			t.Errorf("event %d = %s %s:%s %s detail %s, want %s on source %d with the finding id", i, ev.Action, ev.TargetType, ev.TargetID, ev.Outcome, detail, want[i], src.ID)
		}
		if strings.HasSuffix(ev.Action, ".create") != strings.Contains(detail, `"rule":"skip_index_bloom"`) {
			t.Errorf("event %d %s detail %s: only a dismissal names the rule", i, ev.Action, detail)
		}
	}
	if changes := string(got[0].Changes); !strings.Contains(changes, "covered by the replica's index") {
		t.Errorf("first dismissal changes = %s, want the reason", changes)
	}
	if changes := string(got[2].Changes); !strings.Contains(changes, `"finding_id"`) {
		t.Errorf("restore changes = %s, want the removed dismissal", changes)
	}
}

// TestSchemaFindingDismissalSnapshots dismisses one finding from concurrent
// requests. Each event's previous reason must be the reason that the write
// replaced, so in a serial history every reason but the last is replaced
// exactly once and only the first write replaces nothing.
func TestSchemaFindingDismissalSnapshots(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	ctx := context.Background()
	_, src := mkTestTeam(t, e.db, "snapshots", e.user)
	const finding = "0123456789abcdef"
	if err := e.db.UpsertSchemaAdvisorReport(ctx, &models.StoredSchemaAdvisorReport{
		SourceID: src.ID, GeneratedAt: time.Now().UTC(), WindowDays: 30,
		Report: models.SchemaAdvisorReport{Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{{ID: finding, Rule: "skip_index_bloom", Evidence: []models.AdvisorEvidence{}, DDL: []string{}}}},
	}); err != nil {
		t.Fatal(err)
	}
	path := advisorPath(src.ID, "/findings/"+finding+"/dismissal")

	const writes = 24
	var wg sync.WaitGroup
	for i := range writes {
		wg.Go(func() {
			resp := e.consentRequest(http.MethodPut, path, e.session, fmt.Sprintf(`{"reason":"r%02d"}`, i), sameOriginJSON)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("PUT %d: status %d", i, resp.StatusCode)
			}
		})
	}
	wg.Wait()

	final, err := e.db.GetSchemaAdvisorDismissal(ctx, src.ID, finding)
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.db.ListAuditEvents(ctx, models.AuditFilter{Ascending: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	replaced := map[string]int{}
	for _, ev := range events {
		if ev.Action != "schema_advisor_dismissal.create" {
			continue
		}
		var changes struct {
			Reason struct {
				From *string `json:"from"`
			} `json:"reason"`
		}
		if err := json.Unmarshal(ev.Changes, &changes); err != nil {
			t.Fatal(err)
		}
		from := "<none>"
		if changes.Reason.From != nil {
			from = *changes.Reason.From
		}
		replaced[from]++
	}
	want := map[string]int{"<none>": 1}
	for i := range writes {
		if reason := fmt.Sprintf("r%02d", i); reason != final.Reason {
			want[reason] = 1
		}
	}
	if fmt.Sprint(replaced) != fmt.Sprint(want) {
		t.Fatalf("previous reasons across events = %v\nwant each replaced reason once: %v", replaced, want)
	}
}
