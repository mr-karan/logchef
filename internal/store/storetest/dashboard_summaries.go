package storetest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// SummaryStaleHooks make rows stale the way an older binary or an older
// derivation leaves them, which the store interface itself cannot. Each
// backend's conformance test implements them with raw SQL.
type SummaryStaleHooks struct {
	// ResetSummaryRevision sets summary_revision to 0, as an INSERT by an
	// older binary that knows no derived columns leaves it.
	ResetSummaryRevision func(ctx context.Context, id int) error
	// SetSummaryVersion sets summary_version, so a derivation older than the
	// current one can be simulated.
	SetSummaryVersion func(ctx context.Context, id, version int) error
}

// RunDashboardSummaries proves the derived list columns: writes keep them
// current, the stale scan pages by id, repair is conditional on the revision
// and idempotent, and the list reads them without the blob.
func RunDashboardSummaries(t *testing.T, s store.Store, hooks SummaryStaleHooks) {
	ctx := context.Background()
	owner := mkUser(t, ctx, s, "dash-summaries@test.dev")

	t.Run("WritesKeepTheSummaryCurrent", func(t *testing.T) { testSummaryWrites(t, ctx, s, owner) })
	t.Run("BackfillPagesRepairsAndStops", func(t *testing.T) { testSummaryBackfill(t, ctx, s, owner, hooks) })
	t.Run("RepairLosesToAConcurrentRevision", func(t *testing.T) { testSummaryRepairCAS(t, ctx, s, owner, hooks) })
	t.Run("ListTruncatesLongText", func(t *testing.T) { testSummaryListTruncation(t, ctx, s, owner) })
}

func summaryBlob(teamIDs ...int) json.RawMessage {
	layouts := make([]string, 0, len(teamIDs))
	panels := make([]string, 0, len(teamIDs))
	for i, team := range teamIDs {
		layouts = append(layouts, fmt.Sprintf(`{"id":"p%d","x":0,"y":%d,"w":12,"h":1}`, i, i))
		typ := "stat"
		if i%2 == 1 {
			typ = "table"
		}
		panels = append(panels, fmt.Sprintf(`{"id":"p%d","title":"t","type":%q,"team_id":%d,"source_id":1,"query":"x","query_language":"logchefql"}`, i, typ, team))
	}
	return json.RawMessage(`{"version":1,"layout":[` + strings.Join(layouts, ",") + `],"panels":[` + strings.Join(panels, ",") + `]}`)
}

func newSummaryDashboard(t *testing.T, ctx context.Context, s store.Store, owner *models.User, name string, blob json.RawMessage) *models.Dashboard {
	t.Helper()
	d := &models.Dashboard{Name: name, PanelsJSON: blob, CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, d); err != nil {
		t.Fatalf("CreateDashboard(%s): %v", name, err)
	}
	return d
}

