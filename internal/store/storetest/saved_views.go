package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

type savedViewFixture struct {
	src, otherSrc   *models.Source
	alice, bob      *models.User
	team, otherTeam *models.Team
}

func seedSavedViews(t *testing.T, ctx context.Context, s store.StoreOps, tag string) savedViewFixture {
	t.Helper()
	f := savedViewFixture{
		src:      mkSource(t, ctx, s, "sv_"+tag),
		otherSrc: mkSource(t, ctx, s, "sv_other_"+tag),
		alice:    mkUser(t, ctx, s, "sv-alice-"+tag+"@test.dev"),
		bob:      mkUser(t, ctx, s, "sv-bob-"+tag+"@test.dev"),
	}
	for name, team := range map[string]**models.Team{"sv-team-" + tag: &f.team, "sv-other-team-" + tag: &f.otherTeam} {
		tm := &models.Team{Name: name}
		if err := s.CreateTeam(ctx, tm); err != nil {
			t.Fatalf("CreateTeam(%s): %v", name, err)
		}
		*team = tm
	}
	return f
}

func personalView(f savedViewFixture, owner *models.User, name string) *models.SavedView {
	uid := owner.ID
	return &models.SavedView{SourceID: f.src.ID, Scope: models.SavedViewScopePersonal, UserID: &uid, Name: name, Definition: models.SavedViewDefinition{Version: 1}, CreatedBy: &uid}
}

func teamView(f savedViewFixture, team *models.Team, name string) *models.SavedView {
	tid := team.ID
	return &models.SavedView{SourceID: f.src.ID, Scope: models.SavedViewScopeTeam, TeamID: &tid, Name: name, Definition: models.SavedViewDefinition{Version: 1}, CreatedBy: &f.alice.ID}
}

func sourceView(f savedViewFixture, name string) *models.SavedView {
	return &models.SavedView{SourceID: f.src.ID, Scope: models.SavedViewScopeSource, Name: name, Definition: models.SavedViewDefinition{Version: 1}}
}

func mkView(t *testing.T, ctx context.Context, s store.StoreOps, v *models.SavedView) *models.SavedView {
	t.Helper()
	if err := s.CreateSavedView(ctx, v); err != nil {
		t.Fatalf("CreateSavedView(%s %q): %v", v.Scope, v.Name, err)
	}
	return v
}

func viewNames(views []*models.SavedView) []string {
	names := make([]string, len(views))
	for i, v := range views {
		names[i] = string(v.Scope) + ":" + v.Name
	}
	return names
}

func defaultIDs(t *testing.T, ctx context.Context, s store.StoreOps, f savedViewFixture, user *models.User, team *models.Team) []models.SavedViewID {
	t.Helper()
	views, err := s.ListSavedViews(ctx, f.src.ID, user.ID, team.ID)
	if err != nil {
		t.Fatalf("ListSavedViews: %v", err)
	}
	var ids []models.SavedViewID
	for _, v := range views {
		if v.IsDefault {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

func testSavedViews(t *testing.T, ctx context.Context, s store.Store) {
	t.Run("RoundTrip", func(t *testing.T) { testSavedViewRoundTrip(t, ctx, s) })
	t.Run("ManagedRoundTrip", func(t *testing.T) { testSavedViewManagedRoundTrip(t, ctx, s) })
	t.Run("ScopeCheck", func(t *testing.T) { testSavedViewScopeCheck(t, ctx, s) })
	t.Run("UniqueNames", func(t *testing.T) { testSavedViewUniqueNames(t, ctx, s) })
	t.Run("OneDefaultPerScope", func(t *testing.T) { testSavedViewOneDefault(t, ctx, s) })
	t.Run("UpdateRevision", func(t *testing.T) { testSavedViewUpdate(t, ctx, s) })
	t.Run("DefaultSwap", func(t *testing.T) { testSavedViewDefaultSwap(t, ctx, s) })
	t.Run("ManagedDefaultRollback", func(t *testing.T) { testSavedViewManagedDefaultRollback(t, ctx, s) })
	t.Run("ListAndCount", func(t *testing.T) { testSavedViewList(t, ctx, s) })
	t.Run("Delete", func(t *testing.T) { testSavedViewDelete(t, ctx, s) })
	t.Run("Cascades", func(t *testing.T) { testSavedViewCascades(t, ctx, s) })
}

func testSavedViewRoundTrip(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "roundtrip")
	width := 190
	mode := models.DisplayModeCompact
	filter, tr := `level = "error"`, "1h"
	def := models.SavedViewDefinition{
		Version:     1,
		Columns:     []models.SavedViewColumn{{Name: "timestamp", Width: &width}, {Name: "msg"}},
		Sort:        []models.SavedViewSort{{Column: "timestamp", Desc: true}},
		DisplayMode: &mode,
		Filter:      &filter,
		TimeRange:   &tr,
	}
	v := teamView(f, f.team, "errors")
	v.Description = "only errors"
	v.Definition = def
	v.IsDefault = true
	mkView(t, ctx, s, v)
	if v.ID == 0 || v.Revision != 1 || v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
		t.Fatalf("created view not filled: %+v", v)
	}
	got, err := s.GetSavedView(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetSavedView: %v", err)
	}
	if got.Scope != models.SavedViewScopeTeam || got.TeamID == nil || *got.TeamID != f.team.ID || got.UserID != nil ||
		got.SourceID != f.src.ID || got.Name != "errors" || got.Description != "only errors" || !got.IsDefault || got.Managed ||
		got.CreatedBy == nil || *got.CreatedBy != f.alice.ID {
		t.Errorf("stored view = %+v", got)
	}
	if !reflect.DeepEqual(got.Definition, def) {
		t.Errorf("definition = %+v, want %+v", got.Definition, def)
	}
}

func testSavedViewManagedRoundTrip(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "managedroundtrip")
	mkView(t, ctx, s, &models.SavedView{SourceID: f.src.ID, Scope: models.SavedViewScopeSource, Name: "managed", Definition: models.SavedViewDefinition{Version: 1}, Managed: true})
	views, err := s.ListSavedViews(ctx, f.src.ID, f.alice.ID, f.team.ID)
	if err != nil || len(views) != 1 || !views[0].Managed || views[0].CreatedBy != nil {
		t.Errorf("managed source view = %v / %v", views, err)
	}
	if _, err := s.GetSavedView(ctx, 1<<30); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetSavedView(missing) = %v, want ErrNotFound", err)
	}
}

