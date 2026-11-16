package reports

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestResolveRecipients(t *testing.T) {
	user := func(id models.UserID, email string) *models.User {
		return &models.User{ID: id, Email: email, Status: models.UserStatusActive}
	}
	users := map[models.UserID]*models.User{
		1: user(1, "a@example.com"), 2: user(2, "B@example.com"), 3: user(3, "c@example.com"), 4: user(4, ""), 5: user(5, ""),
	}
	uid := func(id models.UserID) *models.UserID { return &id }
	tid := func(id models.TeamID) *models.TeamID { return &id }
	rows := []models.ReportRecipient{
		{Kind: models.ReportRecipientTeam, TeamID: tid(7)},
		{Kind: models.ReportRecipientTeam, TeamID: tid(8)},
		{Kind: models.ReportRecipientUser, UserID: uid(3)},
	}
	members := map[models.TeamID][]models.UserID{7: {2, 3, 5, 4}, 8: {1, 2, 5}}
	got, err := ResolveRecipients(rows, users, members)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, r := range got {
		parts = append(parts, fmt.Sprintf("%d:%s", r.User.ID, r.Via))
	}
	// User rows win over team rows, the first team over a later one, and
	// the list is in user id order; users without an address stay apart.
	if want := "1:team:8 2:team:7 3:direct 4:team:7 5:team:7"; strings.Join(parts, " ") != want {
		t.Errorf("resolved = %s, want %s", strings.Join(parts, " "), want)
	}

	// Two users with one address, ignoring case, receive one message.
	users[6] = user(6, "b@EXAMPLE.com")
	got, err = ResolveRecipients([]models.ReportRecipient{{Kind: models.ReportRecipientUser, UserID: uid(6)}, {Kind: models.ReportRecipientUser, UserID: uid(2)}}, users, nil)
	if err != nil || len(got) != 1 || got[0].User.ID != 6 {
		t.Errorf("one address twice = %+v, %v", got, err)
	}

	if _, err := ResolveRecipients([]models.ReportRecipient{{Kind: models.ReportRecipientUser, UserID: uid(99)}}, users, nil); err == nil {
		t.Error("a row naming an unloaded user resolves")
	}
}

func TestCanReceive(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    models.User
		want string
	}{
		{"active human", models.User{Email: "a@example.com", Status: models.UserStatusActive}, ""},
		{"inactive", models.User{Email: "a@example.com", Status: models.UserStatusInactive}, SkipInactive},
		{"service account", models.User{Email: "svc@example.com", Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}, SkipInactive},
		{"no address", models.User{Status: models.UserStatusActive}, SkipInactive},
	} {
		if got := CanReceive(&tc.u); got != tc.want {
			t.Errorf("%s: CanReceive = %q, want %q", tc.name, got, tc.want)
		}
	}
}
