package core

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestInitAdminUsersAuditsCreatesAndPromotions(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	rec := audit.New(audit.Options{Store: db, Logger: discardLogger()})
	member := &models.User{Email: "member@example.com", FullName: "Member", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
	admin := &models.User{Email: "admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
	for _, u := range []*models.User{member, admin} {
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	emails := []string{"new@example.com", "member@example.com", "admin@example.com"}
	if err := InitAdminUsers(ctx, db, rec, discardLogger(), emails); err != nil {
		t.Fatalf("InitAdminUsers: %v", err)
	}
	created, err := db.GetUserByEmail(ctx, "new@example.com")
	if err != nil {
		t.Fatal(err)
	}

	events, err := db.ListAuditEvents(ctx, models.AuditFilter{Ascending: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("stored %d events, want user.create and user.update (none for the admin): %+v", len(events), events)
	}
	create, update := events[0], events[1]
	for _, e := range events {
		if e.ActorType != models.AuditActorSystem || e.AuthMethod != models.AuditAuthStartup || e.ActorID != nil {
			t.Errorf("%s actor = %s/%s/%v, want system/startup", e.Action, e.ActorType, e.AuthMethod, e.ActorID)
		}
	}
	if create.Action != string(audit.UserCreate) || create.TargetID != strconv.Itoa(int(created.ID)) {
		t.Errorf("first event = %s on %s, want user.create on %d", create.Action, create.TargetID, created.ID)
	}
	if update.Action != string(audit.UserUpdate) || update.TargetID != strconv.Itoa(int(member.ID)) {
		t.Errorf("second event = %s on %s, want user.update on %d", update.Action, update.TargetID, member.ID)
	}
	var changes map[string]struct {
		From any `json:"from"`
		To   any `json:"to"`
	}
	if err := json.Unmarshal(update.Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes["role"].From != "member" || changes["role"].To != "admin" {
		t.Errorf("promotion changes = %+v, want only role member to admin", changes)
	}

	if err := InitAdminUsers(ctx, db, rec, discardLogger(), emails); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.ListAuditEvents(ctx, models.AuditFilter{Limit: 10}); len(again) != 2 {
		t.Errorf("a second start recorded %d new events, want none", len(again)-2)
	}
}
