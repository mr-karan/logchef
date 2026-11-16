package audit

import (
	"sort"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// One view per audited object, sorted by object. A view names every field it
// records and marks every credential with Secret.

// NotificationDestinationFields records a destination. The webhook URL or
// routing key and the external addresses of an email destination are
// secrets.
func NotificationDestinationFields(d *models.NotificationDestination) *Fields {
	userIDs := make([]string, len(d.Config.UserIDs))
	for i, id := range d.Config.UserIDs {
		userIDs[i] = strconv.FormatInt(int64(id), 10)
	}
	return NewFields().
		Set("name", d.Name).
		Set("type", d.Type).
		Set("scope", d.Scope).
		Set("team_id", d.TeamID).
		Set("is_active", d.IsActive).
		Set("config.payload_version", d.Config.PayloadVersion).
		Set("config.user_ids", userIDs).
		Secret("config.addresses", strings.Join(d.Config.Addresses, "\n")).
		Secret("secret", d.Secret)
}

// OAuthConsentFields records the authorization request a user decided:
// the client, the resource kind (api or mcp) and the scopes. grantID is the
// grant that an approval created, nil for a denial. The redirect URI and the
// PKCE challenge are left out.
func OAuthConsentFields(r *models.OAuthAuthRequest, resourceKind string, grantID *models.OAuthGrantID) *Fields {
	return NewFields().
		Set("client_id", r.ClientID).
		Set("resource", resourceKind).
		Set("scopes", r.Scopes).
		Set("offline_access", r.OfflineAccess).
		Set("grant_id", grantID)
}

// OAuthGrantFields records a grant: its user, client, resource kind (api or
// mcp) and scopes. A grant holds no credential; its tokens live elsewhere.
func OAuthGrantFields(g *models.OAuthGrant, resourceKind string) *Fields {
	return NewFields().
		Set("user_id", g.UserID).
		Set("client_id", g.ClientID).
		Set("resource", resourceKind).
		Set("scopes", g.Scopes).
		Set("offline_access", g.OfflineAccess)
}

// ReportFields records a report's configuration: its source by kind and ID,
// its schedule and its options. Variables are recorded by name only, because
// a value can be a literal from a query.
func ReportFields(r *models.Report) *Fields {
	names := make([]string, 0, len(r.Variables))
	for name := range r.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	weekdays := make([]string, len(r.Schedule.Weekdays))
	for i, d := range r.Schedule.Weekdays {
		weekdays[i] = strconv.Itoa(d)
	}
	return NewFields().
		Set("name", r.Name).
		Set("source_kind", r.SourceKind).
		Set("dashboard_id", r.DashboardID).
		Set("tab_id", r.TabID).
		Set("saved_query_id", r.SavedQueryID).
		Set("schedule.frequency", r.Schedule.Frequency).
		Set("schedule.minute", r.Schedule.Minute).
		Set("schedule.hour", r.Schedule.Hour).
		Set("schedule.weekdays", weekdays).
		Set("schedule.day_of_month", r.Schedule.DayOfMonth).
		Set("timezone", r.Timezone).
		Set("time_range", r.TimeRange).
		Set("range_anchor", r.RangeAnchor).
		Set("variable_names", names).
		Set("max_rows", r.MaxRows).
		Set("skip_if_empty", r.SkipIfEmpty).
		Set("attach_csv", r.AttachCSV)
}

// SessionFields records a browser session by its expiry. The session ID is
// a bearer credential and is never recorded.
func SessionFields(s *models.Session) *Fields {
	return NewFields().Set("expires_at", s.ExpiresAt)
}

// SettingFields records a system setting. The value is a secret when the
// setting is sensitive or its key names a credential.
func SettingFields(s *models.SystemSetting) *Fields {
	f := NewFields().
		Set("key", s.Key).
		Set("value_type", s.ValueType).
		Set("category", s.Category).
		Set("description", s.Description).
		Set("is_sensitive", s.IsSensitive)
	if s.IsSensitive || models.IsSensitiveSetting(s.Key) || deniedKey(s.Key) {
		return f.Secret("value", s.Value)
	}
	return f.Set("value", s.Value)
}

// SourceFields records a source and its connection, with the passwords, the
// tokens and the connection headers as secrets, and the VictoriaLogs scope
// query and the Loki selector as digests.
func SourceFields(s *models.Source) *Fields {
	f := NewFields().
		Set("name", s.Name).
		Set("source_type", models.NormalizeSourceType(s.SourceType)).
		Set("description", s.Description).
		Set("ttl_days", s.TTLDays).
		Set("meta_ts_field", s.MetaTSField).
		Set("meta_severity_field", s.MetaSeverityField).
		Set("managed", s.Managed)
	if s.IsVictoriaLogs() {
		return victoriaLogsConnectionFields(f, s)
	}
	if s.IsLoki() {
		return lokiConnectionFields(f, s)
	}
	c := s.Connection
	f.Set("connection.host", c.Host).
		Set("connection.username", c.Username).
		Secret("connection.password", c.Password).
		Set("connection.database", c.Database).
		Set("connection.table_name", c.TableName).
		Set("connection.tls_enable", c.TLSEnable)
	if st := c.Settings; st != nil {
		f.Set("connection.settings.max_execution_time", st.MaxExecutionTime).
			Set("connection.settings.max_result_rows", st.MaxResultRows).
			Set("connection.settings.max_result_bytes", st.MaxResultBytes).
			Set("connection.settings.max_rows_to_read", st.MaxRowsToRead).
			Set("connection.settings.max_bytes_to_read", st.MaxBytesToRead).
			Set("connection.settings.readonly", st.Readonly).
			Set("connection.settings.result_overflow_mode", st.ResultOverflowMode)
	}
	if a := c.Activity; a != nil {
		f.Set("connection.activity.allow_view", a.AllowView).
			Set("connection.activity.max_rows_to_read", a.MaxRowsToRead).
			Set("connection.activity.max_bytes_to_read", a.MaxBytesToRead)
	}
	return f
}

func victoriaLogsConnectionFields(f *Fields, s *models.Source) *Fields {
	c, err := s.VictoriaLogsConnection()
	if err != nil {
		return f.Set("connection.unreadable", true)
	}
	f.URL("connection.base_url", c.BaseURL).
		Set("connection.auth.mode", c.Auth.Mode).
		Set("connection.auth.username", c.Auth.Username).
		Secret("connection.auth.password", c.Auth.Password).
		Secret("connection.auth.token", c.Auth.Token).
		Set("connection.tenant.account_id", c.Tenant.AccountID).
		Set("connection.tenant.project_id", c.Tenant.ProjectID).
		Digest("connection.scope.query", c.Scope.Query).
		Secret("connection.headers", headersFingerprint(c.Headers))
	if o := c.Optimizer; o != nil {
		f.Set("connection.optimizer.enabled", o.Enabled)
	}
	return f
}

func lokiConnectionFields(f *Fields, s *models.Source) *Fields {
	c, err := s.LokiConnection()
	if err != nil {
		return f.Set("connection.unreadable", true)
	}
	f.URL("connection.base_url", c.BaseURL).
		Set("connection.auth.mode", c.Auth.Mode).
		Set("connection.auth.username", c.Auth.Username).
		Secret("connection.auth.password", c.Auth.Password).
		Secret("connection.auth.token", c.Auth.Token).
		Set("connection.tenant_id", c.TenantID).
		Digest("connection.selector", c.Selector).
		Set("connection.principal", c.Principal).
		Set("connection.parser", c.Parser).
		Secret("connection.headers", headersFingerprint(c.Headers))
	if l := c.Limits; l != nil {
		f.Set("connection.limits.max_entries_per_query", l.MaxEntriesPerQuery).
			Set("connection.limits.sidebar_lookback_seconds", l.SidebarLookbackSeconds).
			Set("connection.limits.schema_lookback_seconds", l.SchemaLookbackSeconds)
	}
	return f
}

// headersFingerprint is a stable encoding of the headers, so that Secret can
// tell whether any header changed without recording one.
func headersFingerprint(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(strconv.Quote(k) + "=" + strconv.Quote(headers[k]) + "\n")
	}
	return b.String()
}

