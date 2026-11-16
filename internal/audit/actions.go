package audit

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Action names an audited change as <resource>.<verb>.
type Action string

// Keep each group sorted by resource; sibling RFCs append theirs.
const (
	AlertCreate  Action = "alert.create"
	AlertUpdate  Action = "alert.update"
	AlertDelete  Action = "alert.delete"
	AlertResolve Action = "alert.resolve"

	APITokenCreate Action = "api_token.create"
	APITokenDelete Action = "api_token.delete"

	AuditEventExport Action = "audit_event.export"

	CollectionCreate Action = "collection.create"
	CollectionUpdate Action = "collection.update"
	CollectionDelete Action = "collection.delete"

	CollectionItemAdd    Action = "collection_item.add"
	CollectionItemUpdate Action = "collection_item.update"
	CollectionItemRemove Action = "collection_item.remove"

	CollectionMemberAdd    Action = "collection_member.add"
	CollectionMemberRemove Action = "collection_member.remove"

	CollectionTeamAdd    Action = "collection_team.add"
	CollectionTeamRemove Action = "collection_team.remove"

	DashboardCreate Action = "dashboard.create"
	DashboardUpdate Action = "dashboard.update"
	DashboardDelete Action = "dashboard.delete"

	LogExportCreate   Action = "log_export.create"
	LogExportDownload Action = "log_export.download"

	NotificationDestinationCreate Action = "notification_destination.create"
	NotificationDestinationUpdate Action = "notification_destination.update"
	NotificationDestinationDelete Action = "notification_destination.delete"
	NotificationDestinationTest   Action = "notification_destination.test"

	OAuthConsentApprove Action = "oauth_consent.approve"
	OAuthConsentDeny    Action = "oauth_consent.deny"

	OAuthGrantRevoke Action = "oauth_grant.revoke"

	QueryShareCreate Action = "query_share.create"
	QueryShareDelete Action = "query_share.delete"

	SavedQueryCreate Action = "saved_query.create"
	SavedQueryUpdate Action = "saved_query.update"
	SavedQueryDelete Action = "saved_query.delete"

	ServiceAccountCreate Action = "service_account.create"
	ServiceAccountDelete Action = "service_account.delete"

	SessionLogin  Action = "session.login"
	SessionLogout Action = "session.logout"

	SettingUpdate Action = "setting.update"
	SettingDelete Action = "setting.delete"
	SettingTest   Action = "setting.test"

	SourceCreate Action = "source.create"
	SourceUpdate Action = "source.update"
	SourceDelete Action = "source.delete"

	TeamCreate Action = "team.create"
	TeamUpdate Action = "team.update"
	TeamDelete Action = "team.delete"

	TeamMemberAdd    Action = "team_member.add"
	TeamMemberRemove Action = "team_member.remove"

	TeamSourceLink   Action = "team_source.link"
	TeamSourceUnlink Action = "team_source.unlink"

	UserCreate Action = "user.create"
	UserUpdate Action = "user.update"
	UserDelete Action = "user.delete"

	SavedViewCreate       Action = "saved_view.create"
	SavedViewUpdate       Action = "saved_view.update"
	SavedViewDelete       Action = "saved_view.delete"
	SavedViewSetDefault   Action = "saved_view.set_default"
	SavedViewClearDefault Action = "saved_view.clear_default"

	ReportCreate Action = "report.create"
	ReportUpdate Action = "report.update"
	ReportDelete Action = "report.delete"
	ReportPause  Action = "report.pause"
	ReportResume Action = "report.resume"
	ReportClaim  Action = "report.claim"

	SchemaAdvisorDismissalCreate Action = "schema_advisor_dismissal.create"
	SchemaAdvisorDismissalDelete Action = "schema_advisor_dismissal.delete"

	AlertSilenceCreate Action = "alert_silence.create"
	AlertSilenceUpdate Action = "alert_silence.update"
	AlertSilenceDelete Action = "alert_silence.delete"
)

// Spec is the registration of one action.
type Spec struct {
	Action Action
	// TargetType is the target type of an event whose handler staged no
	// target: the resource part of the action.
	TargetType string
	// Staged says that every handler of the action stages its fields, so an
	// event without them is a missing staging line.
	Staged bool
}

