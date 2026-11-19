package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// panelsOn is a schema 2 blob with one stat panel per team/source pair.
func panelsOn(pairs ...[2]int) json.RawMessage {
	layout := make([]string, 0, len(pairs))
	panels := make([]string, 0, len(pairs))
	for i, p := range pairs {
		id := "p" + strconv.Itoa(i+1)
		layout = append(layout, fmt.Sprintf(`{"id":%q,"x":0,"y":%d,"w":6,"h":2}`, id, i*2))
		panels = append(panels, fmt.Sprintf(`{"id":%q,"kind":"query","title":%q,"team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"q%d","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}`, id, id, p[0], p[1], i+1))
	}
	return json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[` + strings.Join(layout, ",") + `]}],"panels":[` + strings.Join(panels, ",") + `]}`)
}

func pair(team *models.Team, src *models.Source) [2]int {
	return [2]int{int(team.ID), int(src.ID)}
}

func TestDashboardTeamEditorAccess(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx := context.Background()
	creator := mkTestUser(t, s.store, "creator@test.dev", models.UserRoleMember)
	editor := mkTestUser(t, s.store, "editor@test.dev", models.UserRoleMember)
	teamAdmin := mkTestUser(t, s.store, "team-admin@test.dev", models.UserRoleMember)
	member := mkTestUser(t, s.store, "member@test.dev", models.UserRoleMember)
	mixed := mkTestUser(t, s.store, "mixed@test.dev", models.UserRoleMember)

	teamA, srcA := mkTestTeam(t, s.store, "team-a", creator)
	teamB, srcB := mkTestTeam(t, s.store, "team-b", creator)
	teamC, srcC := mkTestTeam(t, s.store, "team-c", creator)
	teamL, srcL := mkTestTeam(t, s.store, "team-l", creator)
	for _, m := range []struct {
		team *models.Team
		user *models.User
		role models.TeamRole
	}{
		{teamA, editor, models.TeamRoleEditor},
		{teamC, editor, models.TeamRoleMember},
		{teamL, editor, models.TeamRoleEditor},
		{teamA, teamAdmin, models.TeamRoleAdmin},
		{teamB, teamAdmin, models.TeamRoleEditor},
		{teamA, member, models.TeamRoleMember},
		{teamA, mixed, models.TeamRoleEditor},
		{teamB, mixed, models.TeamRoleMember},
	} {
		if err := s.store.AddTeamMember(ctx, m.team.ID, m.user.ID, m.role); err != nil {
			t.Fatalf("AddTeamMember(%s, %s): %v", m.team.Name, m.user.Email, err)
		}
	}

	mkDash := func(name string, panels json.RawMessage) *models.Dashboard {
		t.Helper()
		d := &models.Dashboard{Name: name, PanelsJSON: panels, CreatedBy: &creator.ID}
		if err := s.store.CreateDashboard(ctx, d); err != nil {
			t.Fatalf("CreateDashboard(%s): %v", name, err)
		}
		return d
	}
	shared := mkDash("shared", panelsOn(pair(teamA, srcA)))
	twoTeams := mkDash("two teams", panelsOn(pair(teamA, srcA), pair(teamB, srcB)))
	unlinked := mkDash("unlinked", panelsOn(pair(teamL, srcL)))
	report := &models.Report{Name: "daily", OwnerID: &creator.ID, SourceKind: models.ReportSourceDashboard, DashboardID: &shared.ID,
		Schedule: models.ReportSchedule{Frequency: models.ReportDaily, Hour: new(9)}, Timezone: "UTC", RangeAnchor: models.ReportAnchorSendTime, MaxRows: 25}
	if err := s.store.CreateReport(ctx, report); err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	request := func(u *models.User, method, path, body string) (int, map[string]any) {
		t.Helper()
		status, resp, _ := call(t, dashboardApp(s, u), method, path, body, schema2)
		return status, resp
	}
	path := func(d *models.Dashboard) string { return "/dashboards/" + strconv.Itoa(d.ID) }
	save := func(u *models.User, d *models.Dashboard, name string, revision int, panels json.RawMessage) (int, map[string]any) {
		t.Helper()
		body := mustJSON(t, models.UpdateDashboardRequest{Name: name, Panels: panels, Revision: &revision})
		return request(u, http.MethodPut, path(d), string(body))
	}
	listRow := func(u *models.User, d *models.Dashboard) map[string]any {
		t.Helper()
		status, resp := request(u, http.MethodGet, "/dashboards", "")
		if status != http.StatusOK {
			t.Fatalf("list as %s: status %d body %v", u.Email, status, resp)
		}
		rows, _ := resp["data"].([]any)
		for _, r := range rows {
			if row, _ := r.(map[string]any); row["id"] == float64(d.ID) {
				return row
			}
		}
		t.Fatalf("%s does not see %q in the list", u.Email, d.Name)
		return nil
	}
	detailCanEdit := func(u *models.User, d *models.Dashboard) any {
		t.Helper()
		status, resp := request(u, http.MethodGet, path(d), "")
		if status != http.StatusOK {
			t.Fatalf("GET %q as %s: status %d body %v", d.Name, u.Email, status, resp)
		}
		data, _ := resp["data"].(map[string]any)
		return data["can_edit"]
	}
	stored := func(d *models.Dashboard) *models.Dashboard {
		t.Helper()
		got, err := s.store.GetDashboard(ctx, d.ID)
		if err != nil {
			t.Fatalf("GetDashboard(%q): %v", d.Name, err)
		}
		return got
	}

	t.Run("list and detail flags", func(t *testing.T) {
		for _, tc := range []struct {
			user               *models.User
			dash               *models.Dashboard
			canEdit, canDelete bool
			reportCount        float64
		}{
			{creator, shared, true, true, 1},
			{editor, shared, true, false, 0},
			{teamAdmin, shared, true, false, 0},
			{teamAdmin, twoTeams, true, false, 0},
			{member, shared, false, false, 0},
			{mixed, twoTeams, false, false, 0},
			{editor, twoTeams, false, false, 0},
		} {
			row := listRow(tc.user, tc.dash)
			if row["can_edit"] != tc.canEdit || row["can_delete"] != tc.canDelete || row["report_count"] != tc.reportCount {
				t.Errorf("list %q as %s: can_edit %v can_delete %v report_count %v, want %v %v %v",
					tc.dash.Name, tc.user.Email, row["can_edit"], row["can_delete"], row["report_count"], tc.canEdit, tc.canDelete, tc.reportCount)
			}
			if got := detailCanEdit(tc.user, tc.dash); got != tc.canEdit {
				t.Errorf("detail %q as %s: can_edit %v, want %v", tc.dash.Name, tc.user.Email, got, tc.canEdit)
			}
		}
	})

	t.Run("refused saves leave the row as it was", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			user   *models.User
			dash   *models.Dashboard
			panels json.RawMessage
		}{
			{"plain member", member, shared, panelsOn(pair(teamA, srcA))},
			{"member of one of two teams", mixed, twoTeams, panelsOn(pair(teamA, srcA), pair(teamB, srcB))},
			{"not in one of two teams", editor, twoTeams, panelsOn(pair(teamA, srcA), pair(teamB, srcB))},
			{"drops the team it cannot edit", editor, twoTeams, panelsOn(pair(teamA, srcA))},
			{"moves onto a team where it is a member", editor, shared, panelsOn(pair(teamC, srcC))},
			{"adds a team where it is a member", editor, shared, panelsOn(pair(teamA, srcA), pair(teamC, srcC))},
			{"leaves no query panel", editor, shared, panelsOn()},
		} {
			before := stored(tc.dash)
			status, resp := save(tc.user, tc.dash, "taken over", before.Revision, tc.panels)
			if status != http.StatusForbidden {
				t.Errorf("%s: status %d body %v, want 403", tc.name, status, resp)
			}
			if after := stored(tc.dash); after.Name != before.Name || after.Revision != before.Revision {
				t.Errorf("%s: row changed to %q at revision %d", tc.name, after.Name, after.Revision)
			}
		}
	})

	t.Run("team editor saves and cannot delete", func(t *testing.T) {
		status, resp := save(editor, shared, "shared v2", 1, panelsOn(pair(teamA, srcA), pair(teamA, srcA)))
		if status != http.StatusOK {
			t.Fatalf("editor save: status %d body %v, want 200", status, resp)
		}
		data, _ := resp["data"].(map[string]any)
		if data["revision"] != float64(2) || data["can_edit"] != true {
			t.Fatalf("editor save response: revision %v can_edit %v, want 2 true", data["revision"], data["can_edit"])
		}
		if d := stored(shared); d.Name != "shared v2" || d.CreatedBy == nil || *d.CreatedBy != creator.ID {
			t.Fatalf("stored after editor save: name %q created_by %v, want the creator kept", d.Name, d.CreatedBy)
		}

		if status, resp := save(teamAdmin, shared, "stale", 1, panelsOn(pair(teamA, srcA))); status != http.StatusConflict || resp["error_type"] != string(models.ConflictErrorType) {
			t.Fatalf("save from the old revision: status %d body %v, want 409 ConflictError", status, resp)
		}

		if status, resp := request(editor, http.MethodDelete, path(shared), ""); status != http.StatusForbidden {
			t.Fatalf("editor delete: status %d body %v, want 403", status, resp)
		}
		if d := stored(shared); d.Name != "shared v2" {
			t.Fatalf("dashboard gone or changed after a refused delete: %q", d.Name)
		}

		if err := s.store.UpdateTeamMemberRole(ctx, teamA.ID, editor.ID, models.TeamRoleMember); err != nil {
			t.Fatalf("revoke team edit role: %v", err)
		}
		if status, resp := save(editor, shared, "after role change", 2, panelsOn(pair(teamA, srcA))); status != http.StatusForbidden {
			t.Fatalf("save after role change: status %d body %v, want 403", status, resp)
		}
	})

	t.Run("a panel locked by an unlinked source blocks the editor, not the creator", func(t *testing.T) {
		if err := s.store.RemoveTeamSource(ctx, teamL.ID, srcL.ID); err != nil {
			t.Fatalf("RemoveTeamSource: %v", err)
		}
		if row := listRow(editor, unlinked); row["can_edit"] != true {
			t.Errorf("list reads no blob, so it still offers edit: can_edit %v", row["can_edit"])
		}
		if got := detailCanEdit(editor, unlinked); got != false {
			t.Errorf("detail as editor with a locked panel: can_edit %v, want false", got)
		}
		status, resp := save(editor, unlinked, "taken over", 1, panelsOn(pair(teamL, srcL)))
		if status != http.StatusForbidden {
			t.Fatalf("editor save over a locked panel: status %d body %v, want 403", status, resp)
		}
		if d := stored(unlinked); d.Name != "unlinked" || !strings.Contains(string(d.PanelsJSON), `"q1"`) {
			t.Fatalf("locked panel changed: %q %s", d.Name, d.PanelsJSON)
		}
		if status, resp := save(creator, unlinked, "repaired", 1, panelsOn()); status != http.StatusOK {
			t.Fatalf("creator repair: status %d body %v, want 200", status, resp)
		}
	})

	t.Run("creator can remove a panel added by an editor on another team", func(t *testing.T) {
		if err := s.store.RemoveTeamMember(ctx, teamB.ID, creator.ID); err != nil {
			t.Fatalf("remove creator from team B: %v", err)
		}
		dashboard := mkDash("creator repair", panelsOn(pair(teamA, srcA)))
		if status, resp := save(teamAdmin, dashboard, dashboard.Name, 1, panelsOn(pair(teamA, srcA), pair(teamB, srcB))); status != http.StatusOK {
			t.Fatalf("editor adds panel: status %d body %v", status, resp)
		}
		status, resp := request(creator, http.MethodGet, path(dashboard), "")
		if status != http.StatusOK {
			t.Fatalf("creator reads dashboard: status %d body %v", status, resp)
		}
		data := resp["data"].(map[string]any)
		panels := string(mustJSON(t, data["panels"]))
		if data["can_edit"] != true || strings.Contains(panels, `"q2"`) || !strings.Contains(panels, `"locked":true`) {
			t.Fatalf("creator should be able to remove the redacted panel: %v", data)
		}
		if status, resp := save(creator, dashboard, dashboard.Name, 2, json.RawMessage(panels)); status != http.StatusBadRequest {
			t.Fatalf("creator resubmits locked panel: status %d body %v, want 400", status, resp)
		}
		if status, resp := save(creator, dashboard, dashboard.Name, 2, panelsOn(pair(teamA, srcA), pair(teamA, srcA))); status != http.StatusForbidden {
			t.Fatalf("creator replaces locked panel: status %d body %v, want 403", status, resp)
		}
		if status, resp := save(creator, dashboard, dashboard.Name, 2, panelsOn(pair(teamA, srcA))); status != http.StatusOK {
			t.Fatalf("creator removes locked panel: status %d body %v, want 200", status, resp)
		}
	})
}
