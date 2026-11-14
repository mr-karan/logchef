package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

var macroQueryConfig = config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60}

func TestValidateTimezone(t *testing.T) {
	for _, name := range []string{"UTC", "Asia/Kolkata", "America/Argentina/Buenos_Aires", "Etc/GMT+5"} {
		location, err := ValidateTimezone(name)
		if err != nil || location == nil || location.String() != name {
			t.Errorf("%q: location %v, error %v", name, location, err)
		}
	}
	if location, err := ValidateTimezone(""); err != nil || location != nil {
		t.Errorf("an absent timezone: location %v, error %v", location, err)
	}
	for _, name := range []string{
		"Asia/Kolkata'", "UTC' OR 1=1 --", "Asia Kolkata", " UTC", "UTC\n", "Local", "Mars/Olympus_Mons", "+05:30",
		"../../etc/passwd", strings.Repeat("A", 65),
	} {
		location, err := ValidateTimezone(name)
		if !isQueryRequestError(err) || location != nil || !strings.Contains(err.Error(), "invalid timezone") {
			t.Errorf("%q: location %v, error %v, want a request error", name, location, err)
		}
	}
}

// TestTimezoneValidatedOnEveryEntryPoint sends a timezone with a quote and one
// with a space through every core entry point that takes one. Each is a
// request error, and no request reaches the provider.
func TestTimezoneValidatedOnEveryEntryPoint(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	admin := &models.User{Email: "tz-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	source := newTestSource(t, db, "tz_src")
	team, err := CreateTeam(ctx, db, log, "tz-team", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, source.ID, models.TokenScopeLogsRead)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{
		compileFn: func(context.Context, *models.Source, datasource.LogchefQLCompileRequest) (*datasource.CompiledLogchefQL, error) {
			return &datasource.CompiledLogchefQL{Language: models.QueryLanguageClickHouseSQL, Query: "SELECT 1", Valid: true, Conditions: []logchefql.FilterCondition{}}, nil
		},
	}
	ds := newFakeDatasourceService(db, log, provider)
	const start, end = "2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z"

	entryPoints := map[string]func(timezone string) error{
		"PrepareSourceSQLQuery": func(tz string) error {
			_, err := PrepareSourceSQLQuery(ctx, ds, macroQueryConfig, src, models.APIQueryRequest{QueryText: "SELECT 1", Timezone: tz})
			return err
		},
		"RunQuery": func(tz string) error {
			_, err := RunQuery(ctx, ds, macroQueryConfig, src, models.APIQueryRequest{QueryText: "SELECT 1", Timezone: tz})
			return err
		},
		"PrepareSourceHistogram": func(tz string) error {
			_, err := PrepareSourceHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: "SELECT 1", Timezone: tz})
			return err
		},
		"RunHistogram": func(tz string) error {
			_, err := RunHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: "SELECT 1", Timezone: tz})
			return err
		},
		"PrepareLogchefQLQuery": func(tz string) error {
			_, err := PrepareLogchefQLQuery(ctx, ds, macroQueryConfig, src, LogchefQLQueryRequest{Query: `level="error"`, StartTime: start, EndTime: end, Timezone: tz})
			return err
		},
		"RunLogchefQL": func(tz string) error {
			_, _, err := RunLogchefQL(ctx, ds, macroQueryConfig, src, LogchefQLQueryRequest{Query: `level="error"`, StartTime: start, EndTime: end, Timezone: tz})
			return err
		},
		"CompileLogchefQL": func(tz string) error {
			_, err := CompileLogchefQL(ctx, ds, src, datasource.LogchefQLCompileRequest{Query: `level="error"`, StartTime: start, EndTime: end, Timezone: tz})
			return err
		},
		"TranslateLogchefQL": func(tz string) error {
			_, err := TranslateLogchefQL(ctx, ds, src, datasource.LogchefQLCompileRequest{Query: `level="error"`, StartTime: start, EndTime: end, Timezone: tz})
			return err
		},
		"GetFieldValues": func(tz string) error {
			_, err := GetFieldValues(ctx, ds, src, FieldValuesParams{FieldName: "level", Timezone: tz})
			return err
		},
		"GetAllFieldValues": func(tz string) error {
			_, err := GetAllFieldValues(ctx, ds, src, AllFieldValuesParams{Timezone: tz})
			return err
		},
		"RenderSourceSQL": func(tz string) error {
			_, err := RenderSourceSQL(source, models.CreateExportJobRequest{RawSQL: "SELECT 1", Timezone: tz})
			return err
		},
	}
	for name, call := range entryPoints {
		for _, timezone := range []string{"Asia/Kolkata'", "Asia Kolkata"} {
			provider.requests = 0
			err := call(timezone)
			reqErr, ok := errors.AsType[*QueryRequestError](err)
			if !ok || !strings.Contains(reqErr.Message, "invalid timezone") {
				t.Errorf("%s with %q: error = %v, want an invalid timezone request error", name, timezone, err)
			}
			if provider.requests != 0 {
				t.Errorf("%s with %q: %d requests reached the provider", name, timezone, provider.requests)
			}
		}
		if err := call("Asia/Kolkata"); err != nil {
			t.Errorf("%s with a valid timezone: %v", name, err)
		}
	}
}

