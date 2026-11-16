package loki

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestDescribeListsOnlyWorkingCapabilities(t *testing.T) {
	d := testProvider().Describe()
	want := []datasource.Capability{datasource.CapabilitySchemaInspection, datasource.CapabilityHistogram, datasource.CapabilitySourceInspection}
	if !slices.Equal(d.Capabilities, want) {
		t.Fatalf("capabilities %v", d.Capabilities)
	}
	if !slices.Equal(d.QueryLanguages, []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageLogQL}) {
		t.Fatalf("languages %v", d.QueryLanguages)
	}
	if !slices.Equal(d.SavedQueryEditorModes, []models.SavedQueryEditorMode{models.SavedQueryEditorModeBuilder, models.SavedQueryEditorModeNative}) {
		t.Fatalf("editor modes %v", d.SavedQueryEditorModes)
	}
	if !d.LogchefQLNeedsColumns {
		t.Fatal("LogchefQL compilation must receive the columns, to know the stream labels")
	}
	if d.AlertEditorModes == nil || len(d.AlertEditorModes) != 0 {
		t.Fatalf("alert modes %v, want an empty list", d.AlertEditorModes)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		check       func(t *testing.T, err error)
	}{
		{"parse error", 400, "text/plain; charset=utf-8", "parse error at line 1, col 7: syntax error", func(t *testing.T, err error) {
			assertValidation(t, err, "parse error at line 1, col 7: syntax error")
		}},
		{"limit above the tenant's", 400, "text/plain", "max entries limit per query exceeded, limit > max_entries_limit_per_query (6000 > 5000)", func(t *testing.T, err error) {
			assertValidation(t, err, "(6000 > 5000). Lower the source's entries per request")
		}},
		{"not found", 404, "text/plain", "404 page not found", func(t *testing.T, err error) { assertValidation(t, err, "404 page not found") }},
		{"html body is not shown", 400, "text/html", "<html>internal 10.0.0.1</html>", func(t *testing.T, err error) {
			assertValidation(t, err, "Loki rejected the request with status 400")
		}},
		{"long body is cut", 422, "text/plain", strings.Repeat("x", 900), func(t *testing.T, err error) {
			assertValidation(t, err, strings.Repeat("x", 500)+"...")
		}},
		{"unauthorized", 401, "text/plain", "no org id", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamAuth, 401) }},
		{"forbidden", 403, "text/plain", "denied", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamAuth, 403) }},
		{"busy", 429, "text/plain", "too many outstanding requests", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamBusy, 429) }},
		{"server error", 500, "text/plain", "boom", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamUnavailable, 500) }},
		{"gateway timeout", 504, "text/plain", "timeout", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamUnavailable, 504) }},
		{"malformed body", 200, "application/json", "{not json", func(t *testing.T, err error) { assertUpstream(t, err, datasource.UpstreamUnavailable, 200) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
			start, end := timeRange(time.Now(), time.Hour)
			_, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: `{app="api"}`, StartTime: start, EndTime: end})
			tt.check(t, err)
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()
		source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
		start, end := timeRange(time.Now(), time.Hour)
		_, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: `{app="api"}`, StartTime: start, EndTime: end})
		assertUpstream(t, err, datasource.UpstreamUnavailable, 0)
	})
}

func assertValidation(t *testing.T, err error, message string) {
	t.Helper()
	validation, ok := errors.AsType[*datasource.ValidationError](err)
	if !ok || !strings.HasSuffix(validation.Message, message) {
		t.Fatalf("error %v, want a validation error ending %q", err, message)
	}
}

func assertUpstream(t *testing.T, err error, kind datasource.UpstreamKind, status int) {
	t.Helper()
	upstream, ok := errors.AsType[*datasource.UpstreamError](err)
	if !ok || upstream.Kind != kind || upstream.Status != status {
		t.Fatalf("error %v, want upstream %s %d", err, kind, status)
	}
	if strings.Contains(upstream.Message, "boom") || strings.Contains(upstream.Message, "denied") {
		t.Fatalf("message %q shows the upstream body", upstream.Message)
	}
}

