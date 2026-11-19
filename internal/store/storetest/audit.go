package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testAudit(t *testing.T, ctx context.Context, s store.Store) {
	t.Run("RoundTrip", func(t *testing.T) { testAuditRoundTrip(t, ctx, s) })
	t.Run("Validation", func(t *testing.T) { testAuditValidation(t, ctx, s) })
	t.Run("Filters", func(t *testing.T) { testAuditFilters(t, ctx, s) })
	t.Run("Keyset", func(t *testing.T) { testAuditKeyset(t, ctx, s) })
	t.Run("WithTx", func(t *testing.T) { testAuditWithTx(t, ctx, s) })
	// Prune deletes the oldest rows of the whole table, so it runs last.
	t.Run("PruneBounds", func(t *testing.T) { testAuditPruneBounds(t, ctx, s) })
	t.Run("Prune", func(t *testing.T) { testAuditPrune(t, ctx, s) })
}

func auditEvent(t *testing.T, action string, at time.Time) *models.AuditEvent {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return &models.AuditEvent{
		ID:         id.String(),
		OccurredAt: at,
		Action:     action,
		Outcome:    models.AuditOutcomeSuccess,
		ActorType:  models.AuditActorUser,
		AuthMethod: models.AuditAuthSession,
	}
}

func insertAudit(t *testing.T, ctx context.Context, s store.AuditStore, e *models.AuditEvent) *models.AuditEvent {
	t.Helper()
	if err := s.InsertAuditEvent(ctx, e); err != nil {
		t.Fatalf("InsertAuditEvent(%s): %v", e.Action, err)
	}
	return e
}

func listAudit(t *testing.T, ctx context.Context, s store.AuditStore, f models.AuditFilter) []models.AuditEvent {
	t.Helper()
	if f.Limit == 0 {
		f.Limit = 500
	}
	events, err := s.ListAuditEvents(ctx, f)
	if err != nil {
		t.Fatalf("ListAuditEvents(%+v): %v", f, err)
	}
	return events
}

func auditIDs(events []models.AuditEvent) []string {
	ids := make([]string, len(events))
	for i := range events {
		ids[i] = events[i].ID
	}
	return ids
}

func jsonEqual(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("stored JSON %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(g, w)
}

func testAuditRoundTrip(t *testing.T, ctx context.Context, s store.Store) {
	at := time.Date(2032, 1, 2, 3, 4, 5, 678_901_234, time.FixedZone("IST", 5*3600+1800))
	full := auditEvent(t, "source.update", at)
	full.Outcome = models.AuditOutcomeFailure
	full.Reason = "internal_error"
	full.ActorType = models.AuditActorServiceAccount
	full.ActorID = new(models.UserID(41))
	full.ActorLabel = "ci-bot"
	full.AuthMethod = models.AuditAuthOAuthAPI
	full.CredentialID = "grant-7"
	full.ClientID = "logchef-cli"
	full.TargetType = "source"
	full.TargetID = "7"
	full.TargetLabel = "logs.app"
	full.TeamID = new(models.TeamID(3))
	full.SourceID = new(models.SourceID(7))
	full.IP = "10.0.0.1"
	full.UserAgent = "logchef-cli/0.4.0"
	full.HTTPMethod = "PUT"
	full.Route = "/api/v1/admin/sources/:sourceID"
	full.Status = 503
	full.Changes = json.RawMessage(`{"name": {"from": "a", "to": "b"}}`)
	full.Detail = json.RawMessage(`{"unstaged": true}`)
	insertAudit(t, ctx, s, full)

	wantAt := time.Date(2032, 1, 1, 21, 34, 5, 678_000_000, time.UTC)
	if !full.OccurredAt.Equal(wantAt) || full.OccurredAt.Location() != time.UTC {
		t.Errorf("InsertAuditEvent left OccurredAt = %v, want %v", full.OccurredAt, wantAt)
	}

	bare := insertAudit(t, ctx, s, auditEvent(t, "session.logout", at.Add(time.Second)))

	got := listAudit(t, ctx, s, models.AuditFilter{From: at.Add(-time.Hour), To: at.Add(time.Hour), Ascending: true})
	if len(got) != 2 {
		t.Fatalf("listed %d events, want 2", len(got))
	}
	g := got[0]
	if !jsonEqual(t, g.Changes, string(full.Changes)) || !jsonEqual(t, g.Detail, string(full.Detail)) {
		t.Errorf("changes/detail = %s / %s", g.Changes, g.Detail)
	}
	g.Changes, g.Detail = full.Changes, full.Detail
	if !reflect.DeepEqual(g, *full) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", g, *full)
	}

	b := got[1]
	if b.ID != bare.ID || b.ActorID != nil || b.TeamID != nil || b.SourceID != nil || b.Status != 0 {
		t.Errorf("bare event = %+v", b)
	}
	if !jsonEqual(t, b.Changes, `{}`) || !jsonEqual(t, b.Detail, `{}`) {
		t.Errorf("empty changes/detail stored as %s / %s, want {}", b.Changes, b.Detail)
	}
}

