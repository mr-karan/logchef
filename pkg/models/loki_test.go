package models

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validLokiConnection() LokiConnectionInfo {
	return LokiConnectionInfo{
		BaseURL:  "http://loki.test:3100",
		TenantID: "dev",
		Selector: `{service_name=~".+"}`,
	}
}

func TestLokiConnectionValidate(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(*LokiConnectionInfo)
		wantField string
	}{
		{"minimal", func(*LokiConnectionInfo) {}, ""},
		{"https with path", func(c *LokiConnectionInfo) { c.BaseURL = "https://gateway.test/loki-prod" }, ""},
		{"missing base url", func(c *LokiConnectionInfo) { c.BaseURL = " " }, "base_url"},
		{"non http base url", func(c *LokiConnectionInfo) { c.BaseURL = "ftp://loki.test" }, "base_url"},
		{"base url without host", func(c *LokiConnectionInfo) { c.BaseURL = "http://" }, "base_url"},
		{"base url with credentials", func(c *LokiConnectionInfo) { c.BaseURL = "https://reader:synthetic-password@loki.test" }, "base_url"},
		{"base url with a user name", func(c *LokiConnectionInfo) { c.BaseURL = "https://reader@loki.test" }, "base_url"},
		{"unset auth mode is none", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{} }, ""},
		{"basic auth", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "basic", Username: "u", Password: "p"} }, ""},
		{"basic auth without password", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "basic", Username: "u"} }, "auth.password"},
		{"basic auth without username", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "basic", Password: "p"} }, "auth.username"},
		{"bearer auth", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "bearer", Token: "t"} }, ""},
		{"bearer auth without token", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "bearer"} }, "auth.token"},
		{"auto auth mode", func(c *LokiConnectionInfo) { c.Auth = LokiAuth{Mode: "auto"} }, "auth.mode"},
		{"no tenant", func(c *LokiConnectionInfo) { c.TenantID = "" }, ""},
		{"tenant with every allowed character", func(c *LokiConnectionInfo) { c.TenantID = "Team-1_a.b*c'(d)!" }, ""},
		{"multi tenant", func(c *LokiConnectionInfo) { c.TenantID = "dev|prod" }, ""},
		{"empty tenant between pipes", func(c *LokiConnectionInfo) { c.TenantID = "dev||prod" }, "tenant_id"},
		{"tenant with a slash", func(c *LokiConnectionInfo) { c.TenantID = "dev/prod" }, "tenant_id"},
		{"tenant dot", func(c *LokiConnectionInfo) { c.TenantID = "." }, "tenant_id"},
		{"tenant dot dot", func(c *LokiConnectionInfo) { c.TenantID = "dev|.." }, "tenant_id"},
		{"tenant of 150 bytes", func(c *LokiConnectionInfo) { c.TenantID = strings.Repeat("a", 150) }, ""},
		{"tenant of 151 bytes", func(c *LokiConnectionInfo) { c.TenantID = strings.Repeat("a", 151) }, "tenant_id"},
		{"missing selector", func(c *LokiConnectionInfo) { c.Selector = "" }, "selector"},
		{"selector without braces", func(c *LokiConnectionInfo) { c.Selector = `app="api"` }, "selector"},
		{"empty selector", func(c *LokiConnectionInfo) { c.Selector = `{}` }, "selector"},
		{"selector with spaces and several matchers", func(c *LokiConnectionInfo) { c.Selector = ` { app = "api" , env!~"dev|test" } ` }, ""},
		{"selector with a raw string regex", func(c *LokiConnectionInfo) { c.Selector = "{app=~`pay.*`}" }, ""},
		{"selector with an escaped quote", func(c *LokiConnectionInfo) { c.Selector = `{msg="say \"hi\""}` }, ""},
		{"selector with a pipeline", func(c *LokiConnectionInfo) { c.Selector = `{app="api"} |= "error"` }, "selector"},
		{"two selectors", func(c *LokiConnectionInfo) { c.Selector = `{app="api"}{env="prod"}` }, "selector"},
		{"trailing comma", func(c *LokiConnectionInfo) { c.Selector = `{app="api",}` }, "selector"},
		{"unquoted value", func(c *LokiConnectionInfo) { c.Selector = `{app=api}` }, "selector"},
		{"unterminated value", func(c *LokiConnectionInfo) { c.Selector = `{app="api}` }, "selector"},
		{"invalid label name", func(c *LokiConnectionInfo) { c.Selector = `{app.name="api"}` }, "selector"},
		{"unknown operator", func(c *LokiConnectionInfo) { c.Selector = `{app=="api"}` }, "selector"},
		{"invalid regex", func(c *LokiConnectionInfo) { c.Selector = `{app=~"("}` }, "selector"},
		{"only an equality with empty", func(c *LokiConnectionInfo) { c.Selector = `{app=""}` }, "selector"},
		{"only a regex that matches empty", func(c *LokiConnectionInfo) { c.Selector = `{app=~".*"}` }, "selector"},
		{"only a negated regex that matches empty", func(c *LokiConnectionInfo) { c.Selector = `{app!~".+"}` }, "selector"},
		{"only a negated value", func(c *LokiConnectionInfo) { c.Selector = `{app!="api"}` }, "selector"},
		{"not empty", func(c *LokiConnectionInfo) { c.Selector = `{app!=""}` }, ""},
		{"an empty matcher beside a non-empty one", func(c *LokiConnectionInfo) { c.Selector = `{env=~".*", app="api"}` }, ""},
		{"principal", func(c *LokiConnectionInfo) { c.Principal = "payments-readonly.v2_1" }, ""},
		{"principal with a space", func(c *LokiConnectionInfo) { c.Principal = "payments readonly" }, "principal"},
		{"principal of 65 characters", func(c *LokiConnectionInfo) { c.Principal = strings.Repeat("p", 65) }, "principal"},
		{"json parser", func(c *LokiConnectionInfo) { c.Parser = "json" }, ""},
		{"logfmt parser", func(c *LokiConnectionInfo) { c.Parser = "logfmt" }, ""},
		{"regexp parser", func(c *LokiConnectionInfo) { c.Parser = "regexp" }, "parser"},
		{"custom header", func(c *LokiConnectionInfo) { c.Headers = map[string]string{"X-Gateway-Key": "k"} }, ""},
		{"authorization header", func(c *LokiConnectionInfo) { c.Headers = map[string]string{"Authorization": "Bearer x"} }, "headers"},
		{"tenant header in any case", func(c *LokiConnectionInfo) { c.Headers = map[string]string{" x-scope-orgid ": "other"} }, "headers"},
		{"limits at their bounds", func(c *LokiConnectionInfo) {
			c.Limits = &LokiLimits{MaxEntriesPerQuery: 100, SidebarLookbackSeconds: 86400, SchemaLookbackSeconds: 60}
		}, ""},
		{"entries per query below the range", func(c *LokiConnectionInfo) { c.Limits = &LokiLimits{MaxEntriesPerQuery: 99} }, "limits.max_entries_per_query"},
		{"entries per query above the range", func(c *LokiConnectionInfo) { c.Limits = &LokiLimits{MaxEntriesPerQuery: 50001} }, "limits.max_entries_per_query"},
		{"sidebar lookback below the range", func(c *LokiConnectionInfo) { c.Limits = &LokiLimits{SidebarLookbackSeconds: 59} }, "limits.sidebar_lookback_seconds"},
		{"schema lookback above the range", func(c *LokiConnectionInfo) { c.Limits = &LokiLimits{SchemaLookbackSeconds: 86401} }, "limits.schema_lookback_seconds"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := validLokiConnection()
			tc.edit(&conn)
			err := conn.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			var fieldErr *LokiConnectionError
			if !errors.As(err, &fieldErr) || fieldErr.Field != tc.wantField {
				t.Fatalf("Validate() = %v, want an error on %q", err, tc.wantField)
			}
		})
	}
}

