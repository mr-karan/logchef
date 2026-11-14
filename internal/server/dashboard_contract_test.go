package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// dashboardApp mounts every dashboard route for one user.
func dashboardApp(s *Server, user *models.User) *fiber.App {
	app := fiber.New()
	withUser(app, http.MethodGet, "/dashboards", user, s.handleListDashboards)
	withUser(app, http.MethodPost, "/dashboards", user, s.handleCreateDashboard)
	withUser(app, http.MethodGet, "/dashboards/:dashboardID", user, s.handleGetDashboard)
	withUser(app, http.MethodPut, "/dashboards/:dashboardID", user, s.handleUpdateDashboard)
	withUser(app, http.MethodDelete, "/dashboards/:dashboardID", user, s.handleDeleteDashboard)
	return app
}

// dashboardHeaders are the client contract headers of one request. A nil
// schema sends no schema header.
type dashboardHeaders struct {
	schema   *string
	features *string
}

var (
	noHeaders = dashboardHeaders{}
	schema1   = dashboardHeaders{schema: new("1")}
	schema2   = dashboardHeaders{schema: new("2")}
)

// mustJSON encodes v for a comparison and fails the test when it cannot.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// call sends one dashboard request and decodes the JSON envelope.
func call(t *testing.T, app *fiber.App, method, path, body string, h dashboardHeaders) (status int, decoded map[string]any, raw []byte) {
	t.Helper()
	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if h.schema != nil {
		req.Header.Set(models.DashboardSchemaHeader, *h.schema)
	}
	if h.features != nil {
		req.Header.Set(models.DashboardFeaturesHeader, *h.features)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("app.Test(%s %s): %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode %s %s response: %v\n%s", method, path, err, raw)
	}
	return resp.StatusCode, decoded, raw
}

func panelBlobV2JSON(teamID models.TeamID, sourceID models.SourceID) string {
	return fmt.Sprintf(
		`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}]}],"panels":[{"id":"p1","kind":"query","title":"a","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"x","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}]}`,
		int(teamID), int(sourceID),
	)
}

// canonJSON re-encodes a JSON text with sorted keys so two encodings compare.
func canonJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("canonJSON: %v", err)
	}
	return string(mustJSON(t, v))
}

func storedBlob(t *testing.T, s *Server, id int) string {
	t.Helper()
	d, err := s.store.GetDashboard(context.Background(), id)
	if err != nil {
		t.Fatalf("GetDashboard(%d): %v", id, err)
	}
	return string(d.PanelsJSON)
}

