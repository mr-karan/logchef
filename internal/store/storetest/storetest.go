// Package storetest provides a shared, backend-agnostic conformance suite for
// the store.Store contract. Run(t, s) is executed against both backends (a temp
// SQLite file and a Postgres instance) to prove they behave identically.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Run exercises the full store.Store contract against s. s must be backed by an
// empty, freshly-migrated database; Run creates and cleans up its own data.
func Run(t *testing.T, s store.Store) {
	ctx := context.Background()

	t.Run("Users", func(t *testing.T) { testUsers(t, ctx, s) })
	t.Run("UserPasswordHash", func(t *testing.T) { testUserPasswordHash(t, ctx, s) })
	t.Run("TeamsMembersSources", func(t *testing.T) { testTeams(t, ctx, s) })
	t.Run("Sessions", func(t *testing.T) { testSessions(t, ctx, s) })
	t.Run("Settings", func(t *testing.T) { testSettings(t, ctx, s) })
	t.Run("SavedQueriesCollections", func(t *testing.T) { testSavedQueriesCollections(t, ctx, s) })
	t.Run("Dashboards", func(t *testing.T) { testDashboards(t, ctx, s) })
	t.Run("Alerts", func(t *testing.T) { testAlerts(t, ctx, s) })
	t.Run("UserPreferences", func(t *testing.T) { testUserPreferences(t, ctx, s) })
	t.Run("QuerySharesExportJobsNotFound", func(t *testing.T) { testQuerySharesExportJobsNotFound(t, ctx, s) })
	t.Run("Provisioning", func(t *testing.T) { testProvisioning(t, ctx, s) })
	t.Run("WithTxCommit", func(t *testing.T) { testWithTxCommit(t, ctx, s) })
	t.Run("WithTxRollback", func(t *testing.T) { testWithTxRollback(t, ctx, s) })
	t.Run("WithTxNoNesting", func(t *testing.T) { testWithTxNoNesting(t, ctx, s) })
}

// --- helpers ---

func testUserPasswordHash(t *testing.T, ctx context.Context, s store.Store) {
	u := mkUser(t, ctx, s, "local-auth@test.dev")
	if u.PasswordHash != "" {
		t.Fatalf("new user has non-empty password hash")
	}
	if err := s.SetUserPasswordHash(ctx, u.ID, "$2a$10$fakehashfortesting0000000000000000000000000000000000"); err != nil {
		t.Fatalf("SetUserPasswordHash: %v", err)
	}
	got, err := s.GetUserByEmail(ctx, u.Email)
	if err != nil || got.PasswordHash == "" {
		t.Fatalf("password hash did not round-trip: %v / %+v", err, got)
	}
	if err := s.SetUserPasswordHash(ctx, u.ID, ""); err != nil {
		t.Fatalf("clear password hash: %v", err)
	}
	if got, err = s.GetUserByEmail(ctx, u.Email); err != nil || got.PasswordHash != "" {
		t.Fatalf("password hash not cleared: %v / %q", err, got.PasswordHash)
	}
}

func mkUser(t *testing.T, ctx context.Context, s store.StoreOps, email string) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: email, Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	if u.ID == 0 {
		t.Fatalf("CreateUser(%s) did not populate ID", email)
	}
	return u
}

func mkSource(t *testing.T, ctx context.Context, s store.StoreOps, table string) *models.Source {
	t.Helper()
	const db = "logs"
	src := &models.Source{
		Name:        db + "." + table,
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: db, TableName: table},
	}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return src
}

// assertSourceAccess checks both source access-check queries report want (with
// no error) on whichever backend s is — exercising the EXISTS true/false paths.
func assertSourceAccess(t *testing.T, ctx context.Context, s store.Store, teamID models.TeamID, userID models.UserID, sourceID models.SourceID, want bool) {
	t.Helper()
	if ok, err := s.TeamHasSource(ctx, teamID, sourceID); err != nil || ok != want {
		t.Errorf("TeamHasSource = %v / %v, want %v", ok, err, want)
	}
	if ok, err := s.UserHasSourceAccess(ctx, userID, sourceID); err != nil || ok != want {
		t.Errorf("UserHasSourceAccess = %v / %v, want %v", ok, err, want)
	}
}

