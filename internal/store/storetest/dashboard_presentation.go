package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testDashboardPresentation(t *testing.T, ctx context.Context, s store.Store) {
	owner := mkUser(t, ctx, s, "presentation@test.dev")
	team := &models.Team{Name: "Presentation"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTeamMember(ctx, team.ID, owner.ID, models.TeamRoleAdmin); err != nil {
		t.Fatal(err)
	}
	src := mkSource(t, ctx, s, "presentation")
	if err := s.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(fmt.Sprintf(`{"version":2,"tabs":[{"id":"main","title":"Main","layout":[{"id":"p","x":0,"y":0,"w":6,"h":2},{"id":"table","x":6,"y":0,"w":6,"h":2}]}],"panels":[{"id":"p","kind":"query","title":"Trend","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT day, n FROM events"},"viz":{"type":"stat","value":"n","order_by":"day","thresholds":[{"value":null,"color":"green"},{"value":"9007199254740993.01","color":"red"}],"color_mode":"background"}},{"id":"table","kind":"query","title":"Table","team_id":%d,"source_id":%d,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT ts, details FROM events"},"viz":{"type":"table","rules":[{"columns":["details"],"op":"contains","value":"retry","scope":"row","color":"red"}],"column_formats":{"ts":{"kind":"time","time":"seconds"}}}}]}`, team.ID, src.ID, team.ID, src.ID))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dash, err := core.CreateDashboard(ctx, s, log, owner, &models.CreateDashboardRequest{Name: "Trend", Panels: raw})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := models.ParseDashboardClient(new("2"), "result_frames")
	if err != nil {
		t.Fatal(err)
	}
	current, err := models.ParseDashboardClient(new("2"), "result_frames,thresholds,table_rules,column_formats")
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"thresholds", "table_rules", "column_formats"} {
		client, parseErr := models.ParseDashboardClient(new("2"), strings.Replace("result_frames,thresholds,table_rules,column_formats", missing, "", 1))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if _, writeErr := core.UpdateDashboard(ctx, s, log, dash.ID, owner, client, &models.UpdateDashboardRequest{Name: "Trend", Panels: raw, Revision: &dash.Revision}); !errors.Is(writeErr, core.ErrDashboardClientStale) {
			t.Fatalf("missing %s update: %v", missing, writeErr)
		}
		resp, readErr := core.BuildDashboardResponse(ctx, s, log, owner, client, dash)
		if readErr != nil || resp.CanEdit {
			t.Fatalf("missing %s allowed edit: %v", missing, readErr)
		}
	}
	for _, client := range []models.DashboardClient{stale, current} {
		resp, err := core.BuildDashboardResponse(ctx, s, log, owner, client, dash)
		if err != nil || resp.CanEdit != client.Supports("thresholds") {
			t.Fatalf("read %+v %v", resp, err)
		}
	}
	req := &models.UpdateDashboardRequest{Name: "Trend", Panels: raw, Revision: &dash.Revision}
	if _, err := core.UpdateDashboard(ctx, s, log, dash.ID, owner, stale, req); !errors.Is(err, core.ErrDashboardClientStale) {
		t.Fatalf("stale update: %v", err)
	}
	stored := loadDashboard(t, ctx, s, dash.ID)
	if stored.Revision != dash.Revision {
		t.Fatal("rejected write changed revision")
	}
	if _, err := core.UpdateDashboard(ctx, s, log, dash.ID, owner, current, req); err != nil {
		t.Fatal(err)
	}
}