func TestPrepareSQLQueryMacros(t *testing.T) {
	clickhouse := templateTarget{dialect: template.ClickHouseSQL, timestampColumn: "ts"}
	req := models.APIQueryRequest{
		QueryText: "SELECT toStartOfInterval(ts, {{__interval}}, {{__timezone}}) AS t, count() AS n FROM logs WHERE {{__time_filter}} AND service = {{service}} GROUP BY t ORDER BY t",
		StartTime: "2026-11-06T00:00:00+05:30",
		EndTime:   "2026-11-13T00:00:00+05:30",
		Timezone:  "Asia/Kolkata",
		Interval:  "24h",
		Variables: []models.TemplateVariable{{Name: "service", Type: "string", Value: "api"}},
	}
	params, err := prepareSQLQuery(macroQueryConfig, req, clickhouse)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT toStartOfInterval(ts, INTERVAL 1 DAY, 'Asia/Kolkata') AS t, count() AS n FROM logs WHERE " +
		"`ts` >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC') AND `ts` < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC')" +
		" AND service = 'api' GROUP BY t ORDER BY t"
	if params.RawQuery != want {
		t.Fatalf("got  %s\nwant %s", params.RawQuery, want)
	}

	logsql := req
	logsql.QueryText = `service:={{service}} _time:[{{__from}}, {{__to}}) | stats by (_time:{{__interval}} offset {{__tz_offset}}) count() as n`
	params, err = prepareSQLQuery(macroQueryConfig, logsql, templateTarget{dialect: template.LogsQL})
	if err != nil {
		t.Fatal(err)
	}
	want = `service:="api" _time:["2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z") | stats by (_time:1d offset 19800s) count() as n`
	if params.RawQuery != want {
		t.Fatalf("got  %s\nwant %s", params.RawQuery, want)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*models.APIQueryRequest)
		want   string
	}{
		{"no range", func(r *models.APIQueryRequest) { r.StartTime, r.EndTime = "", "" }, "Variable substitution failed: macro {{__time_filter}} needs a time range: send start_time and end_time"},
		{"no interval", func(r *models.APIQueryRequest) { r.Interval = "" }, "Variable substitution failed: macro {{__interval}} needs an interval"},
		{"an interval outside the set", func(r *models.APIQueryRequest) { r.Interval = "90m" }, `invalid interval "90m": use one of 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h, 2h, 3h, 6h, 12h, 24h`},
		{"an interval with SQL in it", func(r *models.APIQueryRequest) { r.Interval = "1 DAY) --" }, `invalid interval "1 DAY) --"`},
		{"a request variable named after a macro", func(r *models.APIQueryRequest) {
			r.Variables = append(r.Variables, models.TemplateVariable{Name: "__from", Type: "string", Value: "x"})
		}, "Variable substitution failed: variable name __from is reserved for a built-in macro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := req
			bad.Variables = append([]models.TemplateVariable(nil), req.Variables...)
			tc.mutate(&bad)
			_, err := prepareSQLQuery(macroQueryConfig, bad, clickhouse)
			reqErr, ok := errors.AsType[*QueryRequestError](err)
			if !ok || !strings.HasPrefix(reqErr.Message, tc.want) {
				t.Fatalf("error = %v, want a request error starting %q", err, tc.want)
			}
		})
	}

	t.Run("an unused interval is still validated", func(t *testing.T) {
		_, err := prepareSQLQuery(macroQueryConfig, models.APIQueryRequest{QueryText: "SELECT 1", Interval: "auto"}, clickhouse)
		if !isQueryRequestError(err) {
			t.Fatalf("error = %v, want a request error", err)
		}
	})
	t.Run("no timezone renders UTC", func(t *testing.T) {
		params, err := prepareSQLQuery(macroQueryConfig, models.APIQueryRequest{QueryText: "SELECT {{__timezone}}"}, clickhouse)
		if err != nil || params.RawQuery != "SELECT 'UTC'" || params.Timezone != "" {
			t.Fatalf("query %q timezone %q error %v", params.RawQuery, params.Timezone, err)
		}
	})
}