// --- domains ---

func testUsers(t *testing.T, ctx context.Context, s store.Store) {
	u := mkUser(t, ctx, s, "alice@test.dev")

	got, err := s.GetUser(ctx, u.ID)
	if err != nil || got.Email != "alice@test.dev" {
		t.Fatalf("GetUser: %v / %+v", err, got)
	}
	if got.AccountType != models.UserAccountTypeHuman {
		t.Errorf("default account_type = %q, want human", got.AccountType)
	}

	byEmail, err := s.GetUserByEmail(ctx, "alice@test.dev")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("GetUserByEmail: %v / %+v", err, byEmail)
	}

	if _, err := s.GetUser(ctx, 999999); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetUser(missing) err = %v, want ErrNotFound", err)
	}

	// Duplicate email -> conflict.
	dup := &models.User{Email: "alice@test.dev", FullName: "dup", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := s.CreateUser(ctx, dup); !errors.Is(err, models.ErrConflict) {
		t.Errorf("duplicate email err = %v, want ErrConflict", err)
	}

	u.FullName = "Alice Updated"
	u.UpdatedAt = time.Now()
	if err := s.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got, _ := s.GetUser(ctx, u.ID); got.FullName != "Alice Updated" {
		t.Errorf("after update FullName = %q", got.FullName)
	}

	users, err := s.ListUsers(ctx)
	if err != nil || len(users) == 0 {
		t.Fatalf("ListUsers: %v / %d", err, len(users))
	}

	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.GetUser(ctx, u.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("after delete GetUser err = %v, want ErrNotFound", err)
	}
}

func testTeams(t *testing.T, ctx context.Context, s store.Store) {
	alice := mkUser(t, ctx, s, "team-alice@test.dev")
	team := &models.Team{Name: "Platform", Description: "platform team"}
	if err := s.CreateTeam(ctx, team); err != nil || team.ID == 0 {
		t.Fatalf("CreateTeam: %v / id=%d", err, team.ID)
	}

	if got, err := s.GetTeamByName(ctx, "Platform"); err != nil || got.ID != team.ID {
		t.Fatalf("GetTeamByName: %v / %+v", err, got)
	}

	if err := s.AddTeamMember(ctx, team.ID, alice.ID, models.TeamRoleAdmin); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	m, err := s.GetTeamMember(ctx, team.ID, alice.ID)
	if err != nil || m == nil || m.Role != models.TeamRoleAdmin {
		t.Fatalf("GetTeamMember: %v / %+v", err, m)
	}
	teams, err := s.ListTeamsForUser(ctx, alice.ID)
	if err != nil || len(teams) != 1 || teams[0].Role != models.TeamRoleAdmin {
		t.Fatalf("ListTeamsForUser: %v / %+v", err, teams)
	}

	src := mkSource(t, ctx, s, "events")
	if err := s.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	assertSourceAccess(t, ctx, s, team.ID, alice.ID, src.ID, true)
	// Negative case: an unlinked source must report no access (no error).
	other := mkSource(t, ctx, s, "unlinked")
	assertSourceAccess(t, ctx, s, team.ID, alice.ID, other.ID, false)

	srcs, err := s.ListTeamSources(ctx, team.ID)
	if err != nil || len(srcs) != 1 {
		t.Fatalf("ListTeamSources: %v / %d", err, len(srcs))
	}
}