// TestDashboardSchemaHeader covers the read table of "Client schema contract"
// over HTTP: an absent header and 1 read a schema 1 row as stored, 2 reads it
// upgraded, every other value is 400, and a schema 2 row is the stub for a
// schema 1 client.
func TestDashboardSchemaHeader(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "team", creator)
	app := dashboardApp(s, creator)

	v1 := &models.Dashboard{Name: "v1", PanelsJSON: panelBlobJSON(team.ID, src.ID), CreatedBy: &creator.ID}
	v2 := &models.Dashboard{Name: "v2", PanelsJSON: json.RawMessage(panelBlobV2JSON(team.ID, src.ID)), CreatedBy: &creator.ID}
	for _, d := range []*models.Dashboard{v1, v2} {
		if err := s.store.CreateDashboard(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	upgraded, err := models.UpgradeDashboardPanels(v1.PanelsJSON)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		headers dashboardHeaders
		row     *models.Dashboard
		want    string
		stub    bool
	}{
		{"absent reads schema 1 as stored", noHeaders, v1, string(v1.PanelsJSON), false},
		{"explicit 1 reads schema 1 as stored", schema1, v1, string(v1.PanelsJSON), false},
		{"2 reads schema 1 upgraded", schema2, v1, string(upgraded), false},
		{"2 reads schema 2 as stored", schema2, v2, string(v2.PanelsJSON), false},
		{"absent reads schema 2 as the stub", noHeaders, v2, "", true},
		{"explicit 1 reads schema 2 as the stub", schema1, v2, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, resp, _ := call(t, app, http.MethodGet, "/dashboards/"+strconv.Itoa(tc.row.ID), "", tc.headers)
			if status != http.StatusOK {
				t.Fatalf("status = %d body %v", status, resp)
			}
			data := resp["data"].(map[string]any)
			panels := mustJSON(t, data["panels"])
			if tc.stub {
				if data["panels_corrupt"] != true || data["min_schema"] != float64(2) || data["can_edit"] != false || data["corrupt"] != false {
					t.Fatalf("response is not the stub: %v", data)
				}
				if canonJSON(t, panels) != canonJSON(t, models.EmptyDashboardPanelsJSON()) {
					t.Fatalf("stub panels = %s", panels)
				}
				return
			}
			if canonJSON(t, panels) != canonJSON(t, []byte(tc.want)) {
				t.Fatalf("panels\n got %s\nwant %s", panels, tc.want)
			}
			if _, present := data["panels_corrupt"]; present || data["corrupt"] != false || data["can_edit"] != true || data["revision"] != float64(1) {
				t.Fatalf("response fields = %v", data)
			}
		})
	}

	for _, value := range []string{"", "0", "3", "1,2", " ", "\u00a02", "2\u2003"} {
		t.Run("invalid "+strconv.Quote(value), func(t *testing.T) {
			for _, path := range []string{"/dashboards", "/dashboards/" + strconv.Itoa(v1.ID)} {
				status, resp, _ := call(t, app, http.MethodGet, path, "", dashboardHeaders{schema: new(value)})
				if status != http.StatusBadRequest || resp["message"] != models.DashboardSchemaHeader+" must be 1 or 2" {
					t.Fatalf("GET %s with schema %q: status %d body %v", path, value, status, resp)
				}
			}
		})
	}
}

// TestDashboardStaleClientMatrix is the stale-client matrix of "Client schema
// contract". It stores a blob that uses every feature up to this phase and
// sends an update as each earlier kind of client. The cases grow with the
// feature table, and every refused save leaves the row byte for byte as it was.
func TestDashboardStaleClientMatrix(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "team", creator)
	app := dashboardApp(s, creator)

	// The current client stores a chart and a trend using every shipped feature.
	blob := strings.Replace(panelBlobV2JSON(team.ID, src.ID), `"type":"stat","reduce":"sum"`, `"type":"chart","x":"day","y":["n"],"show_values":true,"columns":{},"format":{"decimals":2}`, 1)
	blob = strings.Replace(blob, `"mode":"count"`, `"mode":"result"`, 1)
	blob = strings.Replace(blob, `,"interval":"auto"`, ``, 1)
	blob = strings.Replace(blob, `"panels":[`, fmt.Sprintf(`"panels":[{"id":"p2","kind":"query","title":"Trend","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT day,n FROM events"},"viz":{"type":"stat","value":"n","order_by":"day","compare":{"mode":"previous_row"},"sparkline":true}},`, team.ID, src.ID), 1)
	blob = strings.Replace(blob, `"layout":[`, `"layout":[{"id":"p2","x":6,"y":0,"w":6,"h":2},`, 1)
	blob = strings.Replace(blob, `"type":"stat","value":"n"`, `"type":"stat","thresholds":[{"value":null,"color":"green"},{"value":"9007199254740993.01","color":"red"}],"value":"n"`, 1)
	blob = strings.Replace(blob, `"panels":[`, fmt.Sprintf(`"panels":[{"id":"p3","kind":"query","title":"Table","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT ts,details FROM events"},"viz":{"type":"table","rules":[{"columns":["details"],"op":"contains","value":"retry","scope":"row","color":"red"}],"column_formats":{"ts":{"kind":"time","time":"seconds"}}}},`, team.ID, src.ID), 1)
	blob = strings.Replace(blob, `"layout":[`, `"layout":[{"id":"p3","x":0,"y":2,"w":12,"h":2},`, 1)
	status, resp, _ := call(t, app, http.MethodPost, "/dashboards", `{"name":"matrix","panels":`+blob+`}`, dashboardHeaders{schema: new("2"), features: new(strings.Join(models.KnownDashboardFeatures(), ","))})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body %v", status, resp)
	}
	id := int(resp["data"].(map[string]any)["id"].(float64))
	path := "/dashboards/" + strconv.Itoa(id)
	features := models.KnownDashboardFeatures()
	sort.Strings(features)
	if got := models.DashboardPanelsStoredFeatures(json.RawMessage(storedBlob(t, s, id))); len(got) != len(features) {
		t.Fatalf("stored features = %v, want every known feature %v", got, features)
	}
	before := storedBlob(t, s, id)
	v1Body := fmt.Sprintf(`{"name":"matrix","revision":1,"panels":%s}`, panelBlobJSON(team.ID, src.ID))
	v2Body := `{"name":"matrix","revision":1,"panels":` + blob + `}`

	type matrixCase struct {
		name    string
		body    string
		headers dashboardHeaders
		want    int
		message string
	}
	cases := []matrixCase{
		{"a tab or a script from before phase 2 sends no revision", `{"name":"matrix","panels":` + string(panelBlobJSON(team.ID, src.ID)) + `}`, noHeaders, http.StatusPreconditionRequired, "Reload the page"},
		{"a schema 1 client from phase 2 on sends a schema 1 body", v1Body, noHeaders, http.StatusConflict, "A newer Logchef saved this dashboard"},
	}
	for i := range features {
		earlier := strings.Join(features[:i], ",")
		cases = append(cases, matrixCase{
			"a schema 2 client from an earlier phase declares " + strconv.Quote(earlier),
			v2Body, dashboardHeaders{schema: new("2"), features: new(earlier)},
			http.StatusConflict, strings.Join(features[i:], ", "),
		})
	}
	cases = append(cases, matrixCase{"the current client", v2Body, dashboardHeaders{schema: new("2"), features: new(strings.Join(features, ","))}, http.StatusOK, ""})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp, _ := call(t, app, http.MethodPut, path, tc.body, tc.headers)
			if status != tc.want || !strings.Contains(fmt.Sprint(resp["message"]), tc.message) {
				t.Fatalf("status %d body %v, want %d with %q", status, resp, tc.want, tc.message)
			}
			if tc.want != http.StatusOK {
				if resp["error_type"] != string(models.ConflictErrorType) && tc.want == http.StatusConflict {
					t.Fatalf("error_type = %v, want ConflictError", resp["error_type"])
				}
				if after := storedBlob(t, s, id); after != before {
					t.Fatalf("a refused save changed the row:\n got %s\nwant %s", after, before)
				}
			}
		})
	}
}