func testAuditValidation(t *testing.T, ctx context.Context, s store.Store) {
	at := time.Date(2033, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]func(e *models.AuditEvent){
		"no id":            func(e *models.AuditEvent) { e.ID = "" },
		"no time":          func(e *models.AuditEvent) { e.OccurredAt = time.Time{} },
		"no action":        func(e *models.AuditEvent) { e.Action = "" },
		"no outcome":       func(e *models.AuditEvent) { e.Outcome = "" },
		"no actor type":    func(e *models.AuditEvent) { e.ActorType = "" },
		"no auth method":   func(e *models.AuditEvent) { e.AuthMethod = "" },
		"status too large": func(e *models.AuditEvent) { e.Status = 1 << 40 },
		"changes array":    func(e *models.AuditEvent) { e.Changes = json.RawMessage(`[1]`) },
		"detail malformed": func(e *models.AuditEvent) { e.Detail = json.RawMessage(`{"a":`) },
	}
	for name, mutate := range cases {
		e := auditEvent(t, "team.update", at)
		mutate(e)
		if err := s.InsertAuditEvent(ctx, e); err == nil {
			t.Errorf("%s: InsertAuditEvent accepted %+v", name, e)
		}
	}
	if n, err := s.CountAuditEvents(ctx, models.AuditFilter{From: at, To: at.Add(time.Hour)}, 10); err != nil || n != 0 {
		t.Errorf("rejected events were stored: %d / %v", n, err)
	}
	if _, err := s.ListAuditEvents(ctx, models.AuditFilter{}); err == nil {
		t.Error("ListAuditEvents accepted limit 0")
	}
	if _, err := s.ListAuditEvents(ctx, models.AuditFilter{Limit: 1, Cursor: &models.AuditCursor{OccurredAt: at}}); err == nil {
		t.Error("ListAuditEvents accepted a cursor without an id")
	}
	if _, err := s.CountAuditEvents(ctx, models.AuditFilter{}, 0); err == nil {
		t.Error("CountAuditEvents accepted max 0")
	}
}

