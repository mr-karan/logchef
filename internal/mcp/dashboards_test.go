package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// dashboardPanel is one count-mode stat panel on team and source.
type dashboardPanel struct {
	id     string
	team   models.TeamID
	source models.SourceID
}

func createDashboard(t *testing.T, w *world, owner *models.User, name string, panels ...dashboardPanel) *models.Dashboard {
	t.Helper()
	layout := make([]map[string]any, len(panels))
	blobPanels := make([]map[string]any, len(panels))
	for i, p := range panels {
		layout[i] = map[string]any{"id": p.id, "x": 0, "y": i * 2, "w": 6, "h": 2}
		blobPanels[i] = map[string]any{
			"id": p.id, "kind": "query", "title": p.id, "team_id": p.team, "source_id": p.source,
			"query": map[string]any{"mode": "count", "language": "logchefql", "text": fmt.Sprintf(`level="error" and panel=%q`, p.id)},
			"viz":   map[string]any{"type": "stat"},
		}
	}
	raw, err := json.Marshal(map[string]any{
		"version": 2, "tabs": []any{map[string]any{"id": "main", "title": "Main", "layout": layout}}, "panels": blobPanels,
	})
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := core.CreateDashboard(t.Context(), w.db, discardLogger(), owner, &models.CreateDashboardRequest{Name: name, Panels: raw})
	if err != nil {
		t.Fatalf("CreateDashboard(%s): %v", name, err)
	}
	return dashboard
}

func listedDashboardNames(t *testing.T, w *world, p *access.Principal) []string {
	t.Helper()
	result := callTool(t, w.server(), p, "list_dashboards", map[string]any{})
	var summaries []models.DashboardSummary
	if err := json.Unmarshal([]byte(result.text()), &summaries); err != nil || result.IsError {
		t.Fatalf("list_dashboards: %v %s", err, result.text())
	}
	names := make([]string, len(summaries))
	for i := range summaries {
		names[i] = summaries[i].Name
	}
	slices.Sort(names)
	return names
}

// list_dashboards and get_dashboard follow the dashboard visibility rules,
// redact panels on sources the caller cannot reach, and give an admin's
// OAuth sign-in no admin bypass.
func TestDashboardTools(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	team := createDashboard(t, w, w.member, "payments overview", dashboardPanel{"errors", w.team, w.source.ID})
	mixed := createDashboard(t, w, w.admin, "cross-team", dashboardPanel{"ours", w.team, w.source.ID}, dashboardPanel{"theirs", w.otherTeam, w.otherSource.ID})
	createDashboard(t, w, w.admin, "other team only", dashboardPanel{"theirs", w.otherTeam, w.otherSource.ID})

	member := token(w.member, models.TokenScopeDashboardsRead)
	if got, want := listedDashboardNames(t, w, member), []string{"cross-team", "payments overview"}; !slices.Equal(got, want) {
		t.Fatalf("member sees %v, want %v", got, want)
	}

	result := callTool(t, w.server(), member, "get_dashboard", map[string]any{"dashboard_id": mixed.ID})
	var resp models.DashboardResponse
	if err := json.Unmarshal([]byte(result.text()), &resp); err != nil || result.IsError {
		t.Fatalf("get_dashboard: %v %s", err, result.text())
	}
	var blob struct {
		Version int `json:"version"`
		Panels  []struct {
			ID     string          `json:"id"`
			Locked bool            `json:"locked"`
			Query  json.RawMessage `json:"query"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(resp.Panels, &blob); err != nil || blob.Version != 2 || len(blob.Panels) != 2 {
		t.Fatalf("panels = %s (%v), want a schema 2 blob with two panels", resp.Panels, err)
	}
	for _, p := range blob.Panels {
		switch p.ID {
		case "ours":
			if p.Locked || !strings.Contains(string(p.Query), `panel=\"ours\"`) {
				t.Errorf("panel ours = locked %v query %s, want its query", p.Locked, p.Query)
			}
		case "theirs":
			if !p.Locked || p.Query != nil {
				t.Errorf("panel theirs = locked %v query %s, want it locked without a query", p.Locked, p.Query)
			}
		}
	}

	adminOAuth := access.OAuthPrincipal(w.admin, 1, "logchef-cli", []models.TokenScope{models.TokenScopeDashboardsRead})
	if got, want := listedDashboardNames(t, w, &adminOAuth), []string{"cross-team", "other team only"}; !slices.Equal(got, want) {
		t.Fatalf("admin OAuth sees %v, want only the dashboards the admin created", got)
	}
	if result := callTool(t, w.server(), &adminOAuth, "get_dashboard", map[string]any{"dashboard_id": team.ID}); !result.IsError || !strings.Contains(result.text(), "dashboard not found") {
		t.Fatalf("admin OAuth reads another team's dashboard: %s", result.text())
	}
	if got := listedDashboardNames(t, w, session(w.admin)); len(got) != 3 {
		t.Fatalf("admin session sees %v, want every dashboard", got)
	}
}