// TestDashboardFeaturesHeader covers the features header grammar on writes:
// trimmed names, blanks, duplicates, case sensitivity and unknown names never
// block a feature-free row, a feature-free row saves with no header at all,
// and a stored feature this server does not know refuses the write and is
// served as the stub.
func TestDashboardFeaturesHeader(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "team", creator)
	app := dashboardApp(s, creator)

	dash := &models.Dashboard{Name: "free", PanelsJSON: json.RawMessage(panelBlobV2JSON(team.ID, src.ID)), CreatedBy: &creator.ID}
	if err := s.store.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatal(err)
	}
	path := "/dashboards/" + strconv.Itoa(dash.ID)
	body := func(revision int) string {
		return fmt.Sprintf(`{"name":"free","revision":%d,"panels":%s}`, revision, panelBlobV2JSON(team.ID, src.ID))
	}
	revision := 1
	for _, features := range []*string{nil, new(""), new(" , ,, "), new("Tabs, TABS"), new(" made-up , made-up ")} {
		status, resp, _ := call(t, app, http.MethodPut, path, body(revision), dashboardHeaders{schema: new("2"), features: features})
		label := "<absent>"
		if features != nil {
			label = strconv.Quote(*features)
		}
		if status != http.StatusOK {
			t.Fatalf("features header %s: status %d body %v", label, status, resp)
		}
		revision = int(resp["data"].(map[string]any)["revision"].(float64))
	}

	// A newer server stored a feature this one does not know.
	newer := strings.Replace(panelBlobV2JSON(team.ID, src.ID), `"features":[]`, `"features":["tabs"]`, 1)
	seeded := &models.Dashboard{Name: "newer", PanelsJSON: json.RawMessage(newer), CreatedBy: &creator.ID}
	if err := s.store.CreateDashboard(context.Background(), seeded); err != nil {
		t.Fatal(err)
	}
	newerPath := "/dashboards/" + strconv.Itoa(seeded.ID)
	status, resp, _ := call(t, app, http.MethodPut, newerPath, body(1), dashboardHeaders{schema: new("2"), features: new("tabs")})
	if status != http.StatusConflict || !strings.Contains(fmt.Sprint(resp["message"]), "tabs") {
		t.Fatalf("write to a row with an unknown stored feature: status %d body %v", status, resp)
	}
	if got := storedBlob(t, s, seeded.ID); got != newer {
		t.Fatalf("refused write changed the row: %s", got)
	}
	for _, h := range []dashboardHeaders{noHeaders, schema2} {
		status, resp, _ := call(t, app, http.MethodGet, newerPath, "", h)
		data := resp["data"].(map[string]any)
		if status != http.StatusOK || data["panels_corrupt"] != true || data["min_schema"] != float64(2) || data["can_edit"] != false {
			t.Fatalf("GET of a row with an unknown stored feature: status %d body %v, want the stub", status, resp)
		}
	}
}