// SavedViewFields records a saved view. The filter is LogchefQL text, so it
// is recorded as its language and a digest.
func SavedViewFields(v *models.SavedView) *Fields {
	d := v.Definition
	f := NewFields().
		Set("name", v.Name).
		Set("scope", v.Scope).
		Set("source_id", v.SourceID).
		Set("team_id", v.TeamID).
		Set("revision", v.Revision).
		Set("is_default", v.IsDefault).
		Set("column_count", len(d.Columns)).
		Set("display_mode", d.DisplayMode).
		Set("time_range", d.TimeRange)
	if d.Filter == nil {
		return f.Set("filter_language", nil)
	}
	return f.Set("filter_language", models.QueryLanguageLogchefQL).Digest("filter", *d.Filter)
}

// APITokenFields records a token's name, owner, scopes and expiry. The hash
// and the display prefix, which holds the start of the token, are left out.
func APITokenFields(t *models.APIToken) *Fields {
	return NewFields().
		Set("name", t.Name).
		Set("user_id", t.UserID).
		Set("scopes", t.Scopes).
		Set("expires_at", t.ExpiresAt)
}

// TeamFields records a team.
func TeamFields(t *models.Team) *Fields {
	return NewFields().
		Set("name", t.Name).
		Set("description", t.Description).
		Set("managed", t.Managed)
}

