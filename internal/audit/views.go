package audit

import (
	"sort"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// One view per audited object, sorted by object. A view names every field it
// records and marks every credential with Secret.

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

// SourceFields records a source and its connection, with the password, the
// VictoriaLogs token and the connection headers as secrets and the scope
// query as a digest.
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