// TestDashboardCorruptGet covers GET on a corrupt row, unparseable and
// parseable but invalid: the creator and an admin get 200 with corrupt,
// the opaque raw blob and no panels; a team member and a stranger get 403.
func TestDashboardCorruptGet(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	member := mkTestUser(t, s.store, "member@test.dev", models.UserRoleMember)
	stranger := mkTestUser(t, s.store, "stranger@test.dev", models.UserRoleMember)
	admin := mkTestUser(t, s.store, "admin@test.dev", models.UserRoleAdmin)
	team, src := mkTestTeam(t, s.store, "team", creator, member)

	blobs := map[string]string{
		"unparseable": `{not json`,
		"invalid":     fmt.Sprintf(`{"version":1,"layout":[],"panels":[{"id":"p1","type":"pie","team_id":%d,"source_id":%d,"query_language":"logchefql"}]}`, team.ID, src.ID),
		"invalid v2":  fmt.Sprintf(`{"version":2,"features":[],"tabs":[],"panels":[{"id":"p1","kind":"query","title":"a","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"x"},"viz":{"type":"stat"}}]}`, team.ID, src.ID),
	}
	for name, blob := range blobs {
		t.Run(name, func(t *testing.T) {
			dash := &models.Dashboard{Name: name, PanelsJSON: json.RawMessage(blob), CreatedBy: &creator.ID}
			if err := s.store.CreateDashboard(context.Background(), dash); err != nil {
				t.Fatal(err)
			}
			path := "/dashboards/" + strconv.Itoa(dash.ID)
			for _, viewer := range []*models.User{creator, admin} {
				for _, h := range []dashboardHeaders{noHeaders, schema2} {
					status, resp, raw := call(t, dashboardApp(s, viewer), http.MethodGet, path, "", h)
					data, _ := resp["data"].(map[string]any)
					if status != http.StatusOK || data["corrupt"] != true || data["raw_panels_json"] != blob || data["can_edit"] != true {
						t.Fatalf("%s GET: status %d body %s", viewer.Email, status, raw)
					}
					if _, present := data["panels"]; present {
						t.Fatalf("%s GET returned panels for a corrupt row: %s", viewer.Email, raw)
					}
				}
			}
			for _, viewer := range []*models.User{member, stranger} {
				status, _, raw := call(t, dashboardApp(s, viewer), http.MethodGet, path, "", schema2)
				if status != http.StatusForbidden || strings.Contains(string(raw), "raw_panels_json") {
					t.Fatalf("%s GET: status %d body %s, want 403 without the raw blob", viewer.Email, status, raw)
				}
			}
			// The list shows it to the creator as corrupt and hides it from the member.
			status, resp, _ := call(t, dashboardApp(s, creator), http.MethodGet, "/dashboards", "", schema2)
			if status != http.StatusOK {
				t.Fatalf("list: %d %v", status, resp)
			}
			var found bool
			for _, item := range resp["data"].([]any) {
				row := item.(map[string]any)
				if int(row["id"].(float64)) != dash.ID {
					continue
				}
				found = true
				if row["corrupt"] != true || row["panel_count"] != float64(0) || row["can_edit"] != true {
					t.Fatalf("creator's list row = %v", row)
				}
				if _, present := row["raw_panels_json"]; present {
					t.Fatalf("list exposed the raw blob: %v", row)
				}
			}
			if !found {
				t.Fatal("creator's list lacks the corrupt row")
			}
			_, resp, _ = call(t, dashboardApp(s, member), http.MethodGet, "/dashboards", "", schema2)
			for _, item := range resp["data"].([]any) {
				if int(item.(map[string]any)["id"].(float64)) == dash.ID {
					t.Fatal("member's list shows the corrupt row")
				}
			}
		})
	}
}