// A gateway that echoes the credential in its error body must not put it in
// the message users see or in the error the server logs.
func TestErrorBodiesNeverHoldTheCredential(t *testing.T) {
	const token, password, headerValue = "synthetic-token-123", "synthetic-password-456", "synthetic-key-789"
	for _, conn := range []models.LokiConnectionInfo{
		{Auth: models.LokiAuth{Mode: "bearer", Token: token}, Headers: map[string]string{"X-Gateway-Key": headerValue}},
		{Auth: models.LokiAuth{Mode: "basic", Username: "reader", Password: password}},
	} {
		for _, status := range []int{400, 401, 503} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "invalid credential "+r.Header.Get("Authorization")+" key "+r.Header.Get("X-Gateway-Key"))
			}))
			conn.BaseURL = server.URL
			_, err := testProvider().get(t.Context(), conn, "/loki/api/v1/labels", nil, 1024)
			server.Close()
			if err == nil {
				t.Fatalf("status %d: want an error", status)
			}
			for _, secret := range []string{token, password, headerValue, "cmVhZGVyOnN5bnRoZXRpYy1wYXNzd29yZC00NTY="} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("status %d: error %q holds a secret", status, err)
				}
			}
			if !strings.Contains(err.Error(), "[redacted]") {
				t.Fatalf("status %d: error %q, want the body kept with the secret redacted", status, err)
			}
		}
	}
}

func TestRequestHeaders(t *testing.T) {
	tests := []struct {
		name string
		conn models.LokiConnectionInfo
		want map[string]string
	}{
		{"no auth", models.LokiConnectionInfo{}, map[string]string{"Authorization": "", "X-Scope-Orgid": ""}},
		{"unset mode sends no stale credential", models.LokiConnectionInfo{Auth: models.LokiAuth{Token: "stale"}}, map[string]string{"Authorization": ""}},
		{"bearer", models.LokiConnectionInfo{Auth: models.LokiAuth{Mode: "bearer", Token: "tok"}}, map[string]string{"Authorization": "Bearer tok"}},
		{"basic", models.LokiConnectionInfo{Auth: models.LokiAuth{Mode: "basic", Username: "u", Password: "p"}}, map[string]string{"Authorization": "Basic dTpw"}},
		{"tenant", models.LokiConnectionInfo{TenantID: "dev|prod"}, map[string]string{"X-Scope-Orgid": "dev|prod"}},
		{"custom headers cannot override auth or tenant", models.LokiConnectionInfo{
			TenantID: "dev",
			Auth:     models.LokiAuth{Mode: "bearer", Token: "tok"},
			Headers:  map[string]string{"X-Gateway-Key": "k", "authorization": "Bearer evil", "X-Scope-OrgID": "other"},
		}, map[string]string{"X-Gateway-Key": "k", "Authorization": "Bearer tok", "X-Scope-Orgid": "dev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				_, _ = io.WriteString(w, `{"status":"success","data":[]}`)
			}))
			defer server.Close()
			tt.conn.BaseURL = server.URL
			if _, err := testProvider().get(t.Context(), tt.conn, "/loki/api/v1/labels", nil, 1024); err != nil {
				t.Fatal(err)
			}
			for key, want := range tt.want {
				if got.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, got.Get(key), want)
				}
			}
		})
	}
}

func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked = true }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved/loki/api/v1/labels" {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		target := other.URL + r.URL.Path
		if r.URL.Query().Get("same") == "1" {
			target = "/moved" + r.URL.Path
		}
		http.Redirect(w, r, target, http.StatusFound)
	}))
	defer server.Close()
	conn := models.LokiConnectionInfo{BaseURL: server.URL, Auth: models.LokiAuth{Mode: "bearer", Token: "tok"}}

	if _, err := testProvider().get(t.Context(), conn, "/loki/api/v1/labels", lokiParams{"same": {"1"}}, 1024); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	_, err := testProvider().get(t.Context(), conn, "/loki/api/v1/labels", nil, 1024)
	if err == nil || leaked {
		t.Fatalf("error %v leaked %v, want the redirect refused", err, leaked)
	}
}

