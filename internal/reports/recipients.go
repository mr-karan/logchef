package reports

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// MaxResolvedRecipients bounds the people one run sends to. The rest are
// skipped with SkipRecipientLimit.
const MaxResolvedRecipients = 200

// Reasons of a skipped delivery, besides the run reasons it shares.
const (
	SkipInactive       = "inactive"
	SkipRecipientLimit = "recipient_limit"
)

// Resolved is one person a run sends to, and the row that named them:
// "direct" for a user row, "team:<id>" for a team row.
type Resolved struct {
	User *models.User
	Via  string
}

// ResolveRecipients turns recipient rows into people at send time: a user
// row is that user, and a team row is the team's members now. A person
// named twice is kept once, by a user row before a team row and the first
// team row before a later one. The result is in user id order, the order
// the recipient cap keeps. users holds every user a row or a member list
// names; members holds each team row's member ids.
func ResolveRecipients(rows []models.ReportRecipient, users map[models.UserID]*models.User, members map[models.TeamID][]models.UserID) ([]Resolved, error) {
	seen := map[string]bool{}
	var out []Resolved
	add := func(id models.UserID, via string) error {
		u := users[id]
		if u == nil {
			return fmt.Errorf("recipient user %d is not loaded", id)
		}
		key := "id:" + strconv.FormatInt(int64(u.ID), 10)
		if u.Email != "" {
			key = "address:" + strings.ToLower(u.Email)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, Resolved{User: u, Via: via})
		}
		return nil
	}
	for _, r := range rows {
		if r.Kind == models.ReportRecipientUser && r.UserID != nil {
			if err := add(*r.UserID, "direct"); err != nil {
				return nil, err
			}
		}
	}
	for _, r := range rows {
		if r.Kind != models.ReportRecipientTeam || r.TeamID == nil {
			continue
		}
		for _, id := range members[*r.TeamID] {
			if err := add(id, "team:"+strconv.FormatInt(int64(*r.TeamID), 10)); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(out, func(a, b Resolved) int { return cmp.Compare(a.User.ID, b.User.ID) })
	return out, nil
}

// CanReceive is the first test of a Logchef user recipient: an active
// human account with an address. It returns SkipInactive, or "".
func CanReceive(u *models.User) string {
	if u.Status != models.UserStatusActive || u.AccountType == models.UserAccountTypeService || u.Email == "" {
		return SkipInactive
	}
	return ""
}