// TestDashboardListSummaries is the "Dashboard list" check: two sets of 300
// dashboards with the same names, descriptions, panel counts and kinds, one
// with short queries and one padded to within 1 KiB of the 512 KiB limit,
// list to responses of the same length below 1 MiB. No element carries
// panels for a schema 2 client; an absent header and explicit 1 get the
// constant compatibility fields. The backfill over both sets runs twice and
// the second run writes nothing.
func TestDashboardListSummaries(t *testing.T) {
	const perSet = 300
	ctx := context.Background()
	blob := func(team models.TeamID, src models.SourceID, query string) string {
		return fmt.Sprintf(
			`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}]}],"panels":[`+
				`{"id":"p1","kind":"query","title":"a","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":%q,"limit":100},"viz":{"type":"table"}},`+
				`{"id":"p2","kind":"query","title":"b","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"x","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}]}`,
			int(team), int(src), query, int(team), int(src))
	}

	// One server per set, so the two lists have the same ids, names and
	// descriptions and differ only in the stored blobs.
	type set struct {
		server *Server
		app    *fiber.App
		raw    []byte
	}
	sets := make([]*set, 0, 2)
	for i, pad := range []bool{false, true} {
		s := newDashboardTestServer(t)
		creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
		team, src := mkTestTeam(t, s.store, "team", creator)
		query := "SELECT 1"
		if pad {
			short := blob(team.ID, src.ID, query)
			query += strings.Repeat("x", models.MaxDashboardPanelsSizeV2-512-len(short))
		}
		text := blob(team.ID, src.ID, query)
		if pad && (len(text) < models.MaxDashboardPanelsSizeV2-1024 || len(text) > models.MaxDashboardPanelsSizeV2) {
			t.Fatalf("near-limit blob is %d bytes", len(text))
		}
		if err := models.ValidateDashboardPanels(json.RawMessage(text)); err != nil {
			t.Fatalf("set %d fixture blob invalid: %v", i, err)
		}
		err := s.store.WithTx(ctx, func(tx store.StoreOps) error {
			for n := range perSet {
				d := &models.Dashboard{Name: fmt.Sprintf("dashboard %03d", n), Description: strings.Repeat("d", 40), PanelsJSON: json.RawMessage(text), CreatedBy: &creator.ID}
				if err := tx.CreateDashboard(ctx, d); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seed set %d: %v", i, err)
		}
		app := dashboardApp(s, creator)
		status, resp, raw := call(t, app, http.MethodGet, "/dashboards", "", schema2)
		if status != http.StatusOK {
			t.Fatalf("set %d list: %d", i, status)
		}
		rows := resp["data"].([]any)
		if len(rows) != perSet {
			t.Fatalf("set %d list has %d rows, want %d", i, len(rows), perSet)
		}
		for _, item := range rows {
			row := item.(map[string]any)
			if _, present := row["panels"]; present {
				t.Fatalf("schema 2 list row carries panels: %v", row)
			}
			if row["panel_count"] != float64(2) || row["tab_count"] != float64(1) || row["corrupt"] != false {
				t.Fatalf("list row = %v", row)
			}
			if kinds := mustJSON(t, row["panel_types"]); string(kinds) != `["stat","table"]` {
				t.Fatalf("panel_types = %s", kinds)
			}
		}
		sets = append(sets, &set{server: s, app: app, raw: raw})
	}
	if len(sets[0].raw) != len(sets[1].raw) {
		t.Fatalf("short set lists to %d bytes and near-limit set to %d bytes", len(sets[0].raw), len(sets[1].raw))
	}
	if len(sets[1].raw) >= 1<<20 {
		t.Fatalf("list response is %d bytes, want below 1 MiB", len(sets[1].raw))
	}

	for _, h := range []dashboardHeaders{noHeaders, schema1} {
		_, resp, _ := call(t, sets[1].app, http.MethodGet, "/dashboards", "", h)
		for _, item := range resp["data"].([]any) {
			row := item.(map[string]any)
			panels := mustJSON(t, row["panels"])
			if canonJSON(t, panels) != canonJSON(t, models.EmptyDashboardPanelsJSON()) || row["panels_corrupt"] != true || row["can_edit"] != true {
				t.Fatalf("schema 1 list row lacks the compatibility fields: %v", row)
			}
		}
	}

	for _, set := range sets {
		for pass := range 2 {
			n, err := core.BackfillDashboardSummaries(ctx, set.server.store, set.server.log)
			if err != nil || n != 0 {
				t.Fatalf("backfill pass %d wrote %d rows, %v; want 0 for rows written by this server", pass, n, err)
			}
		}
	}
}

// TestDashboardNoRedactionGap feeds a blob with a locked panel through every
// route that returns panels to a viewer and asserts that the query text is
// absent. In this release a viewer can only read, so GET runs as the viewer in
// both client schemas; create and update return the creator's own panels.
func TestDashboardNoRedactionGap(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	viewer := mkTestUser(t, s.store, "viewer@test.dev", models.UserRoleMember)
	teamA, srcA := mkTestTeam(t, s.store, "team-a", creator, viewer)
	teamB, srcB := mkTestTeam(t, s.store, "team-b", creator)

	blob := fmt.Sprintf(
		`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}]}],"panels":[`+
			`{"id":"p1","kind":"query","title":"A","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"queryA"},"viz":{"type":"stat"}},`+
			`{"id":"p2","kind":"query","title":"B","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":"queryB","limit":5},"viz":{"type":"table","visible_columns":["secret_column"]}}]}`,
		int(teamA.ID), int(srcA.ID), int(teamB.ID), int(srcB.ID))
	dash := &models.Dashboard{Name: "shared", PanelsJSON: json.RawMessage(blob), CreatedBy: &creator.ID}
	if err := s.store.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatal(err)
	}
	path := "/dashboards/" + strconv.Itoa(dash.ID)

	status, resp, raw := call(t, dashboardApp(s, viewer), http.MethodGet, path, "", schema2)
	if status != http.StatusOK {
		t.Fatalf("viewer GET: %d %v", status, resp)
	}
	if strings.Contains(string(raw), "queryB") || strings.Contains(string(raw), "secret_column") || !strings.Contains(string(raw), "queryA") {
		t.Fatalf("viewer GET leaked or over-redacted: %s", raw)
	}
	if !strings.Contains(string(raw), `"locked":true`) || !strings.Contains(string(raw), `"viz":{"type":"table"}`) {
		t.Fatalf("locked panel placeholder missing: %s", raw)
	}
	if status, _, raw := call(t, dashboardApp(s, viewer), http.MethodGet, path, "", noHeaders); status != http.StatusOK || strings.Contains(string(raw), "queryB") {
		t.Fatalf("viewer GET as a schema 1 client: %d %s", status, raw)
	}
	for _, h := range []dashboardHeaders{noHeaders, schema2} {
		status, _, raw := call(t, dashboardApp(s, viewer), http.MethodGet, "/dashboards", "", h)
		if status != http.StatusOK || strings.Contains(string(raw), "queryB") || strings.Contains(string(raw), "queryA") {
			t.Fatalf("list to the viewer carried a query: %d %s", status, raw)
		}
	}
}

// TestDashboardTextBoundsHTTP covers the name and description bounds as 400s.
func TestDashboardTextBoundsHTTP(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "owner@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "team", creator)
	app := dashboardApp(s, creator)

	longName := mustJSON(t, strings.Repeat("é", models.MaxDashboardNameRunes+1))
	longDescription := mustJSON(t, strings.Repeat("日", models.MaxDashboardDescriptionRunes+1))
	for _, body := range []string{
		`{"name":` + string(longName) + `,"panels":` + panelBlobV2JSON(team.ID, src.ID) + `}`,
		`{"name":"ok","description":` + string(longDescription) + `,"panels":` + panelBlobV2JSON(team.ID, src.ID) + `}`,
	} {
		status, resp, _ := call(t, app, http.MethodPost, "/dashboards", body, schema2)
		if status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(resp["message"]), "character limit") {
			t.Fatalf("status %d body %v", status, resp)
		}
	}
	okName := mustJSON(t, strings.Repeat("é", models.MaxDashboardNameRunes))
	status, resp, _ := call(t, app, http.MethodPost, "/dashboards", `{"name":`+string(okName)+`,"panels":`+panelBlobV2JSON(team.ID, src.ID)+`}`, schema2)
	if status != http.StatusCreated {
		t.Fatalf("200-rune name: status %d body %v", status, resp)
	}
}

