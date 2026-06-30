// Package storetest provides a shared, backend-agnostic conformance suite for
// the store.Store contract. Run(t, s) is executed against both backends (a temp
// SQLite file and a Postgres instance) to prove they behave identically.
package storetest

import (
	"context"
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
	t.Run("TeamsMembersSources", func(t *testing.T) { testTeams(t, ctx, s) })
	t.Run("Sessions", func(t *testing.T) { testSessions(t, ctx, s) })
	t.Run("Settings", func(t *testing.T) { testSettings(t, ctx, s) })
	t.Run("SavedQueriesCollections", func(t *testing.T) { testSavedQueriesCollections(t, ctx, s) })
	t.Run("Alerts", func(t *testing.T) { testAlerts(t, ctx, s) })
	t.Run("UserPreferences", func(t *testing.T) { testUserPreferences(t, ctx, s) })
	t.Run("Provisioning", func(t *testing.T) { testProvisioning(t, ctx, s) })
	t.Run("WithTxCommit", func(t *testing.T) { testWithTxCommit(t, ctx, s) })
	t.Run("WithTxRollback", func(t *testing.T) { testWithTxRollback(t, ctx, s) })
}

// --- helpers ---

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

func mkSource(t *testing.T, ctx context.Context, s store.StoreOps, db, table string) *models.Source {
	t.Helper()
	src := &models.Source{
		Name:        db + "." + table,
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: db, TableName: table},
	}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return src
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

	src := mkSource(t, ctx, s, "logs", "events")
	if err := s.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	if ok, err := s.TeamHasSource(ctx, team.ID, src.ID); err != nil || !ok {
		t.Errorf("TeamHasSource = %v / %v", ok, err)
	}
	if ok, err := s.UserHasSourceAccess(ctx, alice.ID, src.ID); err != nil || !ok {
		t.Errorf("UserHasSourceAccess = %v / %v", ok, err)
	}
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
	src := mkSource(t, ctx, s, "logs", "sq")

	sq, err := s.CreateSavedQuery(ctx, src.ID, nil, "errors", "5xx errors", "logchef", `{"content":"status>=500"}`, &owner.ID)
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
}

func testAlerts(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "logs", "alerts")
	a := &models.Alert{
		SourceID:          src.ID,
		Name:              "5xx spike",
		QueryType:         models.AlertQueryTypeSQL,
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