// The CHECK constraint ties the scope to its owner columns on both stores
// (RFC 0007 unverified claim 1).
func testSavedViewScopeCheck(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "check")
	uid, tid := f.alice.ID, f.team.ID
	cases := []struct {
		name  string
		scope models.SavedViewScope
		user  *models.UserID
		team  *models.TeamID
	}{
		{"personal without user", models.SavedViewScopePersonal, nil, nil},
		{"personal with team", models.SavedViewScopePersonal, &uid, &tid},
		{"team without team", models.SavedViewScopeTeam, nil, nil},
		{"team with user", models.SavedViewScopeTeam, &uid, &tid},
		{"source with user", models.SavedViewScopeSource, &uid, nil},
		{"source with team", models.SavedViewScopeSource, nil, &tid},
		{"unknown scope", models.SavedViewScope("org"), nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &models.SavedView{SourceID: f.src.ID, Scope: tc.scope, UserID: tc.user, TeamID: tc.team, Name: tc.name, Definition: models.SavedViewDefinition{Version: 1}}
			if err := s.CreateSavedView(ctx, v); err == nil {
				t.Fatalf("CreateSavedView accepted %+v", v)
			}
		})
	}
}

func testSavedViewUniqueNames(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "names")
	mkView(t, ctx, s, personalView(f, f.alice, "access"))
	mkView(t, ctx, s, teamView(f, f.team, "access"))
	mkView(t, ctx, s, sourceView(f, "access"))
	// The same name in another owner of the same scope, or on another source.
	mkView(t, ctx, s, personalView(f, f.bob, "access"))
	mkView(t, ctx, s, teamView(f, f.otherTeam, "access"))
	other := sourceView(f, "access")
	other.SourceID = f.otherSrc.ID
	mkView(t, ctx, s, other)

	for _, v := range []*models.SavedView{personalView(f, f.alice, "access"), teamView(f, f.team, "access"), sourceView(f, "access")} {
		if err := s.CreateSavedView(ctx, v); !errors.Is(err, models.ErrConflict) {
			t.Errorf("duplicate %s name: err = %v, want ErrConflict", v.Scope, err)
		}
	}
	if err := s.CreateSavedView(ctx, personalView(f, f.alice, "Access")); err != nil {
		t.Errorf("a name that differs only in case is another name: %v", err)
	}
}