func testAuditFilters(t *testing.T, ctx context.Context, s store.Store) {
	base := time.Date(2034, 5, 6, 7, 0, 0, 0, time.UTC)
	mk := func(minute int, action string, edit func(e *models.AuditEvent)) string {
		e := auditEvent(t, action, base.Add(time.Duration(minute)*time.Minute))
		edit(e)
		return insertAudit(t, ctx, s, e).ID
	}
	r1 := mk(1, "source.update", func(e *models.AuditEvent) {
		e.ActorID = new(models.UserID(1))
		e.TargetType, e.TargetID = "source", "7"
		e.SourceID = new(models.SourceID(7))
	})
	r2 := mk(2, "source.create", func(e *models.AuditEvent) {
		e.ActorID = new(models.UserID(2))
		e.AuthMethod, e.CredentialID = models.AuditAuthAPIToken, "tok-9"
		e.TargetType, e.TargetID = "source", "8"
		e.SourceID = new(models.SourceID(8))
		e.Outcome = models.AuditOutcomeFailure
	})
	r3 := mk(3, "team.update", func(e *models.AuditEvent) {
		e.ActorID = new(models.UserID(1))
		e.AuthMethod, e.CredentialID = models.AuditAuthOAuthMCP, "grant-3"
		e.TargetType, e.TargetID = "team", "4"
		e.TeamID = new(models.TeamID(4))
	})
	r4 := mk(4, "team_member.add", func(e *models.AuditEvent) {
		e.ActorType = models.AuditActorSystem
		e.AuthMethod, e.CredentialID = models.AuditAuthProvisioning, "run-1"
		e.TargetType, e.TargetID = "team_member", "4:5"
		e.TeamID = new(models.TeamID(4))
	})
	// A prefix is literal: the underscore in "team_member." matches only itself.
	r5 := mk(5, "teamxmember.add", func(*models.AuditEvent) {})

	window := models.AuditFilter{From: base, To: base.Add(time.Hour), Ascending: true}
	cases := []struct {
		name string
		edit func(f *models.AuditFilter)
		want []string
	}{
		{"none", func(*models.AuditFilter) {}, []string{r1, r2, r3, r4, r5}},
		{"from inclusive", func(f *models.AuditFilter) { f.From = base.Add(2 * time.Minute) }, []string{r2, r3, r4, r5}},
		{"to exclusive", func(f *models.AuditFilter) { f.To = base.Add(3 * time.Minute) }, []string{r1, r2}},
		{"action", func(f *models.AuditFilter) { f.Action = "source.update" }, []string{r1}},
		{"action prefix", func(f *models.AuditFilter) { f.ActionPrefix = "team." }, []string{r3}},
		{"action prefix with underscore", func(f *models.AuditFilter) { f.ActionPrefix = "team_member." }, []string{r4}},
		{"actor", func(f *models.AuditFilter) { f.ActorID = new(models.UserID(1)) }, []string{r1, r3}},
		{"auth method", func(f *models.AuditFilter) { f.AuthMethod = models.AuditAuthAPIToken }, []string{r2}},
		{"credential", func(f *models.AuditFilter) { f.CredentialID = "grant-3" }, []string{r3}},
		{"target type", func(f *models.AuditFilter) { f.TargetType = "source" }, []string{r1, r2}},
		{"target", func(f *models.AuditFilter) { f.TargetType, f.TargetID = "source", "8" }, []string{r2}},
		{"team", func(f *models.AuditFilter) { f.TeamID = new(models.TeamID(4)) }, []string{r3, r4}},
		{"source", func(f *models.AuditFilter) { f.SourceID = new(models.SourceID(7)) }, []string{r1}},
		{"outcome", func(f *models.AuditFilter) { f.Outcome = models.AuditOutcomeFailure }, []string{r2}},
		{"descending", func(f *models.AuditFilter) { f.Ascending = false }, []string{r5, r4, r3, r2, r1}},
		{"limit", func(f *models.AuditFilter) { f.Limit = 2 }, []string{r1, r2}},
	}
	for _, tc := range cases {
		f := window
		tc.edit(&f)
		if got := auditIDs(listAudit(t, ctx, s, f)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: listed %v, want %v", tc.name, got, tc.want)
		}
		if tc.name == "limit" || tc.name == "descending" {
			continue
		}
		if n, err := s.CountAuditEvents(ctx, f, 100); err != nil || n != len(tc.want) {
			t.Errorf("%s: counted %d / %v, want %d", tc.name, n, err, len(tc.want))
		}
	}
	// Bounds finer than the stored millisecond must not move: an event at
	// 07:02:00.000 is after 07:01:59.9995 and before 07:02:00.0005.
	r2At := base.Add(2 * time.Minute)
	subMS := []struct {
		name string
		edit func(f *models.AuditFilter)
		want []string
	}{
		{"from just before", func(f *models.AuditFilter) { f.From = r2At.Add(-500 * time.Microsecond) }, []string{r2, r3, r4, r5}},
		{"from just after", func(f *models.AuditFilter) { f.From = r2At.Add(500 * time.Microsecond) }, []string{r3, r4, r5}},
		{"to just after", func(f *models.AuditFilter) { f.To = r2At.Add(500 * time.Microsecond) }, []string{r1, r2}},
		{"to just before", func(f *models.AuditFilter) { f.To = r2At.Add(-500 * time.Microsecond) }, []string{r1}},
	}
	for _, tc := range subMS {
		for _, ascending := range []bool{true, false} {
			f := window
			f.Ascending = ascending
			tc.edit(&f)
			want := slices.Clone(tc.want)
			if !ascending {
				slices.Reverse(want)
			}
			if got := auditIDs(listAudit(t, ctx, s, f)); !slices.Equal(got, want) {
				t.Errorf("%s (ascending=%v): listed %v, want %v", tc.name, ascending, got, want)
			}
		}
		f := window
		tc.edit(&f)
		if n, err := s.CountAuditEvents(ctx, f, 100); err != nil || n != len(tc.want) {
			t.Errorf("%s: counted %d / %v, want %d", tc.name, n, err, len(tc.want))
		}
	}

	if n, err := s.CountAuditEvents(ctx, window, 3); err != nil || n != 3 {
		t.Errorf("count stopped at %d / %v, want max 3", n, err)
	}
}