func testSessions(t *testing.T, ctx context.Context, s store.Store) {
	u := mkUser(t, ctx, s, "sess@test.dev")
	sess := &models.Session{ID: models.SessionID("sess-token-1"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got, err := s.GetSession(ctx, sess.ID); err != nil || got.UserID != u.ID {
		t.Fatalf("GetSession: %v / %+v", err, got)
	}
	if n, err := s.CountUserSessions(ctx, u.ID); err != nil || n != 1 {
		t.Errorf("CountUserSessions = %d / %v", n, err)
	}
	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession(ctx, sess.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("after delete GetSession err = %v, want ErrNotFound", err)
	}

	// DeleteExpiredSessions only sweeps rows whose expiry has passed; a live
	// session must survive the sweep.
	live := &models.Session{ID: models.SessionID("sess-live"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	expired := &models.Session{ID: models.SessionID("sess-expired"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)}
	if err := s.CreateSession(ctx, live); err != nil {
		t.Fatalf("CreateSession(live): %v", err)
	}
	if err := s.CreateSession(ctx, expired); err != nil {
		t.Fatalf("CreateSession(expired): %v", err)
	}
	if err := s.DeleteExpiredSessions(ctx, time.Now()); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if _, err := s.GetSession(ctx, live.ID); err != nil {
		t.Errorf("live session should survive sweep, got err: %v", err)
	}
	if _, err := s.GetSession(ctx, expired.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("expired session should be swept, got err: %v", err)
	}
}

func testSettings(t *testing.T, ctx context.Context, s store.Store) {
	if err := s.UpsertSetting(ctx, "alerts.enabled", "true", "boolean", "alerts", "Enable alerts", false); err != nil {
		t.Fatalf("UpsertSetting: %v", err)
	}
	if v, err := s.GetSetting(ctx, "alerts.enabled"); err != nil || v != "true" {
		t.Errorf("GetSetting = %q / %v", v, err)
	}
	if !s.GetBoolSetting(ctx, "alerts.enabled", false) {
		t.Error("GetBoolSetting = false, want true")
	}
	list, err := s.ListSettings(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("ListSettings: %v / %d", err, len(list))
	}
	byCat, err := s.ListSettingsByCategory(ctx, "alerts")
	if err != nil || len(byCat) == 0 {
		t.Fatalf("ListSettingsByCategory: %v / %d", err, len(byCat))
	}
	if err := s.DeleteSetting(ctx, "alerts.enabled"); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
}

func testSavedQueriesCollections(t *testing.T, ctx context.Context, s store.Store) {
	owner := mkUser(t, ctx, s, "sq-owner@test.dev")
	src := mkSource(t, ctx, s, "sq")

	sq, err := s.CreateSavedQuery(ctx, src.ID, nil, "errors", "5xx errors", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"status>=500"}`, &owner.ID)
	if err != nil || sq.ID == 0 {
		t.Fatalf("CreateSavedQuery: %v / %+v", err, sq)
	}
	if got, err := s.GetSavedQuery(ctx, sq.ID); err != nil || got.Name != "errors" {
		t.Fatalf("GetSavedQuery: %v / %+v", err, got)
	}

	// Personal collection: one per user (partial unique index).
	pc, err := s.CreateCollection(ctx, "My Queries", "", true, owner.ID)
	if err != nil || pc.ID == 0 {
		t.Fatalf("CreateCollection(personal): %v / %+v", err, pc)
	}
	if _, err := s.CreateCollection(ctx, "Dup Personal", "", true, owner.ID); !errors.Is(err, models.ErrConflict) {
		t.Errorf("second personal collection err = %v, want ErrConflict", err)
	}
	if got, err := s.GetPersonalCollection(ctx, owner.ID); err != nil || got.ID != pc.ID {
		t.Fatalf("GetPersonalCollection: %v / %+v", err, got)
	}

	if err := s.AddCollectionItem(ctx, pc.ID, sq.ID, 0, &owner.ID); err != nil {
		t.Fatalf("AddCollectionItem: %v", err)
	}
	items, err := s.ListCollectionItems(ctx, pc.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListCollectionItems: %v / %d", err, len(items))
	}

	// Not-found neutrality: both backends return models.ErrNotFound (never a raw
	// driver error) when a personal collection or membership is absent.
	stranger := mkUser(t, ctx, s, "sq-stranger@test.dev")
	if _, err := s.GetPersonalCollection(ctx, stranger.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetPersonalCollection(none) err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetCollectionMember(ctx, pc.ID, stranger.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetCollectionMember(non-member) err = %v, want ErrNotFound", err)
	}
}

func testDashboards(t *testing.T, ctx context.Context, s store.Store) {
	owner := mkUser(t, ctx, s, "dash-owner@test.dev")

	panels := json.RawMessage(`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"5xx","type":"timeseries","team_id":1,"source_id":1,"query":"status>=500","query_language":"logchefql"}]}`)
	d := &models.Dashboard{Name: "Ops", Description: "ops overview", PanelsJSON: panels, CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, d); err != nil || d.ID == 0 {
		t.Fatalf("CreateDashboard: %v / id=%d", err, d.ID)
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() {
		t.Fatalf("CreateDashboard did not populate timestamps: %+v", d)
	}

	got, err := s.GetDashboard(ctx, d.ID)
	if err != nil || got.Name != "Ops" || got.Description != "ops overview" {
		t.Fatalf("GetDashboard: %v / %+v", err, got)
	}
	// panels_json must round-trip byte-for-byte.
	if string(got.PanelsJSON) != string(panels) {
		t.Fatalf("panels did not round-trip:\n got %s\nwant %s", got.PanelsJSON, panels)
	}
	if got.CreatedByEmail != owner.Email {
		t.Fatalf("GetDashboard creator identity not joined: %q", got.CreatedByEmail)
	}
	if got.CreatedBy == nil || *got.CreatedBy != owner.ID {
		t.Fatalf("created_by not persisted: %+v", got.CreatedBy)
	}

	// List includes it, newest-updated first, with the creator's email joined in.
	list, err := s.ListDashboards(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("ListDashboards: %v / %d", err, len(list))
	}
	if list[0].ID != d.ID {
		t.Errorf("ListDashboards[0].ID = %d, want %d (newest first)", list[0].ID, d.ID)
	}
	if list[0].CreatedByEmail != owner.Email {
		t.Errorf("ListDashboards[0].CreatedByEmail = %q, want %q", list[0].CreatedByEmail, owner.Email)
	}

	// Update mutates name + panels.
	newPanels := json.RawMessage(`{"version":1,"layout":[],"panels":[]}`)
	got.Name = "Ops v2"
	got.PanelsJSON = newPanels
	if err := s.UpdateDashboard(ctx, got); err != nil {
		t.Fatalf("UpdateDashboard: %v", err)
	}
	if after, _ := s.GetDashboard(ctx, d.ID); after.Name != "Ops v2" || string(after.PanelsJSON) != string(newPanels) {
		t.Errorf("after update = %+v", after)
	}

	if err := s.DeleteDashboard(ctx, d.ID); err != nil {
		t.Fatalf("DeleteDashboard: %v", err)
	}

	// Not-found neutrality: both backends surface models.ErrNotFound (never a raw
	// driver error) for a missing dashboard, on read and on mutation.
	if _, err := s.GetDashboard(ctx, d.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetDashboard(deleted) err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDashboard(ctx, d.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("DeleteDashboard(deleted) err = %v, want ErrNotFound", err)
	}
	if err := s.UpdateDashboard(ctx, got); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("UpdateDashboard(deleted) err = %v, want ErrNotFound", err)
	}
}

func testAlerts(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "alerts")
	a := &models.Alert{
		SourceID:          src.ID,
		Name:              "5xx spike",
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             "SELECT count() FROM logs",
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
		FrequencySeconds:  60,
		Severity:          models.AlertSeverityWarning,
		IsActive:          true,
		LastState:         models.AlertStateResolved,
	}
	if err := s.CreateAlert(ctx, a); err != nil || a.ID == 0 {
		t.Fatalf("CreateAlert: %v / id=%d", err, a.ID)
	}

	got, err := s.GetAlert(ctx, a.ID)
	if err != nil || got.Name != "5xx spike" || got.ThresholdValue != 10 {
		t.Fatalf("GetAlert: %v / %+v", err, got)
	}

	bySrc, err := s.ListAlertsBySource(ctx, src.ID)
	if err != nil || len(bySrc) != 1 {
		t.Fatalf("ListAlertsBySource: %v / %d", err, len(bySrc))
	}

	// A never-evaluated active alert is due (LastEvaluatedAt IS NULL). This is
	// the path whose dialect-specific time math differs between backends.
	due, err := s.ListActiveAlertsDue(ctx)
	if err != nil {
		t.Fatalf("ListActiveAlertsDue: %v", err)
	}
	found := false
	for _, d := range due {
		if d.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("fresh active alert %d not returned by ListActiveAlertsDue", a.ID)
	}

	// History round-trip.
	val := 12.0
	if _, err := s.InsertAlertHistory(ctx, a.ID, models.AlertStatusTriggered, &val, "fired", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("InsertAlertHistory: %v", err)
	}
	hist, err := s.ListAlertHistory(ctx, a.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("ListAlertHistory: %v / %d", err, len(hist))
	}

	if err := s.DeleteAlert(ctx, a.ID); err != nil {
		t.Fatalf("DeleteAlert: %v", err)
	}

	// Not-found neutrality: both backends surface models.ErrNotFound (never a
	// raw driver error) for a missing alert, on read and on mutation.
	if _, err := s.GetAlert(ctx, a.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetAlert(deleted) err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAlert(ctx, a.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("DeleteAlert(deleted) err = %v, want ErrNotFound", err)
	}
}

// testQuerySharesExportJobsNotFound guards backend-neutral not-found on the
// query-share and export-job read/delete paths — both backends must return
// models.ErrNotFound for a missing token/id (SQLite previously leaked raw
// sql.ErrNoRows here while Postgres translated it).
func testQuerySharesExportJobsNotFound(t *testing.T, ctx context.Context, s store.Store) {
	if _, err := s.GetQueryShare(ctx, "nonexistent-token"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetQueryShare(missing) err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteQueryShare(ctx, "nonexistent-token"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("DeleteQueryShare(missing) err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetExportJob(ctx, "nonexistent-id"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetExportJob(missing) err = %v, want ErrNotFound", err)
	}
}

func testUserPreferences(t *testing.T, ctx context.Context, s store.Store) {
	u := mkUser(t, ctx, s, "prefs@test.dev")
	if err := s.UpsertUserPreferencesJSON(ctx, u.ID, `{"theme":"dark"}`); err != nil {
		t.Fatalf("UpsertUserPreferencesJSON: %v", err)
	}
	got, err := s.GetUserPreferencesJSON(ctx, u.ID)
	if err != nil || got != `{"theme":"dark"}` {
		t.Errorf("GetUserPreferencesJSON = %q / %v", got, err)
	}
}

func testProvisioning(t *testing.T, ctx context.Context, s store.Store) {
	u := mkUser(t, ctx, s, "managed@test.dev")
	if managed, err := s.IsUserManaged(ctx, u.ID); err != nil || managed {
		t.Errorf("IsUserManaged(new) = %v / %v, want false", managed, err)
	}
	if err := s.SetUserManaged(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserManaged: %v", err)
	}
	if managed, err := s.IsUserManaged(ctx, u.ID); err != nil || !managed {
		t.Errorf("IsUserManaged(after set) = %v / %v, want true", managed, err)
	}
}

func testWithTxCommit(t *testing.T, ctx context.Context, s store.Store) {
	email := "tx-commit@test.dev"
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		mkUser(t, ctx, tx, email)
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if _, err := s.GetUserByEmail(ctx, email); err != nil {
		t.Errorf("user should exist after commit: %v", err)
	}
}

func testWithTxRollback(t *testing.T, ctx context.Context, s store.Store) {
	email := "tx-rollback@test.dev"
	boom := errors.New("boom")
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		u := &models.User{Email: email, FullName: email, Role: models.UserRoleMember, Status: models.UserStatusActive}
		if err := tx.CreateUser(ctx, u); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx err = %v, want boom", err)
	}
	if _, err := s.GetUserByEmail(ctx, email); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("user should not exist after rollback: %v", err)
	}
}

// testWithTxNoNesting asserts that re-entering WithTx from inside a transaction
// is rejected rather than deadlocking (SQLite's single write connection) or
// nil-panicking (Postgres's nil tx-scoped pool). The tx handle is StoreOps,
// which has no WithTx; a caller can only nest by asserting it back to TxRunner.
func testWithTxNoNesting(t *testing.T, ctx context.Context, s store.Store) {
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		txr, ok := tx.(store.TxRunner)
		if !ok {
			t.Fatal("tx handle does not expose TxRunner")
		}
		if nestErr := txr.WithTx(ctx, func(store.StoreOps) error { return nil }); nestErr == nil {
			t.Error("nested WithTx should return an error, got nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
}