// A second default in one scope fails on both stores (RFC 0007 unverified
// claim 1); defaults of different scopes and owners coexist.
func testSavedViewOneDefault(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "onedefault")
	for _, mk := range []func(name string) *models.SavedView{
		func(name string) *models.SavedView { return personalView(f, f.alice, name) },
		func(name string) *models.SavedView { return teamView(f, f.team, name) },
		func(name string) *models.SavedView { return sourceView(f, name) },
	} {
		first := mk("first")
		first.IsDefault = true
		mkView(t, ctx, s, first)
		second := mk("second")
		second.IsDefault = true
		if err := s.CreateSavedView(ctx, second); !errors.Is(err, models.ErrConflict) {
			t.Errorf("second %s default: err = %v, want ErrConflict", first.Scope, err)
		}
	}
	bobs := personalView(f, f.bob, "bob")
	bobs.IsDefault = true
	mkView(t, ctx, s, bobs)
	others := teamView(f, f.otherTeam, "other")
	others.IsDefault = true
	mkView(t, ctx, s, others)
	elsewhere := sourceView(f, "elsewhere")
	elsewhere.SourceID = f.otherSrc.ID
	elsewhere.IsDefault = true
	mkView(t, ctx, s, elsewhere)
}

func testSavedViewUpdate(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "update")
	v := mkView(t, ctx, s, teamView(f, f.team, "v1"))
	mkView(t, ctx, s, teamView(f, f.team, "taken"))
	tr := "15m"

	stale := *v
	edit := *v
	edit.Name, edit.Description = "v2", "second"
	edit.Definition = models.SavedViewDefinition{Version: 1, TimeRange: &tr}
	if err := s.UpdateSavedView(ctx, &edit); err != nil {
		t.Fatalf("UpdateSavedView: %v", err)
	}
	if edit.Revision != 2 || edit.Name != "v2" || edit.Definition.TimeRange == nil || *edit.Definition.TimeRange != "15m" {
		t.Errorf("updated view = %+v", edit)
	}

	stale.Name = "v3"
	if err := s.UpdateSavedView(ctx, &stale); !errors.Is(err, models.ErrConflict) {
		t.Fatalf("stale revision: err = %v, want ErrConflict", err)
	}
	got, err := s.GetSavedView(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetSavedView: %v", err)
	}
	if got.Revision != 2 || got.Name != "v2" || got.Description != "second" {
		t.Errorf("stale update changed the row: %+v", got)
	}

	rename := *got
	rename.Name = "taken"
	if err := s.UpdateSavedView(ctx, &rename); !errors.Is(err, models.ErrConflict) {
		t.Errorf("rename to a taken name: err = %v, want ErrConflict", err)
	}
	missing := *got
	missing.ID = 1 << 30
	if err := s.UpdateSavedView(ctx, &missing); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("update of a missing view: err = %v, want ErrNotFound", err)
	}
}

func testSavedViewDefaultSwap(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "swap")
	a := mkView(t, ctx, s, teamView(f, f.team, "a"))
	b := mkView(t, ctx, s, teamView(f, f.team, "b"))
	p := personalView(f, f.alice, "mine")
	p.IsDefault = true
	mkView(t, ctx, s, p)
	other := teamView(f, f.otherTeam, "other")
	other.IsDefault = true
	mkView(t, ctx, s, other)

	cleared, err := s.SetSavedViewDefault(ctx, a.ID)
	if err != nil || cleared != nil {
		t.Fatalf("first default: cleared %+v / %v, want nil / nil", cleared, err)
	}
	cleared, err = s.SetSavedViewDefault(ctx, b.ID)
	if err != nil || cleared == nil || cleared.ID != a.ID || cleared.Managed {
		t.Fatalf("swap: cleared %+v / %v, want view %d unmanaged", cleared, err, a.ID)
	}
	if got := defaultIDs(t, ctx, s, f, f.alice, f.team); !reflect.DeepEqual(got, []models.SavedViewID{p.ID, b.ID}) {
		t.Errorf("defaults after swap = %v, want personal %d and team %d", got, p.ID, b.ID)
	}
	if got, _ := s.GetSavedView(ctx, other.ID); got == nil || !got.IsDefault {
		t.Errorf("another team's default was cleared: %+v", got)
	}
	cleared, err = s.SetSavedViewDefault(ctx, b.ID)
	if err != nil || cleared != nil {
		t.Errorf("setting the current default again: cleared %+v / %v, want nil / nil", cleared, err)
	}

	if err := s.ClearSavedViewDefault(ctx, b.ID); err != nil {
		t.Fatalf("ClearSavedViewDefault: %v", err)
	}
	if got := defaultIDs(t, ctx, s, f, f.alice, f.team); !reflect.DeepEqual(got, []models.SavedViewID{p.ID}) {
		t.Errorf("defaults after clear = %v, want personal %d", got, p.ID)
	}
	if _, err := s.SetSavedViewDefault(ctx, 1<<30); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("SetSavedViewDefault(missing) = %v, want ErrNotFound", err)
	}
	if err := s.ClearSavedViewDefault(ctx, 1<<30); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("ClearSavedViewDefault(missing) = %v, want ErrNotFound", err)
	}
}