// testAuditKeyset inserts events with IDs from uuid.NewV7 in a loop, fifty
// per millisecond, so most of the order comes from the ID tie-break.
func testAuditKeyset(t *testing.T, ctx context.Context, s store.Store) {
	base := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 1000
	inserted := make([]string, 0, total)
	for i := range total {
		e := auditEvent(t, "dashboard.update", base.Add(time.Duration(i/50)*time.Millisecond))
		e.CredentialID = "keyset"
		inserted = append(inserted, insertAudit(t, ctx, s, e).ID)
	}
	reversed := slices.Clone(inserted)
	slices.Reverse(reversed)

	for _, ascending := range []bool{true, false} {
		f := models.AuditFilter{CredentialID: "keyset", Ascending: ascending, Limit: 7}
		var walked []string
		for pages := 0; ; pages++ {
			if pages > total {
				t.Fatal("cursor walk does not end")
			}
			page := listAudit(t, ctx, s, f)
			if len(page) == 0 {
				break
			}
			walked = append(walked, auditIDs(page)...)
			last := page[len(page)-1]
			f.Cursor = &models.AuditCursor{OccurredAt: last.OccurredAt, ID: last.ID}
		}
		want := inserted
		if !ascending {
			want = reversed
		}
		if !slices.Equal(walked, want) {
			t.Errorf("ascending=%v: walk returned %d events, want each of %d once in insertion order", ascending, len(walked), len(want))
		}
	}
}

func testAuditWithTx(t *testing.T, ctx context.Context, s store.Store) {
	at := time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)
	errAbort := errors.New("abort")
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		e := auditEvent(t, "api_token.create", at)
		e.CredentialID = "rolled-back"
		if err := tx.InsertAuditEvent(ctx, e); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("WithTx = %v, want abort", err)
	}
	if n, err := s.CountAuditEvents(ctx, models.AuditFilter{CredentialID: "rolled-back"}, 10); err != nil || n != 0 {
		t.Errorf("rolled-back insert left %d rows / %v", n, err)
	}

	err = s.WithTx(ctx, func(tx store.StoreOps) error {
		e := auditEvent(t, "api_token.create", at)
		e.CredentialID = "committed"
		return tx.InsertAuditEvent(ctx, e)
	})
	if err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}
	if n, err := s.CountAuditEvents(ctx, models.AuditFilter{CredentialID: "committed"}, 10); err != nil || n != 1 {
		t.Errorf("committed insert left %d rows / %v, want 1", n, err)
	}
}