func TestDashboardCanonicalWritesHTTP(t *testing.T) {
	s := newDashboardTestServer(t)
	owner := mkTestUser(t, s.store, "canonical@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "team", owner)
	app := dashboardApp(s, owner)
	original := panelBlobV2JSON(team.ID, src.ID)
	status, resp, _ := call(t, app, "POST", "/dashboards", `{"name":"safe","panels":`+original+`}`, schema2)
	if status != 201 {
		t.Fatalf("create: %d %v", status, resp)
	}
	id := int(resp["data"].(map[string]any)["id"].(float64))
	path := "/dashboards/" + strconv.Itoa(id)
	before := storedBlob(t, s, id)
	for name, blob := range map[string]string{
		"canonical size": strings.Replace(original, `"text":"x"`, `"text":"`+strings.Repeat("\u2028", 100_000)+`"`, 1),
		"missing panels": `{"version":2,"tabs":[{"id":"main","title":"Main","layout":[]}]}`,
		"missing layout": `{"version":2,"tabs":[{"id":"main","title":"Main"}],"panels":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rowsBefore, err := s.store.ListDashboardSummaries(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"POST", "PUT"} {
				target := "/dashboards"
				if method == "PUT" {
					target = path
				}
				status, resp, _ := call(t, app, method, target, `{"name":"bad","revision":1,"panels":`+blob+`}`, schema2)
				if status != 400 {
					t.Fatalf("%s: %d %v", method, status, resp)
				}
				if name == "canonical size" {
					want := "invalid dashboard configuration: panels request is 300301 bytes; its stored form would be 600301 bytes after encoding, over the 524288-byte limit"
					if resp["message"] != want {
						t.Fatalf("%s: message %v, want %q", method, resp["message"], want)
					}
				}
			}
			rowsAfter, err := s.store.ListDashboardSummaries(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			unchanged, err := s.store.GetDashboard(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if len(rowsAfter) != len(rowsBefore) || string(unchanged.PanelsJSON) != before || unchanged.Revision != 1 || unchanged.Name != "safe" {
				t.Fatal("rejected write changed storage")
			}
		})
	}
	for _, value := range []string{"3", "\u00a02", "2\u2003"} {
		status, resp, _ := call(t, app, "DELETE", path, "", dashboardHeaders{schema: new(value)})
		if status != 400 || storedBlob(t, s, id) != before {
			t.Fatalf("DELETE schema %q: %d %v", value, status, resp)
		}
	}
	duplicate := strings.ReplaceAll(string(panelBlobJSON(team.ID, src.ID)), `"id":"p1"`, `"id":"p1","id":null`)
	status, resp, _ = call(t, app, "POST", "/dashboards", `{"name":"duplicate null","panels":`+duplicate+`}`, schema2)
	if status != 201 || resp["data"].(map[string]any)["corrupt"] != false {
		t.Fatalf("duplicate null: %d %v", status, resp)
	}
	upgraded := resp["data"].(map[string]any)["panels"].(map[string]any)
	panelID := upgraded["panels"].([]any)[0].(map[string]any)["id"]
	layoutID := upgraded["tabs"].([]any)[0].(map[string]any)["layout"].([]any)[0].(map[string]any)["id"]
	if panelID != "p1" || layoutID != "p1" {
		t.Fatalf("duplicate null upgraded ids: panel %v layout %v, want p1", panelID, layoutID)
	}
}

type perpetuallyStaleDashboardStore struct {
	store.Store
	passes int
}

func (s *perpetuallyStaleDashboardStore) ListDashboardSummaries(ctx context.Context) ([]*models.DashboardSummary, error) {
	rows, err := s.Store.ListDashboardSummaries(ctx)
	s.passes++
	for _, row := range rows {
		row.SummaryRevision = 0
	}
	return rows, err
}
func TestDashboardListRepairExhaustedHTTP(t *testing.T) {
	s := newDashboardTestServer(t)
	owner := mkTestUser(t, s.store, "busy@test.dev", models.UserRoleMember)
	d := &models.Dashboard{Name: "busy", CreatedBy: &owner.ID, PanelsJSON: models.EmptyDashboardPanelsJSON()}
	if err := s.store.CreateDashboard(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	race := &perpetuallyStaleDashboardStore{Store: s.store}
	s.store = race
	req := httptest.NewRequest(http.MethodGet, "/dashboards", http.NoBody)
	resp, err := dashboardApp(s, owner).Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "1" || race.passes != 3 {
		t.Fatalf("status %d, headers %v, passes %d", resp.StatusCode, resp.Header, race.passes)
	}
}

func TestDashboardStatCompareContract(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.store, "trend@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.store, "trend", creator)
	app := dashboardApp(s, creator)
	blob := strings.Replace(panelBlobV2JSON(team.ID, src.ID), `"reduce":"sum"`, `"reduce":"last","compare":{"mode":"previous_row","good":"up"},"sparkline":true`, 1)
	body := fmt.Sprintf(`{"name":"Trend","panels":%s}`, blob)
	status, _, _ := call(t, app, http.MethodPost, "/dashboards", body, schema2)
	if status != http.StatusCreated {
		t.Fatalf("undeclared create status %d", status)
	}
	headers := dashboardHeaders{schema: new("2"), features: new("stat_compare,result_frames")}

	plain, err := core.CreateDashboard(context.Background(), s.store, s.log, creator, &models.CreateDashboardRequest{Name: "Plain", Panels: json.RawMessage(panelBlobV2JSON(team.ID, src.ID))})
	if err != nil {
		t.Fatal(err)
	}
	introduction := fmt.Sprintf(`{"name":"Plain","revision":1,"panels":%s}`, blob)
	status, _, _ = call(t, app, http.MethodPut, fmt.Sprintf("/dashboards/%d", plain.ID), introduction, schema2)
	if status != http.StatusOK {
		t.Fatalf("undeclared feature introduction: %d", status)
	}
	status, resp, _ := call(t, app, http.MethodPost, "/dashboards", body, headers)
	if status != http.StatusCreated {
		t.Fatalf("declared create %d %v", status, resp)
	}
	data := resp["data"].(map[string]any)
	id := int(data["id"].(float64))
	path := fmt.Sprintf("/dashboards/%d", id)
	for _, h := range []dashboardHeaders{schema2, headers} {
		status, resp, _ = call(t, app, http.MethodGet, path, "", h)
		data = resp["data"].(map[string]any)
		if status != http.StatusOK || data["can_edit"] != (h.features != nil) {
			t.Fatalf("read status %d data %v", status, data)
		}
	}
	update := fmt.Sprintf(`{"name":"Trend","revision":1,"panels":%s}`, blob)
	staleBody := fmt.Sprintf(`{"name":"Trend","revision":1,"panels":%s}`, panelBlobV2JSON(team.ID, src.ID))
	oldClient := dashboardHeaders{schema: new("2"), features: new("result_frames")}
	status, resp, _ = call(t, app, http.MethodPut, path, staleBody, oldClient)
	if status != http.StatusConflict || !strings.Contains(fmt.Sprint(resp), "stat_compare") {
		t.Fatalf("undeclared update %d %v", status, resp)
	}
	status, resp, _ = call(t, app, http.MethodPut, path, update, headers)
	if status != http.StatusOK {
		t.Fatalf("declared update %d %v", status, resp)
	}
}