// A caller that refuses to clear a managed default rolls the swap back.
func testSavedViewManagedDefaultRollback(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "rollback")
	m := sourceView(f, "managed")
	m.Managed, m.IsDefault = true, true
	mkView(t, ctx, s, m)
	u := mkView(t, ctx, s, sourceView(f, "unmanaged"))
	errRefused := errors.New("refused")
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		cleared, err := tx.SetSavedViewDefault(ctx, u.ID)
		if err != nil {
			return err
		}
		if cleared == nil || cleared.ID != m.ID || !cleared.Managed {
			t.Errorf("cleared = %+v, want managed view %d", cleared, m.ID)
		}
		return errRefused
	})
	if !errors.Is(err, errRefused) {
		t.Fatalf("WithTx = %v, want the refusal", err)
	}
	if got, _ := s.GetSavedView(ctx, m.ID); got == nil || !got.IsDefault {
		t.Errorf("rolled-back swap cleared the managed default: %+v", got)
	}
	if got, _ := s.GetSavedView(ctx, u.ID); got == nil || got.IsDefault {
		t.Errorf("rolled-back swap set the default: %+v", got)
	}
}

func testSavedViewList(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "list")
	mkView(t, ctx, s, sourceView(f, "zeta"))
	mkView(t, ctx, s, sourceView(f, "Alpha"))
	mkView(t, ctx, s, teamView(f, f.team, "team b"))
	mkView(t, ctx, s, teamView(f, f.team, "team a"))
	mkView(t, ctx, s, personalView(f, f.alice, "mine"))
	// Views the caller must not see.
	mkView(t, ctx, s, personalView(f, f.bob, "bob's"))
	mkView(t, ctx, s, teamView(f, f.otherTeam, "other team"))
	elsewhere := sourceView(f, "elsewhere")
	elsewhere.SourceID = f.otherSrc.ID
	mkView(t, ctx, s, elsewhere)
	m := sourceView(f, "managed")
	m.Managed = true
	mkView(t, ctx, s, m)

	views, err := s.ListSavedViews(ctx, f.src.ID, f.alice.ID, f.team.ID)
	if err != nil {
		t.Fatalf("ListSavedViews: %v", err)
	}
	want := []string{"personal:mine", "team:team a", "team:team b", "source:Alpha", "source:managed", "source:zeta"}
	if got := viewNames(views); !reflect.DeepEqual(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	uid, tid := f.alice.ID, f.team.ID
	for _, tc := range []struct {
		key  models.SavedViewScopeKey
		want int
	}{
		{models.SavedViewScopeKey{SourceID: f.src.ID, Scope: models.SavedViewScopePersonal, UserID: &uid}, 1},
		{models.SavedViewScopeKey{SourceID: f.src.ID, Scope: models.SavedViewScopeTeam, TeamID: &tid}, 2},
		{models.SavedViewScopeKey{SourceID: f.src.ID, Scope: models.SavedViewScopeSource}, 3},
		{models.SavedViewScopeKey{SourceID: f.otherSrc.ID, Scope: models.SavedViewScopeSource}, 1},
	} {
		n, err := s.CountSavedViewsInScope(ctx, tc.key)
		if err != nil || n != tc.want {
			t.Errorf("CountSavedViewsInScope(%s) = %d / %v, want %d", tc.key.Scope, n, err, tc.want)
		}
	}

	managed, err := s.ListManagedSavedViews(ctx)
	if err != nil {
		t.Fatalf("ListManagedSavedViews: %v", err)
	}
	found := false
	for _, v := range managed {
		if !v.Managed {
			t.Errorf("unmanaged view %d in the managed list", v.ID)
		}
		found = found || v.ID == m.ID
	}
	if !found {
		t.Errorf("managed view %d missing from %v", m.ID, viewNames(managed))
	}
}

