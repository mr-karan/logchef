package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

type destinationFixture struct {
	owner, member, outsider   *models.User
	teamID                    models.TeamID
	private, teamDest, global *models.NotificationDestination
}

// testDestinations covers the destination store: the round trip of config and
// secret, who can use what, the links to rules, and the delete rules.
func testDestinations(t *testing.T, ctx context.Context, s store.Store) {
	f := destinationFixture{
		owner:    mkUser(t, ctx, s, "dest-owner@example.com"),
		member:   mkUser(t, ctx, s, "dest-member@example.com"),
		outsider: mkUser(t, ctx, s, "dest-outsider@example.com"),
	}
	team := &models.Team{Name: "dest-team"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTeamMember(ctx, team.ID, f.member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	f.teamID = team.ID
	hookURL := "https://hooks.example.com/t/secret-token"
	f.private = &models.NotificationDestination{Name: "Ops webhook", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopePrivate,
		Config: models.DestinationConfig{PayloadVersion: 1}, Secret: hookURL, IsActive: true, CreatedBy: &f.owner.ID}
	f.teamDest = &models.NotificationDestination{Name: "Team mail", Type: models.DestinationTypeEmail, Scope: models.DestinationScopeTeam, TeamID: &team.ID,
		Config: models.DestinationConfig{UserIDs: []models.UserID{f.member.ID}, Addresses: []string{"oncall@example.com"}}, IsActive: true, CreatedBy: &f.owner.ID}
	f.global = &models.NotificationDestination{Name: "All hands", Type: models.DestinationTypeEmail, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{UserIDs: []models.UserID{f.owner.ID}}, IsActive: false}
	for _, d := range []*models.NotificationDestination{f.private, f.teamDest, f.global} {
		if err := s.CreateNotificationDestination(ctx, d); err != nil || d.ID == 0 {
			t.Fatalf("create %s: %v, id %d", d.Name, err, d.ID)
		}
	}
	testDestinationReads(t, ctx, s, &f)
	testDestinationUsable(t, ctx, s, &f)
	testDestinationUpdate(t, ctx, s, &f)
	testDestinationLinks(t, ctx, s, &f)
}

func testDestinationReads(t *testing.T, ctx context.Context, s store.Store, f *destinationFixture) {
	got, err := s.GetNotificationDestination(ctx, f.private.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret != f.private.Secret || !got.HasSecret || got.SecretHint != "hooks.example.com" || got.Config.PayloadVersion != 1 || got.CreatedBy == nil || *got.CreatedBy != f.owner.ID {
		t.Errorf("private destination round trip = %+v", got)
	}
	got, err = s.GetNotificationDestination(ctx, f.teamDest.ID)
	if err != nil || got.TeamID == nil || *got.TeamID != f.teamID || got.HasSecret ||
		!slices.Equal(got.Config.UserIDs, f.teamDest.Config.UserIDs) || !slices.Equal(got.Config.Addresses, f.teamDest.Config.Addresses) {
		t.Errorf("team destination round trip = %+v, %v", got, err)
	}
	if _, err := s.GetNotificationDestination(ctx, 999999); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("get missing destination: %v, want ErrNotFound", err)
	}
	if all, err := s.ListNotificationDestinations(ctx); err != nil || len(all) < 3 {
		t.Errorf("list all = %d, %v", len(all), err)
	}
	if n, err := s.CountTeamNotificationDestinations(ctx, f.teamID); err != nil || n != 1 {
		t.Errorf("team destinations = %d, %v; want 1", n, err)
	}
}

func sortedDestinationIDs(ids ...models.DestinationID) []models.DestinationID {
	slices.Sort(ids)
	return ids
}

func testDestinationUsable(t *testing.T, ctx context.Context, s store.Store, f *destinationFixture) {
	usable := func(user *models.User) []models.DestinationID {
		t.Helper()
		list, err := s.ListUsableNotificationDestinations(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		var ids []models.DestinationID
		for _, d := range list {
			ids = append(ids, d.ID)
		}
		return sortedDestinationIDs(ids...)
	}
	for _, tc := range []struct {
		user *models.User
		want []models.DestinationID
	}{
		{f.owner, sortedDestinationIDs(f.private.ID, f.global.ID)},
		{f.member, sortedDestinationIDs(f.teamDest.ID, f.global.ID)},
		{f.outsider, []models.DestinationID{f.global.ID}},
	} {
		if got := usable(tc.user); !slices.Equal(got, tc.want) {
			t.Errorf("%s can use %v, want %v", tc.user.Email, got, tc.want)
		}
	}
}

func testDestinationUpdate(t *testing.T, ctx context.Context, s store.Store, f *destinationFixture) {
	otherURL := "https://other.example.com/x"
	f.private.Name, f.private.Secret, f.private.IsActive = "Ops webhook v2", otherURL, false
	if err := s.UpdateNotificationDestination(ctx, f.private); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetNotificationDestination(ctx, f.private.ID); err != nil || got.Name != "Ops webhook v2" || got.SecretHint != "other.example.com" || got.IsActive {
		t.Errorf("after update = %+v, %v", got, err)
	}
	missing := *f.private
	missing.ID = 999999
	if err := s.UpdateNotificationDestination(ctx, &missing); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("update missing destination: %v, want ErrNotFound", err)
	}
}

func testDestinationLinks(t *testing.T, ctx context.Context, s store.Store, f *destinationFixture) {
	src := mkSource(t, ctx, s, "dest_alerts")
	alert := &models.Alert{SourceID: src.ID, Name: "dest rule", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		Query: "SELECT 1", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 1,
		FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved, CreatedBy: &f.owner.ID}
	if err := s.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlertDestinations(ctx, alert.ID, []models.DestinationID{f.teamDest.ID, f.private.ID}); err != nil {
		t.Fatal(err)
	}
	want := sortedDestinationIDs(f.private.ID, f.teamDest.ID)
	if got, err := s.GetAlert(ctx, alert.ID); err != nil || !slices.Equal(got.DestinationIDs, want) {
		t.Errorf("alert destination ids = %v, %v; want %v", got.DestinationIDs, err, want)
	}
	if listed, err := s.ListAlertsBySource(ctx, src.ID); err != nil || len(listed) != 1 || !slices.Equal(listed[0].DestinationIDs, want) {
		t.Errorf("listed alert destination ids = %+v, %v", listed, err)
	}
	full, err := s.ListAlertDestinations(ctx, alert.ID)
	if err != nil || len(full) != 2 || full[0].ID != want[0] || full[1].ID != want[1] {
		t.Fatalf("alert destinations = %+v, %v", full, err)
	}
	if i := slices.IndexFunc(full, func(d *models.NotificationDestination) bool { return d.ID == f.private.ID }); full[i].Secret != f.private.Secret {
		t.Errorf("alert destination secret = %q, want the stored URL", full[i].Secret)
	}
	if n, err := s.CountAlertsUsingDestination(ctx, f.private.ID); err != nil || n != 1 {
		t.Errorf("rules using private = %d, %v; want 1", n, err)
	}
	if err := s.DeleteNotificationDestination(ctx, f.private.ID); !errors.Is(err, models.ErrConflict) {
		t.Errorf("delete a destination in use: %v, want ErrConflict", err)
	}

	testDestinationRelink(t, ctx, s, f, alert.ID)
}

// testDestinationRelink replaces the rule's links, then deletes the freed
// destination and the rule.
func testDestinationRelink(t *testing.T, ctx context.Context, s store.Store, f *destinationFixture, alertID models.AlertID) {
	if err := s.SetAlertDestinations(ctx, alertID, []models.DestinationID{f.global.ID}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetAlert(ctx, alertID); err != nil || !slices.Equal(got.DestinationIDs, []models.DestinationID{f.global.ID}) {
		t.Errorf("after replacing links = %v, %v", got.DestinationIDs, err)
	}
	if err := s.DeleteNotificationDestination(ctx, f.private.ID); err != nil {
		t.Errorf("delete an unused destination: %v", err)
	}
	if err := s.DeleteNotificationDestination(ctx, f.private.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("delete twice: %v, want ErrNotFound", err)
	}
	if err := s.DeleteAlert(ctx, alertID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountAlertsUsingDestination(ctx, f.global.ID); err != nil || n != 0 {
		t.Errorf("links after the rule was deleted = %d, %v; want 0", n, err)
	}
}
