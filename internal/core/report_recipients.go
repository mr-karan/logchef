package core

// Report recipients (RFC 0008 unit R8): the user and team rows of a report,
// who may add them, how a run resolves them into people at send time, and
// which panels each person can reach.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// maxReportRecipientRows bounds the recipient rows of one report. A team
// counts once; it expands at send time.
const maxReportRecipientRows = 50

// reportRecipientInput is one entry of the recipients array of POST and
// PUT /reports.
type reportRecipientInput struct {
	Kind   models.ReportRecipientKind `json:"kind"`
	UserID *models.UserID             `json:"user_id"`
	TeamID *models.TeamID             `json:"team_id"`
	Email  *string                    `json:"email"`
}

// recipientKey identifies a row by its target, as the unique index does.
func recipientKey(r models.ReportRecipient) string {
	switch {
	case r.Kind == models.ReportRecipientUser && r.UserID != nil:
		return "user:" + strconv.FormatInt(int64(*r.UserID), 10)
	case r.Kind == models.ReportRecipientTeam && r.TeamID != nil:
		return "team:" + strconv.FormatInt(int64(*r.TeamID), 10)
	}
	return string(r.Kind) + ":" + strings.ToLower(r.Email)
}

// parseReportRecipients reads the recipients array. An empty array means
// the owner alone, stored as a user row so the editor shows it.
func parseReportRecipients(raw []json.RawMessage, owner *models.User) ([]models.ReportRecipient, error) {
	if len(raw) > maxReportRecipientRows {
		return nil, invalidReport("recipients: at most %d entries; add a team to reach more people", maxReportRecipientRows)
	}
	if len(raw) == 0 {
		if owner == nil {
			return nil, invalidReport("recipients: a report without an owner needs at least one recipient")
		}
		return []models.ReportRecipient{{Kind: models.ReportRecipientUser, UserID: &owner.ID}}, nil
	}
	out := make([]models.ReportRecipient, 0, len(raw))
	seen := map[string]bool{}
	for i, item := range raw {
		var in reportRecipientInput
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return nil, invalidReport("recipients[%d]: %v", i, err)
		}
		var r models.ReportRecipient
		switch {
		case in.Kind == models.ReportRecipientUser && in.UserID != nil && in.TeamID == nil && in.Email == nil:
			r = models.ReportRecipient{Kind: in.Kind, UserID: in.UserID}
		case in.Kind == models.ReportRecipientTeam && in.TeamID != nil && in.UserID == nil && in.Email == nil:
			r = models.ReportRecipient{Kind: in.Kind, TeamID: in.TeamID}
		case in.Kind == models.ReportRecipientEmail:
			return nil, invalidReport("recipients[%d]: external email recipients are off on this server", i)
		default:
			return nil, invalidReport("recipients[%d]: a user entry needs user_id only and a team entry team_id only", i)
		}
		key := recipientKey(r)
		if seen[key] {
			return nil, invalidReport("recipients[%d]: %s is listed twice", i, key)
		}
		seen[key] = true
		out = append(out, r)
	}
	return out, nil
}

// checkNewRecipients checks the rows that a save adds. A row the report
// already holds stays, whoever added it. A global admin may add any user
// or team; anyone else may add themself, a user who shares a team with
// them, or a team they belong to. Every added row records who added it.
func checkNewRecipients(ctx context.Context, db store.StoreOps, p access.Principal, existing, next []models.ReportRecipient) error {
	held := make(map[string]models.ReportRecipient, len(existing))
	for _, r := range existing {
		held[recipientKey(r)] = r
	}
	options, err := ListReportRecipientOptions(ctx, db, p)
	if err != nil {
		return err
	}
	for i := range next {
		r := &next[i]
		if old, ok := held[recipientKey(*r)]; ok {
			r.AddedBy = old.AddedBy
			continue
		}
		r.AddedBy = &p.User.ID
		switch r.Kind {
		case models.ReportRecipientUser:
			if !options.hasUser(*r.UserID) {
				return invalidReport("recipients: user %d is not one you can add; pick a member of one of your teams", *r.UserID)
			}
		case models.ReportRecipientTeam:
			if !options.hasTeam(*r.TeamID) {
				return invalidReport("recipients: team %d is not one you can add; pick a team you belong to", *r.TeamID)
			}
		case models.ReportRecipientEmail:
			return invalidReport("recipients: external email recipients are off on this server")
		}
	}
	return nil
}