func testSavedViewDelete(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "delete")
	v := mkView(t, ctx, s, sourceView(f, "gone"))
	if err := s.DeleteSavedView(ctx, v.ID); err != nil {
		t.Fatalf("DeleteSavedView: %v", err)
	}
	if _, err := s.GetSavedView(ctx, v.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetSavedView after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSavedView(ctx, v.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func testSavedViewCascades(t *testing.T, ctx context.Context, s store.Store) {
	f := seedSavedViews(t, ctx, s, "cascade")
	alicePersonal := mkView(t, ctx, s, personalView(f, f.alice, "alice"))
	bobPersonal := mkView(t, ctx, s, personalView(f, f.bob, "bob"))
	aliceAuthored := mkView(t, ctx, s, teamView(f, f.team, "by alice"))
	otherTeamView := mkView(t, ctx, s, teamView(f, f.otherTeam, "other"))
	srcView := mkView(t, ctx, s, sourceView(f, "source"))
	elsewhere := sourceView(f, "elsewhere")
	elsewhere.SourceID = f.otherSrc.ID
	mkView(t, ctx, s, elsewhere)

	exists := func(id models.SavedViewID) bool {
		t.Helper()
		_, err := s.GetSavedView(ctx, id)
		if err != nil && !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("GetSavedView(%d): %v", id, err)
		}
		return err == nil
	}

	if err := s.DeleteUser(ctx, f.alice.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if exists(alicePersonal.ID) || !exists(bobPersonal.ID) {
		t.Errorf("user delete: alice's view exists %v, bob's exists %v; want false, true", exists(alicePersonal.ID), exists(bobPersonal.ID))
	}
	if got, err := s.GetSavedView(ctx, aliceAuthored.ID); err != nil || got.CreatedBy != nil {
		t.Errorf("view authored by the deleted user = %+v / %v, want kept with no creator", got, err)
	}

	if err := s.DeleteTeam(ctx, f.team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	if exists(aliceAuthored.ID) || !exists(otherTeamView.ID) || !exists(srcView.ID) {
		t.Errorf("team delete: team view exists %v, other team's %v, source view %v; want false, true, true",
			exists(aliceAuthored.ID), exists(otherTeamView.ID), exists(srcView.ID))
	}

	if err := s.DeleteSource(ctx, f.src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if exists(srcView.ID) || exists(bobPersonal.ID) || exists(otherTeamView.ID) || !exists(elsewhere.ID) {
		t.Errorf("source delete left views of the source, or removed another source's view")
	}
}

// RunSavedViewDefaultRace swaps two defaults of one scope at once from two
// store instances (RFC 0007 unverified claim 2). The second swap must wait
// for the first, then either take the default (a store that serializes
// writers) or fail with models.ErrConflict (Postgres, where the partial
// unique index rejects it). It never deadlocks, and one default remains.
func RunSavedViewDefaultRace(t *testing.T, open Opener) {
	ctx := context.Background()
	a, b := open(t), open(t)
	for i, withCurrent := range []bool{true, false} {
		t.Run(fmt.Sprintf("current default %v", withCurrent), func(t *testing.T) {
			f := seedSavedViews(t, ctx, a, fmt.Sprintf("race%d", i))
			if withCurrent {
				current := teamView(f, f.team, "current")
				current.IsDefault = true
				mkView(t, ctx, a, current)
			}
			first := mkView(t, ctx, a, teamView(f, f.team, "first"))
			second := mkView(t, ctx, a, teamView(f, f.team, "second"))

			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			secondDone := make(chan error, 1)
			err := a.WithTx(ctx, func(tx store.StoreOps) error {
				if _, err := tx.SetSavedViewDefault(ctx, first.ID); err != nil {
					return err
				}
				go func() {
					_, err := b.SetSavedViewDefault(ctx, second.ID)
					secondDone <- err
				}()
				select {
				case err := <-secondDone:
					t.Errorf("second swap finished while the first was open: %v", err)
					secondDone <- err
				case <-time.After(150 * time.Millisecond):
				}
				return nil
			})
			if err != nil {
				t.Fatalf("first swap: %v", err)
			}
			var secondErr error
			select {
			case secondErr = <-secondDone:
			case <-ctx.Done():
				t.Fatal("second swap did not finish after the first committed")
			}
			want := []models.SavedViewID{second.ID}
			if secondErr != nil {
				if !errors.Is(secondErr, models.ErrConflict) {
					t.Fatalf("second swap: %v, want nil or ErrConflict", secondErr)
				}
				want = []models.SavedViewID{first.ID}
			}
			t.Logf("second swap: %v", secondErr)
			if got := defaultIDs(t, ctx, a, f, f.alice, f.team); !reflect.DeepEqual(got, want) {
				t.Errorf("defaults after the race = %v, want %v", got, want)
			}
		})
	}
}