// probeLoki answers the three validation calls; each handler may be replaced.
type probeLoki struct {
	buildinfo, labels, queryRange http.HandlerFunc
}

func (p probeLoki) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := map[string]http.HandlerFunc{
		"/loki/api/v1/status/buildinfo": p.buildinfo,
		"/loki/api/v1/labels":           p.labels,
		"/loki/api/v1/query_range":      p.queryRange,
	}[r.URL.Path]
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	handler(w, r)
}

func okProbe() probeLoki {
	return probeLoki{
		buildinfo: func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"version":"3.7.6"}`) },
		labels: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"status":"success","data":["app"]}`)
		},
		queryRange: func(w http.ResponseWriter, _ *http.Request) { writeStreams(w, nil) },
	}
}

func textStatus(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestValidateConnection(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(*probeLoki)
		wantField string
		wantText  string
	}{
		{"accepted", func(*probeLoki) {}, "", "Loki 3.7.6 accepted"},
		{"old version warns", func(p *probeLoki) {
			p.buildinfo = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"version":"2.9.4"}`) }
		}, "", "needs Loki 3.0 or later"},
		{"tenant rejected", func(p *probeLoki) { p.labels = textStatus(401, "no org id") }, "connection.auth", "credentials or tenant"},
		{"selector rejected", func(p *probeLoki) { p.queryRange = textStatus(400, "parse error at line 1, col 2") }, "connection.selector", "parse error at line 1, col 2"},
		{"loki down", func(p *probeLoki) { p.buildinfo = textStatus(503, "unavailable") }, "connection.base_url", "Failed to reach Loki"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := okProbe()
			tt.edit(&probe)
			var sawSelector string
			queryRange := probe.queryRange
			probe.queryRange = func(w http.ResponseWriter, r *http.Request) {
				sawSelector = r.URL.Query().Get("query")
				queryRange(w, r)
			}
			server := httptest.NewServer(probe)
			defer server.Close()
			raw := mustJSON(t, models.LokiConnectionInfo{BaseURL: server.URL, TenantID: "dev", Selector: `{app="api"}`})

			result, err := testProvider().ValidateConnection(t.Context(), &models.ValidateConnectionRequest{SourceType: models.SourceTypeLoki, Connection: raw})
			if tt.wantField == "" {
				if err != nil || !strings.Contains(result.Message, tt.wantText) {
					t.Fatalf("result %+v error %v", result, err)
				}
				if sawSelector != `{app="api"}` {
					t.Fatalf("selector probe sent %q", sawSelector)
				}
				return
			}
			validation, ok := errors.AsType[*datasource.ValidationError](err)
			if !ok || validation.Field != tt.wantField || !strings.Contains(validation.Message, tt.wantText) {
				t.Fatalf("error %v, want %s: %s", err, tt.wantField, tt.wantText)
			}
		})
	}
}

func TestPrepareSourceDefaults(t *testing.T) {
	server := httptest.NewServer(okProbe())
	defer server.Close()
	raw := mustJSON(t, models.LokiConnectionInfo{BaseURL: server.URL, Selector: `{app="api"}`, Principal: "readonly"})
	source, err := testProvider().PrepareSource(t.Context(), &models.CreateSourceRequest{Name: "loki", SourceType: models.SourceTypeLoki, Connection: raw})
	if err != nil {
		t.Fatal(err)
	}
	if source.MetaTSField != "_time" || source.MetaSeverityField != "detected_level" {
		t.Fatalf("meta fields %q %q", source.MetaTSField, source.MetaSeverityField)
	}
	if !strings.HasSuffix(source.IdentityKey, "|principal=readonly") {
		t.Fatalf("identity %q", source.IdentityKey)
	}

	bad := mustJSON(t, models.LokiConnectionInfo{BaseURL: server.URL, Selector: `{app=""}`})
	_, err = testProvider().PrepareSource(t.Context(), &models.CreateSourceRequest{Name: "loki", SourceType: models.SourceTypeLoki, Connection: bad})
	validation, ok := errors.AsType[*datasource.ValidationError](err)
	if !ok || validation.Field != "connection.selector" {
		t.Fatalf("error %v, want a selector error", err)
	}
}

func TestUpdateSource(t *testing.T) {
	server := httptest.NewServer(okProbe())
	defer server.Close()
	stored := models.LokiConnectionInfo{
		BaseURL: server.URL, TenantID: "dev", Selector: `{app="api"}`, Principal: "ro",
		Auth:    models.LokiAuth{Mode: "basic", Username: "u", Password: "stored-password"},
		Headers: map[string]string{"X-Gateway-Key": "stored-key"},
	}
	update := func(t *testing.T, next models.LokiConnectionInfo) (models.LokiConnectionInfo, error) {
		t.Helper()
		source := testSource(t, stored)
		raw := mustJSON(t, next)
		_, err := testProvider().UpdateSource(t.Context(), source, &models.UpdateSourceRequest{Connection: raw})
		if err != nil {
			return models.LokiConnectionInfo{}, err
		}
		return source.LokiConnection()
	}
	redacted := stored
	redacted.Auth.Password = ""
	redacted.Headers = map[string]string{"X-Gateway-Key": ""}

	t.Run("blank secrets keep the stored values", func(t *testing.T) {
		got, err := update(t, redacted)
		if err != nil || got.Auth.Password != "stored-password" || got.Headers["X-Gateway-Key"] != "stored-key" {
			t.Fatalf("got %+v error %v", got, err)
		}
	})
	t.Run("switching mode drops the old credential", func(t *testing.T) {
		next := redacted
		next.Auth = models.LokiAuth{Mode: "bearer", Token: "new-token"}
		got, err := update(t, next)
		if err != nil || got.Auth.Password != "" || got.Auth.Token != "new-token" {
			t.Fatalf("got %+v error %v", got, err)
		}
	})
	t.Run("parser and limits change", func(t *testing.T) {
		next := redacted
		next.Parser = "json"
		next.Limits = &models.LokiLimits{MaxEntriesPerQuery: 1000}
		got, err := update(t, next)
		if err != nil || got.Parser != "json" || got.Limits.MaxEntriesPerQuery != 1000 {
			t.Fatalf("got %+v error %v", got, err)
		}
	})
	t.Run("an equivalent base URL is allowed", func(t *testing.T) {
		next := redacted
		next.BaseURL = server.URL + "/"
		if _, err := update(t, next); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		field string
		edit  func(*models.LokiConnectionInfo)
	}{
		{"connection.base_url", func(c *models.LokiConnectionInfo) { c.BaseURL = "http://other.test:3100" }},
		{"connection.tenant_id", func(c *models.LokiConnectionInfo) { c.TenantID = "prod" }},
		{"connection.selector", func(c *models.LokiConnectionInfo) { c.Selector = `{app="web"}` }},
		{"connection.principal", func(c *models.LokiConnectionInfo) { c.Principal = "admin" }},
	} {
		t.Run("immutable "+tc.field, func(t *testing.T) {
			next := redacted
			tc.edit(&next)
			_, err := update(t, next)
			validation, ok := errors.AsType[*datasource.ValidationError](err)
			if !ok || validation.Field != tc.field {
				t.Fatalf("error %v, want %s rejected", err, tc.field)
			}
		})
	}
}

func TestLifecycle(t *testing.T) {
	server := httptest.NewServer(okProbe())
	defer server.Close()
	p := testProvider()
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	if err := p.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if !p.CachedSourceConnectionStatus(source.ID) || !p.CheckSourceConnectionStatus(t.Context(), source) {
		t.Fatal("want a healthy source")
	}
	if err := p.RemoveSource(source.ID); err != nil {
		t.Fatal(err)
	}
	if p.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("removed source still reports healthy")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.InitializeSource(context.Background(), source); err == nil {
		t.Fatal("want initialize to fail after close")
	}
}
