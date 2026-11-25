package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// contentAuditEnv is an audit env whose admin belongs to a team with one
// ClickHouse source.
type contentAuditEnv struct {
	*auditRouteEnv
	team   *models.Team
	source *models.Source
}

func newContentAuditEnv(t *testing.T, options ...auditEnvOption) *contentAuditEnv {
	t.Helper()
	e := newAuditRouteEnv(t, append([]auditEnvOption{withAuditLogLines()}, options...)...)
	team, source := mkTestTeam(t, e.db, "audit-content", e.admin)
	return &contentAuditEnv{auditRouteEnv: e, team: team, source: source}
}

// created decodes the data envelope of a response into v.
func created(t *testing.T, resp auditResp, v any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &envelope); err != nil {
		t.Fatalf("decode %s: %v", resp.body, err)
	}
	if err := json.Unmarshal(envelope.Data, v); err != nil {
		t.Fatalf("decode data %s: %v", envelope.Data, err)
	}
}

// wantChangeKeys fails unless the event changed exactly these keys.
func wantChangeKeys(t *testing.T, ev models.AuditEvent, keys ...string) map[string]map[string]any {
	t.Helper()
	changes := changesOf(t, ev)
	got := make([]string, 0, len(changes))
	for k := range changes {
		got = append(got, k)
	}
	if len(got) != len(keys) {
		t.Fatalf("%s changed %v, want %v", ev.Action, got, keys)
	}
	for _, k := range keys {
		if _, ok := changes[k]; !ok {
			t.Fatalf("%s changed %v, want %v", ev.Action, got, keys)
		}
	}
	return changes
}

// wantDigest fails unless the value is a digest, not text.
func wantDigest(t *testing.T, v any) {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok || m["sha256"] == nil || m["bytes"] == nil {
		t.Errorf("value %v is not a digest", v)
	}
}