func TestPrepareHistogramMacros(t *testing.T) {
	clickhouse := templateTarget{dialect: template.ClickHouseSQL, timestampColumn: "ts"}
	params, err := prepareHistogram(models.APIHistogramRequest{
		QueryText: "SELECT * FROM logs WHERE {{__time_filter}} AND bucket_s = {{__interval_s}}",
		StartTime: "2026-11-05T18:30:00Z", EndTime: "2026-11-12T18:30:00Z", Window: "24h", Timezone: "Asia/Kolkata",
	}, clickhouse)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM logs WHERE `ts` >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC') AND `ts` < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC') AND bucket_s = 86400"
	if params.Query != want || params.Window != "24h" || params.Timezone != "Asia/Kolkata" {
		t.Fatalf("query %q window %q timezone %q", params.Query, params.Window, params.Timezone)
	}

	t.Run("millisecond timestamps feed the macros", func(t *testing.T) {
		params, err := prepareHistogram(models.APIHistogramRequest{QueryText: "SELECT * FROM logs WHERE ts >= {{__from}}", StartTimestamp: 1793903400000, EndTimestamp: 1794508200000}, clickhouse)
		if err != nil || params.Query != "SELECT * FROM logs WHERE ts >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC')" {
			t.Fatalf("query %q error %v", params.Query, err)
		}
		if params.Timezone != "UTC" || params.Window != "1m" {
			t.Fatalf("defaults: timezone %q window %q", params.Timezone, params.Window)
		}
	})
	t.Run("a VictoriaLogs duration outside the table", func(t *testing.T) {
		logsql := templateTarget{dialect: template.LogsQL}
		params, err := prepareHistogram(models.APIHistogramRequest{QueryText: "error", Window: "1h30m"}, logsql)
		if err != nil || params.Window != "1h30m" {
			t.Fatalf("window %q error %v, want the window passed through", params.Window, err)
		}
		_, err = prepareHistogram(models.APIHistogramRequest{QueryText: "error | stats by (_time:{{__interval}}) count()", Window: "1h30m"}, logsql)
		if !isQueryRequestError(err) || !strings.Contains(err.Error(), "macro {{__interval}} needs an interval") {
			t.Fatalf("error = %v, want the missing interval error", err)
		}
	})
	t.Run("a time macro with no range", func(t *testing.T) {
		_, err := prepareHistogram(models.APIHistogramRequest{QueryText: "SELECT * FROM logs WHERE {{__time_filter}}"}, clickhouse)
		if !isQueryRequestError(err) || !strings.Contains(err.Error(), "needs a time range") {
			t.Fatalf("error = %v, want the missing range error", err)
		}
	})
}