// registrations lists every action and whether its handlers stage fields.
// A unit flips Staged when it adds the fields for that action.
var registrations = []struct {
	action Action
	staged bool
}{
	{AlertCreate, false}, {AlertUpdate, false}, {AlertDelete, false}, {AlertResolve, false},
	{APITokenCreate, true}, {APITokenDelete, true},
	{AuditEventExport, false},
	{CollectionCreate, false}, {CollectionUpdate, false}, {CollectionDelete, false},
	{CollectionItemAdd, false}, {CollectionItemUpdate, false}, {CollectionItemRemove, false},
	{CollectionMemberAdd, false}, {CollectionMemberRemove, false},
	{CollectionTeamAdd, false}, {CollectionTeamRemove, false},
	{DashboardCreate, false}, {DashboardUpdate, false}, {DashboardDelete, false},
	{LogExportCreate, true}, {LogExportDownload, false},
	{NotificationDestinationCreate, true}, {NotificationDestinationUpdate, true}, {NotificationDestinationDelete, true}, {NotificationDestinationTest, false},
	{OAuthConsentApprove, true}, {OAuthConsentDeny, true},
	{OAuthGrantRevoke, true},
	{QueryShareCreate, false}, {QueryShareDelete, false},
	{SavedQueryCreate, false}, {SavedQueryUpdate, false}, {SavedQueryDelete, false},
	{ServiceAccountCreate, true}, {ServiceAccountDelete, true},
	{SessionLogin, true}, {SessionLogout, true},
	// A setting test changes no object, so it has a target and a detail but
	// no fields to stage.
	{SettingUpdate, true}, {SettingDelete, true}, {SettingTest, false},
	{SourceCreate, true}, {SourceUpdate, true}, {SourceDelete, true},
	{TeamCreate, true}, {TeamUpdate, true}, {TeamDelete, true},
	{TeamMemberAdd, true}, {TeamMemberRemove, true},
	{TeamSourceLink, true}, {TeamSourceUnlink, true},
	{UserCreate, true}, {UserUpdate, true}, {UserDelete, true},
	{SavedViewCreate, true}, {SavedViewUpdate, true}, {SavedViewDelete, true}, {SavedViewSetDefault, true}, {SavedViewClearDefault, true},
	{ReportCreate, true}, {ReportUpdate, true}, {ReportDelete, false},
	{ReportPause, false}, {ReportResume, false}, {ReportClaim, false},
	{SchemaAdvisorDismissalCreate, true}, {SchemaAdvisorDismissalDelete, true},
	{AlertSilenceCreate, true}, {AlertSilenceUpdate, true}, {AlertSilenceDelete, true},
}

// verbs is the closed list of verbs an action may use.
var verbs = []string{
	"create", "update", "delete", "add", "remove", "link", "unlink", "approve",
	"deny", "revoke", "resolve", "test", "login", "logout", "download", "export",
	"set_default", "clear_default",
	"pause", "resume", "claim",
}

var resourcePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

var registry = buildRegistry()

func buildRegistry() map[Action]Spec {
	specs := make(map[Action]Spec, len(registrations))
	for _, r := range registrations {
		resource, err := parseAction(r.action)
		if err != nil {
			panic(err)
		}
		if _, dup := specs[r.action]; dup {
			panic(fmt.Sprintf("audit: action %q registered twice", r.action))
		}
		specs[r.action] = Spec{Action: r.action, TargetType: resource, Staged: r.staged}
	}
	return specs
}

func parseAction(a Action) (resource string, err error) {
	resource, verb, ok := strings.Cut(string(a), ".")
	if !ok || strings.Contains(verb, ".") || !resourcePattern.MatchString(resource) || !slices.Contains(verbs, verb) {
		return "", fmt.Errorf("audit: action %q is not <resource>.<verb> with a listed verb", a)
	}
	return resource, nil
}

// Lookup returns the registration of a named action. It rejects a name that
// no constant in this file registers.
func Lookup(name string) (Spec, bool) {
	spec, ok := registry[Action(name)]
	return spec, ok
}

// HasResource says whether any registered action names resource.
func HasResource(resource string) bool {
	for _, spec := range registry {
		if spec.TargetType == resource {
			return true
		}
	}
	return false
}