func TestAuditSavedQueryEventsRecordADigest(t *testing.T) {
	t.Parallel()
	e := newContentAuditEnv(t)
	content := func(literal string) string {
		return strconv.Quote(fmt.Sprintf(`{"version":1,"sourceId":%d,"timeRange":{"relative":"15m"},"limit":100,"content":"SELECT * FROM logs WHERE session_id = '%s'","variables":[]}`, e.source.ID, literal))
	}

	resp := e.do(http.MethodPost, "/api/v1/saved-queries", fmt.Sprintf(
		`{"name":"sessions","source_id":%d,"query_language":"clickhouse-sql","editor_mode":"native","query_content":%s}`,
		e.source.ID, content("first-session-sentinel")), e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var q models.SavedQuery
	created(t, resp, &q)

	path := "/api/v1/saved-queries/" + strconv.Itoa(q.ID)
	wantStatus(t, e.do(http.MethodPut, path, `{"name":"sessions v2","query_content":`+content("second-session-sentinel")+`}`, e.cookie), http.StatusOK)
	wantStatus(t, e.do(http.MethodDelete, path, "", e.cookie), http.StatusOK)

	events := e.events()
	if len(events) != 3 {
		t.Fatalf("stored %d events, want 3", len(events))
	}
	create := events[0]
	checkActorAndTarget(t, create, "saved_query.create", e.admin, "saved_query", strconv.Itoa(q.ID))
	if create.SourceID == nil || *create.SourceID != e.source.ID {
		t.Errorf("create source_id = %v, want %d", create.SourceID, e.source.ID)
	}
	wantDigest(t, changesOf(t, create)["query_content"]["to"])

	update := wantChangeKeys(t, events[1], "name", "query_content")
	if update["name"]["from"] != "sessions" || update["name"]["to"] != "sessions v2" {
		t.Errorf("name change = %v", update["name"])
	}
	wantDigest(t, update["query_content"]["from"])
	wantDigest(t, update["query_content"]["to"])

	if del := changesOf(t, events[2]); del["name"]["from"] != "sessions v2" || del["name"]["to"] != nil {
		t.Errorf("delete changes = %v, want the deleted query as from", del)
	}
	e.wantNoText("first-session-sentinel", "second-session-sentinel")
}

func TestAuditDashboardEventsRecordASummary(t *testing.T) {
	t.Parallel()
	e := newContentAuditEnv(t)
	// The fullest dashboard: every panel holds a long query, so the blob is
	// far larger than an event may be.
	blob := func(title string) string {
		var layout, panels []string
		for i := range models.MaxDashboardPanels {
			id := fmt.Sprintf("p%d", i)
			layout = append(layout, fmt.Sprintf(`{"id":%q,"x":0,"y":%d,"w":6,"h":2}`, id, i*2))
			panels = append(panels, fmt.Sprintf(`{"id":%q,"title":%q,"type":"stat","team_id":%d,"source_id":%d,"query":%q,"query_language":"logchefql"}`,
				id, title, e.team.ID, e.source.ID, "msg:\"panel-sentinel\" "+strings.Repeat("x", 1500)))
		}
		return `{"version":1,"layout":[` + strings.Join(layout, ",") + `],"panels":[` + strings.Join(panels, ",") + `]}`
	}

	resp := e.do(http.MethodPost, "/api/v1/dashboards", `{"name":"Ops","description":"","panels":`+blob("before")+`}`, e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var d struct {
		ID       int `json:"id"`
		Revision int `json:"revision"`
	}
	created(t, resp, &d)
	path := "/api/v1/dashboards/" + strconv.Itoa(d.ID)
	wantStatus(t, e.do(http.MethodPut, path, fmt.Sprintf(`{"name":"Ops 2","description":"","revision":%d,"panels":%s}`, d.Revision, blob("after")), e.cookie), http.StatusOK)
	wantStatus(t, e.do(http.MethodDelete, path, "", e.cookie), http.StatusOK)

	events := e.events()
	if len(events) != 3 {
		t.Fatalf("stored %d events, want 3", len(events))
	}
	if c := changesOf(t, events[0]); c["panel_count"]["to"] != float64(models.MaxDashboardPanels) || c["revision"]["to"] != float64(d.Revision) {
		t.Errorf("create changes = %v", c)
	}
	update := wantChangeKeys(t, events[1], "name", "revision")
	if update["revision"]["from"] != float64(d.Revision) || update["revision"]["to"] != float64(d.Revision+1) {
		t.Errorf("revision change = %v", update["revision"])
	}
	for _, ev := range events {
		if n := len(ev.Changes) + len(ev.Detail); n > 16*1024 {
			t.Errorf("%s holds %d bytes of changes and detail, want at most 16 KiB", ev.Action, n)
		}
	}
	e.wantNoText("panel-sentinel")
}

func TestAuditQueryShareEventsStoreTheRedactedToken(t *testing.T) {
	t.Parallel()
	e := newContentAuditEnv(t, withConfig(func(c *config.Config) {
		c.Shares = config.SharesConfig{DefaultTTL: time.Hour, MaxQueryTextBytes: 4096}
	}))

	resp := e.do(http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/sources/%d/query-shares", e.team.ID, e.source.ID),
		`{"payload":{"version":2,"mode":"logchefql","query":"session:\"share-sentinel\"","limit":100,"time_range":{"relative":"15m"}}}`, e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var share struct {
		Token string `json:"token"`
	}
	created(t, resp, &share)
	if len(share.Token) <= 8 {
		t.Fatalf("token %q is too short to tell from its redaction", share.Token)
	}
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/query-shares/"+share.Token, "", e.cookie), http.StatusNoContent)

	events := e.events()
	if len(events) != 2 {
		t.Fatalf("stored %d events, want 2", len(events))
	}
	for _, ev := range events {
		if ev.TargetID != models.RedactShareToken(share.Token) {
			t.Errorf("%s target_id = %q, want the redacted token", ev.Action, ev.TargetID)
		}
	}
	wantDigest(t, changesOf(t, events[0])["payload"]["to"])
	if c := changesOf(t, events[1]); c["expires_at"]["from"] == nil || c["expires_at"]["to"] != nil {
		t.Errorf("delete changes = %v, want the deleted share as from", c)
	}
	e.wantNoText(share.Token, "share-sentinel")
}

func TestAuditAlertEventsRecordADigest(t *testing.T) {
	t.Parallel()
	e := newContentAuditEnv(t, withConfig(func(c *config.Config) { c.Alerts.Enabled = true }))

	resp := e.do(http.MethodPost, "/api/v1/alerts", fmt.Sprintf(
		`{"source_id":%d,"name":"5xx","query_language":"clickhouse-sql","editor_mode":"native","query":"SELECT count() FROM logs WHERE session_id = 'alert-sentinel'","lookback_seconds":300,"threshold_operator":"gt","threshold_value":10,"frequency_seconds":60,"severity":"warning","labels":{"team":"payments","api_token":"label-token-sentinel"},"annotations":{"runbook":"https://wiki.example.com/annotation-sentinel"},"is_active":true}`,
		e.source.ID), e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var alert models.Alert
	created(t, resp, &alert)
	path := "/api/v1/alerts/" + strconv.Itoa(int(alert.ID))
	wantStatus(t, e.do(http.MethodPut, path, `{"threshold_value":20}`, e.cookie), http.StatusOK)
	wantStatus(t, e.do(http.MethodPut, path, fmt.Sprintf(`{"no_data_policy":"keep","notify_on_resolve":%t}`, !alert.NotifyOnResolve), e.cookie), http.StatusOK)
	wantStatus(t, e.do(http.MethodDelete, path, "", e.cookie), http.StatusOK)

	events := e.events()
	if len(events) != 4 {
		t.Fatalf("stored %d events, want 4", len(events))
	}
	c := changesOf(t, events[0])
	wantDigest(t, c["query"]["to"])
	wantDigest(t, c["annotations"]["to"])
	wantDigest(t, c["labels"]["to"])
	if names, _ := c["label_names"]["to"].([]any); len(names) != 2 || names[0] != "api_token" || names[1] != "team" {
		t.Errorf("label_names = %v", c["label_names"])
	}
	update := wantChangeKeys(t, events[1], "threshold_value")
	if update["threshold_value"]["from"] != float64(10) || update["threshold_value"]["to"] != float64(20) {
		t.Errorf("threshold change = %v", update["threshold_value"])
	}
	policy := wantChangeKeys(t, events[2], "no_data_policy", "notify_on_resolve")
	if policy["no_data_policy"]["from"] != string(alert.NoDataPolicy) || policy["no_data_policy"]["to"] != "keep" ||
		policy["notify_on_resolve"]["to"] != !alert.NotifyOnResolve {
		t.Errorf("policy change = %v", policy)
	}
	if c := changesOf(t, events[3]); c["name"]["from"] != "5xx" {
		t.Errorf("delete changes = %v, want the deleted alert as from", c)
	}
	e.wantNoText("alert-sentinel", "annotation-sentinel", "label-token-sentinel")
}

func TestAuditCollectionEventsRecordEachChangeOnce(t *testing.T) {
	t.Parallel()
	e := newContentAuditEnv(t)
	member := mkTestUser(t, e.db, "collection-member@example.com", models.UserRoleMember)
	sq, err := e.db.CreateSavedQuery(t.Context(), e.source.ID, nil, "errors", "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"level:error"}`, &e.admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	resp := e.do(http.MethodPost, "/api/v1/collections", `{"name":"Runbooks","description":""}`, e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var coll models.Collection
	created(t, resp, &coll)
	base := "/api/v1/collections/" + strconv.Itoa(coll.ID)
	memberBody := fmt.Sprintf(`{"user_id":%d,"role":"editor"}`, member.ID)
	teamBody := fmt.Sprintf(`{"team_id":%d}`, e.team.ID)
	itemBody := fmt.Sprintf(`{"saved_query_id":%d,"kind":"pin"}`, sq.ID)
	item := fmt.Sprintf("%s/items/%d", base, sq.ID)

	steps := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPut, base, `{"name":"Runbooks v2","description":"on call"}`, http.StatusOK},
		{http.MethodPost, base + "/members", memberBody, http.StatusCreated},
		{http.MethodPost, base + "/members", memberBody, http.StatusCreated}, // already a member: no event
		{http.MethodPut, fmt.Sprintf("%s/members/%d", base, member.ID), `{"role":"member"}`, http.StatusOK},
		{http.MethodPut, fmt.Sprintf("%s/members/%d", base, member.ID), `{"role":"member"}`, http.StatusOK}, // same role: no event
		{http.MethodPost, base + "/teams", teamBody, http.StatusCreated},
		{http.MethodPost, base + "/teams", teamBody, http.StatusCreated}, // already shared: no event
		{http.MethodPost, base + "/items", itemBody, http.StatusCreated},
		{http.MethodPost, base + "/items", itemBody, http.StatusOK}, // same link: no event
		{http.MethodPut, item, `{"kind":"managed"}`, http.StatusOK}, // pin to managed
		{http.MethodPut, item, `{"kind":"managed"}`, http.StatusOK}, // already managed: no event
		{http.MethodDelete, item, "", http.StatusOK},                //
		{http.MethodDelete, fmt.Sprintf("%s/teams/%d", base, e.team.ID), "", http.StatusOK},
		{http.MethodDelete, fmt.Sprintf("%s/teams/%d", base, e.team.ID), "", http.StatusOK}, // not shared: no event
		{http.MethodDelete, fmt.Sprintf("%s/members/%d", base, member.ID), "", http.StatusOK},
		{http.MethodDelete, fmt.Sprintf("%s/members/%d", base, member.ID), "", http.StatusOK}, // not a member: no event
		{http.MethodDelete, base, "", http.StatusOK},
	}
	for _, s := range steps {
		if resp := e.do(s.method, s.path, s.body, e.cookie); resp.status != s.status {
			t.Fatalf("%s %s: status %d, want %d: %s", s.method, s.path, resp.status, s.status, resp.body)
		}
	}

	events := e.events()
	var actions []string
	for _, ev := range events {
		actions = append(actions, ev.Action)
	}
	want := []string{
		"collection.create", "collection.update", "collection_member.add", "collection_member.update", "collection_team.add",
		"collection_item.add", "collection_item.update", "collection_item.remove",
		"collection_team.remove", "collection_member.remove", "collection.delete",
	}
	if strings.Join(actions, " ") != strings.Join(want, " ") {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	rename := wantChangeKeys(t, events[1], "name", "description")
	if rename["name"]["from"] != "Runbooks" || rename["name"]["to"] != "Runbooks v2" {
		t.Errorf("rename = %v", rename["name"])
	}
	if c := changesOf(t, events[2]); c["role"]["to"] != "editor" {
		t.Errorf("member add = %v", c)
	}
	role := wantChangeKeys(t, events[3], "role")
	if role["role"]["from"] != "editor" || role["role"]["to"] != "member" {
		t.Errorf("member role change = %v", role["role"])
	}
	kind := wantChangeKeys(t, events[6], "kind")
	if kind["kind"]["from"] != "pin" || kind["kind"]["to"] != "managed" {
		t.Errorf("kind change = %v", kind["kind"])
	}
	if c := changesOf(t, events[7]); c["kind"]["from"] != "managed" || c["kind"]["to"] != nil {
		t.Errorf("item remove = %v", c)
	}
	if c := changesOf(t, events[10]); c["name"]["from"] != "Runbooks v2" {
		t.Errorf("collection delete = %v", c)
	}
}