// testAuditPruneBounds checks that a cutoff finer than the stored millisecond
// does not move: it uses events older than every other event in the table.
func testAuditPruneBounds(t *testing.T, ctx context.Context, s store.Store) {
	old := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		insertAudit(t, ctx, s, auditEvent(t, "setting.update", old.Add(time.Duration(i)*time.Minute)))
	}
	newest := old.Add(2 * time.Minute)
	if n, err := s.PruneAuditEvents(ctx, newest.Add(-500*time.Microsecond), 0, 10); err != nil || n != 2 {
		t.Fatalf("prune just before the newest event deleted %d / %v, want 2", n, err)
	}
	if n, err := s.PruneAuditEvents(ctx, newest.Add(500*time.Microsecond), 0, 10); err != nil || n != 1 {
		t.Fatalf("prune just after the newest event deleted %d / %v, want 1", n, err)
	}
}

func testAuditPrune(t *testing.T, ctx context.Context, s store.Store) {
	if _, err := s.PruneAuditEvents(ctx, time.Time{}, 0, 0); err == nil {
		t.Error("PruneAuditEvents accepted batch 0")
	}
	if n, err := s.PruneAuditEvents(ctx, time.Time{}, 0, 10); err != nil || n != 0 {
		t.Errorf("prune with both rules off deleted %d / %v", n, err)
	}

	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	var oldIDs []string
	for i := range 3 {
		oldIDs = append(oldIDs, insertAudit(t, ctx, s, auditEvent(t, "setting.update", old.Add(time.Duration(i)*time.Minute))).ID)
	}
	all := models.AuditFilter{}
	total, err := s.CountAuditEvents(ctx, all, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}

	before := old.Add(time.Hour)
	for _, want := range []int{2, 1, 0} {
		if n, err := s.PruneAuditEvents(ctx, before, 0, 2); err != nil || n != want {
			t.Fatalf("prune by age deleted %d / %v, want %d", n, err, want)
		}
		left := auditIDs(listAudit(t, ctx, s, models.AuditFilter{To: before, Ascending: true}))
		if want == 2 && !slices.Equal(left, oldIDs[2:]) {
			t.Errorf("first batch left %v, want the newest old event %v", left, oldIDs[2:])
		}
	}
	total -= 3

	if n, err := s.PruneAuditEvents(ctx, time.Time{}, total, 10); err != nil || n != 0 {
		t.Errorf("prune to the current count deleted %d / %v", n, err)
	}
	oldest := listAudit(t, ctx, s, models.AuditFilter{Ascending: true, Limit: 3})
	newest := listAudit(t, ctx, s, models.AuditFilter{Limit: 1})
	if n, err := s.PruneAuditEvents(ctx, time.Time{}, total-3, 2); err != nil || n != 2 {
		t.Fatalf("prune by count deleted %d / %v, want the batch of 2", n, err)
	}
	if n, err := s.PruneAuditEvents(ctx, time.Time{}, total-3, 2); err != nil || n != 1 {
		t.Fatalf("second prune by count deleted %d / %v, want 1", n, err)
	}
	left := listAudit(t, ctx, s, models.AuditFilter{Ascending: true, Limit: 1})
	if n, _ := s.CountAuditEvents(ctx, all, 1_000_000); n != total-3 {
		t.Errorf("%d rows remain, want %d", n, total-3)
	}
	if slices.Contains(auditIDs(oldest), left[0].ID) {
		t.Errorf("prune by count kept one of the three oldest events")
	}
	if got := listAudit(t, ctx, s, models.AuditFilter{Limit: 1}); got[0].ID != newest[0].ID {
		t.Errorf("prune by count removed the newest event")
	}
}