// ReportRecipientOption is a user or team the caller may add.
type ReportRecipientOption struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// ReportRecipientOptions lists what the recipient picker offers.
type ReportRecipientOptions struct {
	Users []ReportRecipientOption `json:"users"`
	Teams []ReportRecipientOption `json:"teams"`
}

func (o *ReportRecipientOptions) hasUser(id models.UserID) bool {
	return slices.ContainsFunc(o.Users, func(u ReportRecipientOption) bool { return u.ID == int64(id) })
}

func (o *ReportRecipientOptions) hasTeam(id models.TeamID) bool {
	return slices.ContainsFunc(o.Teams, func(t ReportRecipientOption) bool { return t.ID == int64(id) })
}

// ListReportRecipientOptions returns the users and teams the caller may add
// as recipients, by name. Service accounts receive no email and are left
// out.
func ListReportRecipientOptions(ctx context.Context, db store.StoreOps, p access.Principal) (*ReportRecipientOptions, error) {
	out := &ReportRecipientOptions{Users: []ReportRecipientOption{}, Teams: []ReportRecipientOption{}}
	seen := map[int64]bool{}
	addUser := func(id models.UserID, name, email string, accountType models.UserAccountType) {
		if !seen[int64(id)] && accountType != models.UserAccountTypeService {
			seen[int64(id)] = true
			out.Users = append(out.Users, ReportRecipientOption{ID: int64(id), Name: name, Email: email})
		}
	}
	var (
		teams []*models.Team
		err   error
	)
	if p.RequireGlobalAdmin() == nil {
		if teams, err = db.ListTeams(ctx); err != nil {
			return nil, fmt.Errorf("listing teams: %w", err)
		}
		users, err := db.ListUsers(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing users: %w", err)
		}
		for _, u := range users {
			addUser(u.ID, u.FullName, u.Email, u.AccountType)
		}
	} else {
		if teams, err = db.ListUserTeams(ctx, p.User.ID); err != nil {
			return nil, fmt.Errorf("listing the caller's teams: %w", err)
		}
		addUser(p.User.ID, p.User.FullName, p.User.Email, p.User.AccountType)
		for _, t := range teams {
			members, err := db.ListTeamMembersWithDetails(ctx, t.ID)
			if err != nil {
				return nil, fmt.Errorf("listing members of team %d: %w", t.ID, err)
			}
			for _, m := range members {
				addUser(m.UserID, m.FullName, m.Email, m.AccountType)
			}
		}
	}
	for _, t := range teams {
		out.Teams = append(out.Teams, ReportRecipientOption{ID: int64(t.ID), Name: t.Name})
	}
	byName := func(a, b ReportRecipientOption) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Email, b.Email), cmp.Compare(a.ID, b.ID))
	}
	slices.SortFunc(out.Users, byName)
	slices.SortFunc(out.Teams, byName)
	return out, nil
}

// ReportRecipientResponse is a recipient row with the name it shows as.
type ReportRecipientResponse struct {
	Kind   models.ReportRecipientKind `json:"kind"`
	UserID *models.UserID             `json:"user_id,omitempty"`
	TeamID *models.TeamID             `json:"team_id,omitempty"`
	Email  string                     `json:"email,omitempty"`
	Name   string                     `json:"name"`
}

func recipientResponses(rows []models.ReportRecipient, users map[models.UserID]*models.User, teams map[models.TeamID]string) []ReportRecipientResponse {
	out := make([]ReportRecipientResponse, len(rows))
	for i, r := range rows {
		out[i] = ReportRecipientResponse{Kind: r.Kind, UserID: r.UserID, TeamID: r.TeamID, Email: r.Email}
		switch {
		case r.UserID != nil && users[*r.UserID] != nil:
			u := users[*r.UserID]
			out[i].Name, out[i].Email = cmp.Or(u.FullName, u.Email), u.Email
		case r.TeamID != nil:
			out[i].Name = teams[*r.TeamID]
		}
	}
	return out
}