func TestRenderSourceSQLMacros(t *testing.T) {
	source := &models.Source{MetaTSField: "event_time"}
	got, err := RenderSourceSQL(source, models.CreateExportJobRequest{
		RawSQL:    "SELECT * FROM logs WHERE {{__time_filter}} AND level = {{level}}",
		StartTime: "2026-11-05T18:30:00Z", EndTime: "2026-11-12T18:30:00Z", Timezone: "Asia/Kolkata",
		Variables: []models.TemplateVariable{{Name: "level", Type: "string", Value: "error"}},
	})
	want := "SELECT * FROM logs WHERE `event_time` >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC') AND `event_time` < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC') AND level = 'error'"
	if err != nil || got != want {
		t.Fatalf("got  %s\nwant %s\nerror %v", got, want, err)
	}
	_, err = RenderSourceSQL(source, models.CreateExportJobRequest{RawSQL: "SELECT * FROM logs WHERE {{__time_filter}}"})
	if !isQueryRequestError(err) || !strings.Contains(err.Error(), "needs a time range") {
		t.Fatalf("error = %v, want the missing range error", err)
	}
}

// The source decides what a macro renders: a ClickHouse source filters on its
// own timestamp column, and a VictoriaLogs source gets the LogsQL forms.
func TestMacrosRenderForTheStoredSource(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	admin := &models.User{Email: "macro-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	team, err := CreateTeam(ctx, db, log, "macro-team", "")
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(source *models.Source) access.AuthorizedSource {
		t.Helper()
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatal(err)
		}
		src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, source.ID, models.TokenScopeLogsRead)
		if err != nil {
			t.Fatal(err)
		}
		return src
	}
	clickhouse := authorize(&models.Source{
		Name: "macro_ch", SourceType: models.SourceTypeClickHouse, MetaTSField: "event_time",
		Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "macro_ch"},
	})
	victoriaLogs := authorize(&models.Source{
		Name: "macro_vl", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time",
		ConnectionConfig: json.RawMessage(`{"base_url":"http://vl:9428"}`),
	})
	ds := datasource.NewService(db, log)
	const start, end = "2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z"
	const sqlFilter = "`event_time` >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC') AND `event_time` < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC')"
	const logsqlRange = `_time:["2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z")`

	query, err := PrepareSourceSQLQuery(ctx, ds, macroQueryConfig, clickhouse, models.APIQueryRequest{QueryText: "SELECT 1 WHERE {{__time_filter}}", StartTime: start, EndTime: end})
	if err != nil || query.RawQuery != "SELECT 1 WHERE "+sqlFilter {
		t.Fatalf("ClickHouse query %q error %v", query.RawQuery, err)
	}
	histogram, err := PrepareSourceHistogram(ctx, ds, clickhouse, models.APIHistogramRequest{QueryText: "SELECT 1 WHERE {{__time_filter}}", StartTime: start, EndTime: end})
	if err != nil || histogram.Query != "SELECT 1 WHERE "+sqlFilter {
		t.Fatalf("ClickHouse histogram %q error %v", histogram.Query, err)
	}

	query, err = PrepareSourceSQLQuery(ctx, ds, macroQueryConfig, victoriaLogs, models.APIQueryRequest{QueryText: "_time:[{{__from}}, {{__to}})", StartTime: start, EndTime: end})
	if err != nil || query.RawQuery != logsqlRange {
		t.Fatalf("VictoriaLogs query %q error %v", query.RawQuery, err)
	}
	histogram, err = PrepareSourceHistogram(ctx, ds, victoriaLogs, models.APIHistogramRequest{QueryText: "_time:[{{__from}}, {{__to}})", StartTime: start, EndTime: end})
	if err != nil || histogram.Query != logsqlRange {
		t.Fatalf("VictoriaLogs histogram %q error %v", histogram.Query, err)
	}
	_, err = PrepareSourceSQLQuery(ctx, ds, macroQueryConfig, victoriaLogs, models.APIQueryRequest{QueryText: "{{__time_filter}}", StartTime: start, EndTime: end})
	if !isQueryRequestError(err) || !strings.Contains(err.Error(), "not available in LogsQL") {
		t.Fatalf("error = %v, want the LogsQL rejection", err)
	}
}
