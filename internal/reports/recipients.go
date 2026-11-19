package reports

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
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
	// SkipExternalDisabled: external recipients are off on this server.
	SkipExternalDisabled = "external_disabled"
	// SkipDomainNotAllowed: the address is outside the allowed domains.
	SkipDomainNotAllowed = "domain_not_allowed"
)

// Resolved is one person a run sends to, and the row that named them:
// "direct" for a user row, "team:<id>" for a team row, "email" for an email
// row. User is nil for an external address, which Address then holds.
type Resolved struct {
	User    *models.User
	Address string
	Via     string
}

// ResolveRecipients turns recipient rows into people at send time: a user
// row is that user, a team row is the team's members now, and an email row
// is the user with that address, ignoring case, or else an external
// address. A person named twice is kept once, by a user row before a team
// row before an email row, and the first team row before a later one. The
// result is users in user id order, then external addresses in order, the
// order the recipient cap keeps. users holds every user, since any of them
// may own an email row's address; members holds each team row's member ids.
func ResolveRecipients(rows []models.ReportRecipient, users map[models.UserID]*models.User, members map[models.TeamID][]models.UserID) ([]Resolved, error) {
	byAddress := UsersByAddress(maps.Values(users))
	seen := map[string]bool{}
	var out, external []Resolved
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
	for _, r := range rows {
		if r.Kind != models.ReportRecipientEmail {
			continue
		}
		key := strings.ToLower(r.Email)
		if u := byAddress[key]; u != nil {
			if err := add(u.ID, "email"); err != nil {
				return nil, err
			}
			continue
		}
		if !seen["address:"+key] {
			seen["address:"+key] = true
			external = append(external, Resolved{Address: r.Email, Via: "email"})
		}
	}
	slices.SortFunc(out, func(a, b Resolved) int { return cmp.Compare(a.User.ID, b.User.ID) })
	slices.SortFunc(external, func(a, b Resolved) int { return cmp.Compare(strings.ToLower(a.Address), strings.ToLower(b.Address)) })
	return append(out, external...), nil
}

// UsersByAddress maps each user's address, in lower case, to the user.
// Two users whose addresses differ only in case map to the lower id, so the
// choice does not depend on the order of users.
func UsersByAddress(users iter.Seq[*models.User]) map[string]*models.User {
	out := map[string]*models.User{}
	for u := range users {
		key := strings.ToLower(u.Email)
		if u.Email != "" && (out[key] == nil || u.ID < out[key].ID) {
			out[key] = u
		}
	}
	return out
}

// CanReceive is the first test of a Logchef user recipient: an active
// human account with an address. It returns SkipInactive, or "".
func CanReceive(u *models.User) string {
	if u.Status != models.UserStatusActive || u.AccountType == models.UserAccountTypeService || u.Email == "" {
		return SkipInactive
	}
	return ""
}