func TestLokiLimitsDefaults(t *testing.T) {
	want := LokiLimits{MaxEntriesPerQuery: 5000, SidebarLookbackSeconds: 900, SchemaLookbackSeconds: 3600}
	if got := (LokiConnectionInfo{}).EffectiveLimits(); got != want {
		t.Fatalf("EffectiveLimits() with no limits = %+v, want %+v", got, want)
	}
	partial := LokiConnectionInfo{Limits: &LokiLimits{MaxEntriesPerQuery: 1000}}
	want.MaxEntriesPerQuery = 1000
	if got := partial.EffectiveLimits(); got != want {
		t.Fatalf("EffectiveLimits() with one limit = %+v, want %+v", got, want)
	}
}

func lokiSource(t *testing.T, conn LokiConnectionInfo) *Source {
	t.Helper()
	raw, err := json.Marshal(conn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	src := &Source{SourceType: SourceTypeLoki, ConnectionConfig: raw}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	return src
}

func TestLokiIdentityKey(t *testing.T) {
	conn := validLokiConnection()
	conn.BaseURL = "HTTP://Loki.Test:3100/"
	conn.Principal = "payments-readonly"
	conn.Auth = LokiAuth{Mode: "basic", Username: "reader", Password: "basic-secret", Token: "token-secret"}
	conn.Headers = map[string]string{"X-Gateway-Key": "header-secret"}

	key := lokiSource(t, conn).IdentityKey
	if want := `loki:http://loki.test:3100|tenant=dev|selector={service_name=~".+"}|principal=payments-readonly`; key != want {
		t.Fatalf("identity key = %q, want %q", key, want)
	}
	for _, secret := range []string{"basic-secret", "token-secret", "header-secret"} {
		if strings.Contains(key, secret) {
			t.Fatalf("identity key %q holds the secret %q", key, secret)
		}
	}

	rotated := conn
	rotated.Auth.Password = "rotated"
	rotated.Headers = map[string]string{"X-Gateway-Key": "rotated"}
	if got := lokiSource(t, rotated).IdentityKey; got != key {
		t.Fatalf("credential rotation changed the identity key: %q -> %q", key, got)
	}

	otherPrincipal := conn
	otherPrincipal.Principal = "payments-admin"
	if got := lokiSource(t, otherPrincipal).IdentityKey; got == key {
		t.Fatalf("connections that differ only in principal share the identity key %q", key)
	}
	otherSelector := conn
	otherSelector.Selector = `{namespace="payments"}`
	if got := lokiSource(t, otherSelector).IdentityKey; got == key {
		t.Fatalf("connections that differ only in selector share the identity key %q", key)
	}
	otherTenant := conn
	otherTenant.TenantID = "prod"
	if got := lokiSource(t, otherTenant).IdentityKey; got == key {
		t.Fatalf("connections that differ only in tenant share the identity key %q", key)
	}
}

func TestLokiRedactedConnectionConfig(t *testing.T) {
	conn := validLokiConnection()
	conn.Principal = "payments-readonly"
	conn.Parser = "json"
	conn.Auth = LokiAuth{Mode: "basic", Username: "reader", Password: "basic-secret", Token: "token-secret"}
	conn.Headers = map[string]string{"X-Gateway-Key": "header-secret", "X-Other": "other-secret"}
	src := lokiSource(t, conn)
	if err := src.HydrateConnection(); err != nil {
		t.Fatalf("HydrateConnection: %v", err)
	}

	raw := src.ToResponse().Connection
	for _, secret := range []string{"basic-secret", "token-secret", "header-secret", "other-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("redacted connection %s holds the secret %q", raw, secret)
		}
	}
	var got LokiConnectionInfo
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal redacted connection: %v", err)
	}
	if got.Auth.Username != "reader" || got.Auth.Mode != "basic" || got.Selector != conn.Selector ||
		got.TenantID != "dev" || got.Principal != "payments-readonly" || got.Parser != "json" {
		t.Fatalf("redaction dropped a field that is not a secret: %+v", got)
	}
	if len(got.Headers) != 2 || got.Headers["X-Gateway-Key"] != "" || got.Headers["X-Other"] != "" {
		t.Fatalf("redacted headers = %v, want both names with blank values", got.Headers)
	}
}

func TestLokiTypeAndLanguage(t *testing.T) {
	if !SourceTypeLoki.Valid() {
		t.Fatal("SourceTypeLoki is not valid")
	}
	if !QueryLanguageLogQL.Valid() || NormalizeQueryLanguage("logql") != QueryLanguageLogQL {
		t.Fatal("logql is not a valid query language")
	}
	for _, mode := range []AlertEditorMode{AlertEditorModeNative, AlertEditorModeCondition} {
		language, gotMode, err := ResolveAlertMetadata(QueryLanguageLogQL, mode)
		if err != nil || language != QueryLanguageLogQL || gotMode != mode {
			t.Fatalf("ResolveAlertMetadata(logql, %s) = %s, %s, %v", mode, language, gotMode, err)
		}
	}
	if _, _, err := ResolveAlertMetadata(QueryLanguageLogchefQL, AlertEditorModeNative); err == nil {
		t.Fatal("ResolveAlertMetadata accepted logchefql")
	}
}