func findSummary(t *testing.T, ctx context.Context, s store.Store, id int) *models.DashboardSummary {
	t.Helper()
	list, err := s.ListDashboardSummaries(ctx)
	if err != nil {
		t.Fatalf("ListDashboardSummaries: %v", err)
	}
	for _, item := range list {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("dashboard %d missing from the list", id)
	return nil
}

func staleIDs(t *testing.T, ctx context.Context, s store.Store) []int {
	t.Helper()
	var ids []int
	after := 0
	for {
		page, err := s.ListStaleDashboardSummaryInputs(ctx, after, 50)
		if err != nil {
			t.Fatalf("ListStaleDashboardSummaryInputs: %v", err)
		}
		if len(page) == 0 {
			return ids
		}
		for _, row := range page {
			ids = append(ids, row.ID)
		}
		after = page[len(page)-1].ID
	}
}

func testSummaryWrites(t *testing.T, ctx context.Context, s store.Store, owner *models.User) {
	d := newSummaryDashboard(t, ctx, s, owner, "Summaries", summaryBlob(3, 1, 3))
	got := findSummary(t, ctx, s, d.ID)
	if got.PanelCount != 3 || got.TabCount != 1 || !slices.Equal(got.PanelTypes, []string{"stat", "table"}) || !slices.Equal(got.PanelTeamIDs, []int{1, 3}) || got.Corrupt {
		t.Fatalf("summary after create = %+v", got)
	}
	if got.Stale() || got.SummaryRevision != 1 || got.SummaryVersion != models.DashboardSummaryVersion {
		t.Fatalf("summary after create is stale: revision %d summary_revision %d version %d", got.Revision, got.SummaryRevision, got.SummaryVersion)
	}
	if got.CreatedBy == nil || *got.CreatedBy != owner.ID || got.CreatedByEmail != owner.Email {
		t.Fatalf("summary lost its creator: %+v", got)
	}
	verifySummaryAfterUpdate(t, ctx, s, d.ID)
}

// verifySummaryAfterUpdate saves an invalid blob and expects the derived
// columns to move with it in the same statement.
func verifySummaryAfterUpdate(t *testing.T, ctx context.Context, s store.Store, id int) {
	t.Helper()
	loaded := loadDashboard(t, ctx, s, id)
	loaded.PanelsJSON = json.RawMessage(`{"version":1,"layout":[],"panels":[{"id":"p0","type":"stat","team_id":9,"source_id":1,"query_language":"logchefql"}]}`)
	if err := s.UpdateDashboard(ctx, loaded); err != nil {
		t.Fatalf("UpdateDashboard: %v", err)
	}
	got := findSummary(t, ctx, s, id)
	if !got.Corrupt || got.PanelCount != 0 || len(got.PanelTypes) != 0 || len(got.PanelTeamIDs) != 0 {
		t.Fatalf("corrupt summary after update = %+v", got)
	}
	if got.Stale() || got.Revision != 2 || got.SummaryRevision != 2 {
		t.Fatalf("summary after update is stale: %+v", got)
	}
	if ids := staleIDs(t, ctx, s); slices.Contains(ids, id) {
		t.Fatalf("a current row is listed as stale: %v", ids)
	}
}

const backfillRows = 120

// seedStaleRows creates rows and makes every one stale: even positions as an
// older binary's insert leaves them, odd positions as an older derivation.
// Every fortieth row from the eighth holds an invalid blob.
func seedStaleRows(t *testing.T, ctx context.Context, s store.Store, owner *models.User, hooks SummaryStaleHooks) []int {
	t.Helper()
	ids := make([]int, 0, backfillRows)
	for i := range backfillRows {
		blob := summaryBlob(1 + i%3)
		if i%40 == 7 {
			blob = json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"nope","team_id":1,"source_id":1,"query_language":"logchefql"}]}`)
		}
		d := newSummaryDashboard(t, ctx, s, owner, fmt.Sprintf("backfill-%03d", i), blob)
		ids = append(ids, d.ID)
	}
	for i, id := range ids {
		var err error
		if i%2 == 0 {
			err = hooks.ResetSummaryRevision(ctx, id)
		} else {
			err = hooks.SetSummaryVersion(ctx, id, 0)
		}
		if err != nil {
			t.Fatalf("make row %d stale: %v", id, err)
		}
	}
	return ids
}

// repairStaleRows runs one backfill pass over every stale row and returns
// how many rows it wrote.
func repairStaleRows(t *testing.T, ctx context.Context, s store.Store) int {
	t.Helper()
	writes, err := core.BackfillDashboardSummaries(ctx, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return writes
}

func testSummaryBackfill(t *testing.T, ctx context.Context, s store.Store, owner *models.User, hooks SummaryStaleHooks) {
	ids := seedStaleRows(t, ctx, s, owner, hooks)

	first, err := s.ListStaleDashboardSummaryInputs(ctx, 0, 50)
	if err != nil {
		t.Fatalf("ListStaleDashboardSummaryInputs: %v", err)
	}
	if len(first) != 50 || !slices.IsSorted(idsOf(first)) || first[0].ID != ids[0] {
		t.Fatalf("first stale page = %d rows starting at %d, want 50 in id order from %d", len(first), first[0].ID, ids[0])
	}
	for _, row := range first {
		if len(row.PanelsJSON) == 0 || row.Revision != 1 {
			t.Fatalf("stale input lacks its blob or revision: %+v", row)
		}
	}
	if stale := staleIDs(t, ctx, s); !slices.Equal(stale, ids) {
		t.Fatalf("stale scan = %v, want every seeded row", stale)
	}

	// An interrupted backfill: repair the first page, leave the rest for later.
	writes := 0
	for _, row := range first {
		n, err := s.RepairDashboardSummary(ctx, row.ID, row.Revision, models.SummarizeDashboardPanels(row.PanelsJSON))
		if err != nil {
			t.Fatalf("RepairDashboardSummary(%d): %v", row.ID, err)
		}
		writes += int(n)
	}
	if writes != 50 {
		t.Fatalf("first page repaired %d rows, want 50", writes)
	}
	if stale := staleIDs(t, ctx, s); !slices.Equal(stale, ids[50:]) {
		t.Fatalf("after one page the stale scan = %v, want the remaining %d", stale, backfillRows-50)
	}

	// The next start finishes the job, and a start after that writes nothing.
	if writes := repairStaleRows(t, ctx, s); writes != backfillRows-50 {
		t.Fatalf("second backfill pass wrote %d rows, want %d", writes, backfillRows-50)
	}
	if writes := repairStaleRows(t, ctx, s); writes != 0 {
		t.Fatalf("third backfill pass wrote %d rows, want 0", writes)
	}
	verifyBackfilledRows(t, ctx, s, ids)
}

// verifyBackfilledRows checks every seeded row's derived columns after the
// backfill, including the corrupt ones, and that no revision moved.
func verifyBackfilledRows(t *testing.T, ctx context.Context, s store.Store, ids []int) {
	t.Helper()
	list, err := s.ListDashboardSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		i := slices.Index(ids, item.ID)
		if i < 0 {
			continue
		}
		if item.Stale() {
			t.Fatalf("row %d is still stale after the backfill: %+v", item.ID, item)
		}
		if item.Revision != 1 {
			t.Fatalf("repair moved the revision of %d to %d", item.ID, item.Revision)
		}
		if i%40 == 7 {
			if !item.Corrupt || item.PanelCount != 0 {
				t.Fatalf("invalid blob %d was not marked corrupt: %+v", item.ID, item)
			}
			continue
		}
		if item.Corrupt || item.PanelCount != 1 || !slices.Equal(item.PanelTeamIDs, []int{1 + i%3}) || !slices.Equal(item.PanelTypes, []string{"stat"}) {
			t.Fatalf("row %d summary = %+v, want one stat on team %d", item.ID, item, 1+i%3)
		}
	}
}

func idsOf(rows []models.DashboardSummaryInput) []int {
	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func testSummaryRepairCAS(t *testing.T, ctx context.Context, s store.Store, owner *models.User, hooks SummaryStaleHooks) {
	d := newSummaryDashboard(t, ctx, s, owner, "cas", summaryBlob(1))
	if err := hooks.ResetSummaryRevision(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	stale, err := s.ListStaleDashboardSummaryInputs(ctx, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(stale, func(r models.DashboardSummaryInput) bool { return r.ID == d.ID })
	if i < 0 {
		t.Fatalf("row %d is not stale after the reset", d.ID)
	}
	read := stale[i]

	// A save lands between the scan and the repair. It wrote its own summary,
	// so the repair for the old revision must lose and change nothing.
	loaded := loadDashboard(t, ctx, s, d.ID)
	loaded.PanelsJSON = summaryBlob(5, 6)
	if err := s.UpdateDashboard(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	n, err := s.RepairDashboardSummary(ctx, read.ID, read.Revision, models.SummarizeDashboardPanels(read.PanelsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repair for revision %d wrote %d rows after revision %d landed", read.Revision, n, loaded.Revision)
	}
	got := findSummary(t, ctx, s, d.ID)
	if got.Stale() || got.PanelCount != 2 || !slices.Equal(got.PanelTeamIDs, []int{5, 6}) {
		t.Fatalf("summary after the lost repair = %+v, want the save's summary", got)
	}

	// A repair of a row that is already current writes nothing.
	n, err = s.RepairDashboardSummary(ctx, d.ID, got.Revision, models.DashboardPanelsSummary{PanelCount: got.PanelCount, TabCount: got.TabCount, PanelTypes: got.PanelTypes, PanelTeamIDs: got.PanelTeamIDs, Corrupt: got.Corrupt})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repair of a current row wrote %d rows", n)
	}
}

func testSummaryListTruncation(t *testing.T, ctx context.Context, s store.Store, owner *models.User) {
	name := strings.Repeat("é", models.MaxDashboardNameRunes+5)
	description := strings.Repeat("日", models.MaxDashboardDescriptionRunes+5)
	d := &models.Dashboard{Name: name, Description: description, PanelsJSON: summaryBlob(1), CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, d); err != nil {
		t.Fatal(err)
	}
	got := findSummary(t, ctx, s, d.ID)
	if got.Name != strings.Repeat("é", models.MaxDashboardNameRunes) {
		t.Fatalf("list name has %d runes, want %d", len([]rune(got.Name)), models.MaxDashboardNameRunes)
	}
	if got.Description != strings.Repeat("日", models.MaxDashboardDescriptionRunes) {
		t.Fatalf("list description has %d runes, want %d", len([]rune(got.Description)), models.MaxDashboardDescriptionRunes)
	}
	whole := loadDashboard(t, ctx, s, d.ID)
	if whole.Name != name || whole.Description != description {
		t.Fatalf("GetDashboard truncated the text")
	}
	empty := newSummaryDashboard(t, ctx, s, owner, "no description", summaryBlob(1))
	if got := findSummary(t, ctx, s, empty.ID); got.Description != "" {
		t.Fatalf("empty description listed as %q", got.Description)
	}
}