// TeamMemberFields records a membership and its role.
func TeamMemberFields(m *models.TeamMember) *Fields {
	return NewFields().
		Set("team_id", m.TeamID).
		Set("user_id", m.UserID).
		Set("role", m.Role)
}

// TeamSourceFields records a link between a team and a source, which has no
// fields of its own.
func TeamSourceFields(teamID models.TeamID, sourceID models.SourceID) *Fields {
	return NewFields().
		Set("team_id", teamID).
		Set("source_id", sourceID)
}

// UserFields records a user or a service account. The password hash and the
// sign-in times are left out.
func UserFields(u *models.User) *Fields {
	return NewFields().
		Set("email", u.Email).
		Set("full_name", u.FullName).
		Set("role", u.Role).
		Set("status", u.Status).
		Set("account_type", u.AccountType).
		Set("managed", u.Managed)
}

// LogExportFields records an export: the query language, the format, the
// row limit that applies, and the query as a digest, so a literal inside it
// never reaches the event.
func LogExportFields(language models.QueryLanguage, format string, rowLimit int, query string) *Fields {
	return NewFields().
		Set("language", language).
		Set("format", format).
		Set("row_limit", rowLimit).
		Digest("query", query)
}

// SchemaAdvisorDismissalFields records a dismissed schema advisor finding:
// its id and the admin's reason. The target is the source.
func SchemaAdvisorDismissalFields(d *models.SchemaAdvisorDismissal) *Fields {
	return NewFields().
		Set("finding_id", d.FindingID).
		Set("reason", d.Reason)
}

// AlertSilenceFields records a silence: its rule, matchers, comment and
// schedule. Matchers are recorded as "label=value", sorted.
func AlertSilenceFields(s *models.AlertSilence) *Fields {
	matchers := make([]string, 0, len(s.Matchers))
	for key, value := range s.Matchers {
		matchers = append(matchers, key+"="+value)
	}
	sort.Strings(matchers)
	f := NewFields().
		Set("source_id", s.SourceID).
		Set("alert_id", s.AlertID).
		Set("matchers", matchers).
		Set("comment", s.Comment).
		Set("schedule_kind", s.ScheduleKind).
		Set("starts_at", s.StartsAt).
		Set("ends_at", s.EndsAt)
	if s.Weekly != nil {
		days := make([]string, len(s.Weekly.Weekdays))
		for i, day := range s.Weekly.Weekdays {
			days[i] = strconv.Itoa(day)
		}
		f.Set("weekly.weekdays", days).
			Set("weekly.start", s.Weekly.Start).
			Set("weekly.duration_minutes", s.Weekly.DurationMinutes).
			Set("weekly.timezone", s.Weekly.Timezone)
	}
	return f
}
