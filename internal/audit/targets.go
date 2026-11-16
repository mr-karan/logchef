package audit

import (
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"
)

// One target builder per audited object, sorted by object. The type is the
// resource part of the object's actions. A relation whose parent has no
// envelope column, such as a collection member, takes "<parent>:<child>" as
// its ID.

func id[T ~int | ~int64](v T) string { return strconv.FormatInt(int64(v), 10) }

func pair[A, B ~int | ~int64](a A, b B) string { return id(a) + ":" + id(b) }

func AlertTarget(a *models.Alert) Target {
	sourceID := a.SourceID
	return Target{Type: "alert", ID: id(a.ID), Label: a.Name, SourceID: &sourceID}
}

func APITokenTarget(t *models.APIToken) Target {
	return Target{Type: "api_token", ID: id(t.ID), Label: t.Name}
}

func APITokenIDTarget(tokenID int) Target {
	return Target{Type: "api_token", ID: id(tokenID)}
}

func CollectionTarget(collectionID int, name string) Target {
	return Target{Type: "collection", ID: id(collectionID), Label: name}
}

func CollectionItemTarget(collectionID, savedQueryID int) Target {
	return Target{Type: "collection_item", ID: pair(collectionID, savedQueryID)}
}

func CollectionMemberTarget(collectionID int, userID models.UserID) Target {
	return Target{Type: "collection_member", ID: pair(collectionID, userID)}
}

func CollectionTeamTarget(collectionID int, teamID models.TeamID) Target {
	return Target{Type: "collection_team", ID: pair(collectionID, teamID), TeamID: &teamID}
}

func DashboardTarget(dashboardID int, name string) Target {
	return Target{Type: "dashboard", ID: id(dashboardID), Label: name}
}

// ExportJobTarget is an export job: the artifact that log_export.create made.
func ExportJobTarget(job *models.ExportJob, teamID models.TeamID) Target {
	sourceID := job.SourceID
	return Target{Type: "log_export", ID: job.ID, TeamID: &teamID, SourceID: &sourceID}
}

// ExportSourceTarget is the source of a streamed export, which has no ID of
// its own.
func ExportSourceTarget(teamID models.TeamID, sourceID models.SourceID) Target {
	return Target{Type: "source", ID: id(sourceID), TeamID: &teamID, SourceID: &sourceID}
}

func OAuthConsentTarget(requestID models.OAuthAuthRequestID) Target {
	return Target{Type: "oauth_consent", ID: string(requestID)}
}

func OAuthGrantTarget(grantID models.OAuthGrantID) Target {
	return Target{Type: "oauth_grant", ID: id(grantID)}
}

// QueryShareTarget records the share by its redacted token: the full token
// is a bearer credential.
func QueryShareTarget(s *models.QueryShare) Target {
	sourceID := s.SourceID
	return Target{Type: "query_share", ID: models.RedactShareToken(s.Token), TeamID: s.TeamID, SourceID: &sourceID}
}

func ReportTarget(r *models.Report) Target {
	return Target{Type: "report", ID: id(r.ID), Label: r.Name}
}

func SavedQueryTarget(q *models.SavedQuery) Target {
	sourceID := q.SourceID
	return Target{Type: "saved_query", ID: id(q.ID), Label: q.Name, SourceID: &sourceID}
}

// SavedViewTarget is a view, with its source and, for a team view, its team.
func SavedViewTarget(v *models.SavedView) Target {
	sourceID := v.SourceID
	return Target{Type: "saved_view", ID: id(v.ID), Label: v.Name, TeamID: v.TeamID, SourceID: &sourceID}
}

// SessionUserTarget is the user a session.login or session.logout applies
// to. A failed sign-in has no user, so its label is the attempted email.
func SessionUserTarget(userID *models.UserID, email string) Target {
	t := Target{Type: "user", Label: email}
	if userID != nil {
		t.ID = id(*userID)
	}
	return t
}

func SettingTarget(key string) Target {
	return Target{Type: "setting", ID: key, Label: key}
}

// SettingTestTarget is a test notification, by kind: email or webhook.
func SettingTestTarget(kind string) Target {
	return Target{Type: "setting", ID: "test_" + kind}
}

func SourceTarget(s *models.Source) Target {
	sourceID := s.ID
	return Target{Type: "source", ID: id(s.ID), Label: s.Name, SourceID: &sourceID}
}

func SourceIDTarget(sourceID models.SourceID) Target {
	return Target{Type: "source", ID: id(sourceID), SourceID: &sourceID}
}

func TeamTarget(t *models.Team) Target {
	teamID := t.ID
	return Target{Type: "team", ID: id(t.ID), Label: t.Name, TeamID: &teamID}
}

func TeamIDTarget(teamID models.TeamID) Target {
	return Target{Type: "team", ID: id(teamID), TeamID: &teamID}
}

// TeamMemberTarget is a member by user ID; the team is in the envelope.
func TeamMemberTarget(teamID models.TeamID, userID models.UserID) Target {
	return Target{Type: "team_member", ID: id(userID), TeamID: &teamID}
}

// TeamSourceTarget is a link by source ID; both sides are in the envelope.
func TeamSourceTarget(teamID models.TeamID, sourceID models.SourceID) Target {
	return Target{Type: "team_source", ID: id(sourceID), TeamID: &teamID, SourceID: &sourceID}
}

func UserTarget(u *models.User) Target {
	return Target{Type: "user", ID: id(u.ID), Label: u.Email}
}

func UserIDTarget(userID models.UserID) Target {
	return Target{Type: "user", ID: id(userID)}
}