// RemoveMyReportRecipient deletes the caller's direct user row. Only a
// direct recipient removes themself: a caller who receives the report
// through a team alone gets ErrInvalidReport saying so, and anyone else
// ErrReportNotFound.
func RemoveMyReportRecipient(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (*models.Report, error) {
	r, err := reloadReport(ctx, db, id)
	if err != nil {
		return nil, err
	}
	err = db.RemoveReportUserRecipient(ctx, id, p.User.ID)
	if err == nil {
		return r, nil
	}
	if !models.IsNotFound(err) {
		return nil, fmt.Errorf("removing the caller from report %d: %w", id, err)
	}
	received, err := db.ListReportsForRecipient(ctx, p.User.ID)
	if err != nil {
		return nil, fmt.Errorf("listing reports sent to user %d: %w", p.User.ID, err)
	}
	if slices.ContainsFunc(received, func(x *models.Report) bool { return x.ID == id }) {
		return nil, invalidReport("recipients: you receive this report through a team; leave the team or ask the report owner to remove it")
	}
	return nil, ErrReportNotFound
}

// resolveRunRecipients turns the report's rows into people now: users by
// their row, teams by their members at send time.
func (rr *ReportRunner) resolveRunRecipients(ctx context.Context, id models.ReportID) ([]reports.Resolved, error) {
	rows, err := rr.DB.ListReportRecipients(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("listing recipients of report %d: %w", id, err)
	}
	all, err := rr.DB.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	users := make(map[models.UserID]*models.User, len(all))
	for _, u := range all {
		users[u.ID] = u
	}
	members := map[models.TeamID][]models.UserID{}
	for _, row := range rows {
		if row.Kind != models.ReportRecipientTeam || row.TeamID == nil {
			continue
		}
		list, err := rr.DB.ListTeamMembersWithDetails(ctx, *row.TeamID)
		if err != nil {
			return nil, fmt.Errorf("listing members of team %d: %w", *row.TeamID, err)
		}
		for _, m := range list {
			members[*row.TeamID] = append(members[*row.TeamID], m.UserID)
		}
	}
	return reports.ResolveRecipients(rows, users, members)
}

// recipientReach tells which panels of a plan a user can reach, with the
// test the dashboard applies to a viewer and no exemption for the creator
// or for admins: a dashboard panel through the panel's team, a saved query
// through any team of the user's own that has its source.
type recipientReach struct {
	plan *reportPlan
	// sourceTeams holds the teams that have a saved query's source.
	sourceTeams map[models.SourceID]map[models.TeamID]bool
}

func (rr *ReportRunner) newRecipientReach(ctx context.Context, plan *reportPlan) (*recipientReach, error) {
	reach := &recipientReach{plan: plan, sourceTeams: map[models.SourceID]map[models.TeamID]bool{}}
	for _, p := range plan.panels {
		if p.teamID != 0 || reach.sourceTeams[p.sourceID] != nil {
			continue
		}
		teams, err := rr.DB.ListSourceTeams(ctx, p.sourceID)
		if err != nil {
			return nil, fmt.Errorf("listing teams of source %d: %w", p.sourceID, err)
		}
		set := map[models.TeamID]bool{}
		for _, t := range teams {
			set[t.ID] = true
		}
		reach.sourceTeams[p.sourceID] = set
	}
	return reach, nil
}

// lockedFor marks the panels u cannot reach and counts the panels with
// data that u can. A panel locked for the owner is locked for everyone
// already and stays as it is.
func (r *recipientReach) lockedFor(ctx context.Context, db store.StoreOps, u *models.User) (locked []bool, reachable int, err error) {
	teams, err := db.ListUserTeams(ctx, u.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("listing teams of user %d: %w", u.ID, err)
	}
	mine := make(map[models.TeamID]bool, len(teams))
	for _, t := range teams {
		mine[t.ID] = true
	}
	locked = make([]bool, len(r.plan.panels))
	for i, p := range r.plan.panels {
		if p.status == models.ReportPanelLocked {
			continue
		}
		ok := mine[p.teamID]
		if p.teamID == 0 {
			ok = false
			for id := range mine {
				if r.sourceTeams[p.sourceID][id] {
					ok = true
					break
				}
			}
		}
		locked[i] = !ok
		if ok && p.job != nil {
			reachable++
		}
	}
	return locked, reachable, nil
}
