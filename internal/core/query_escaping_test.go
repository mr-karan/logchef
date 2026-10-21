package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestPreparedQueriesRoundTripVictoriaLogs(t *testing.T) {
	endpoint := os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")
	if endpoint == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL to run against VictoriaLogs")
	}
	db := newTestDB(t)
	log := discardLogger()
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "escaping", SourceType: models.SourceTypeVictoriaLogs, ConnectionConfig: conn, MetaTSField: "_time"}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{Email: "escaping@example.com", FullName: "Escaping", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	team, err := CreateTeam(ctx, db, log, "escaping", "")
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

	client := &http.Client{Timeout: 10 * time.Second}
	cfg := config.QueryConfig{DefaultTimeoutSeconds: 10}
	for _, value := range []string{`\`, `'`, `\'`, `\' OR 1=1 --`, `" | fields missing`, "`", `[[info]]`, `a\nb`, "a\nb\x00c"} {
		t.Run(value, func(t *testing.T) {
			req := models.APIQueryRequest{QueryText: `* | generate_sequence 1 | format {{value}} as value | fields value`, Variables: []models.TemplateVariable{{Name: "value", Type: "string", Value: value}}}
			query, err := PrepareSourceSQLQuery(ctx, ds, cfg, src, req)
			if err != nil {
				t.Fatal(err)
			}
			hist, err := PrepareSourceHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: req.QueryText, Variables: req.Variables})
			if err != nil {
				t.Fatal(err)
			}
			windowed, err := PrepareSQLQuery(cfg, req, source.SourceType)
			if err != nil {
				t.Fatal(err)
			}
			queries := []string{query.RawQuery, hist.Query, windowed.RawQuery}
			for _, text := range []string{`* | generate_sequence 1 | format '{{value}}' as value | fields value`, `* | generate_sequence 1 | format "{{value}}" as value | fields value`} {
				quotedReq := req
				quotedReq.QueryText = text
				quoted, err := PrepareSQLQuery(cfg, quotedReq, source.SourceType)
				if err != nil {
					t.Fatal(err)
				}
				queries = append(queries, quoted.RawQuery)
			}
			for _, sql := range queries {
				body := url.Values{"query": {sql}, "start": {"2026-01-01T00:00:00Z"}, "end": {"2026-01-01T00:00:01Z"}}
				httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/select/logsql/query", strings.NewReader(body.Encode()))
				if err != nil {
					t.Fatal(err)
				}
				httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				resp, err := client.Do(httpReq)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("query %q: HTTP %d %s", sql, resp.StatusCode, data)
					continue
				}
				var got map[string]string
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatalf("decode %q: %v", data, err)
				}
				if got["value"] != value {
					t.Errorf("got %q, want %q", got["value"], value)
				}
			}
		})
	}
}

func TestNativeTemplatePreparationUsesMetadataOnly(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ds := datasource.NewService(db, log)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := newTestSource(t, db, "metadata_only")
	admin := &models.User{Email: "metadata-only@example.com", FullName: "Metadata", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	team, err := CreateTeam(ctx, db, log, "metadata-only", "")
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
	variables := []models.TemplateVariable{{Name: "value", Type: "string", Value: `\'`}}
	cfg := config.QueryConfig{DefaultTimeoutSeconds: 10}
	query, err := PrepareSourceSQLQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT {{value}}", Variables: variables})
	if err != nil {
		t.Fatal(err)
	}
	histogram, err := PrepareSourceHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: "SELECT {{value}}", Variables: variables})
	if err != nil {
		t.Fatal(err)
	}
	if query.RawQuery != `SELECT '\\'''` || histogram.Query != query.RawQuery {
		t.Fatalf("query %q, histogram %q", query.RawQuery, histogram.Query)
	}
	if err := db.DeleteSource(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSourceSQLQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT {{value}}", Variables: variables}); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("missing source error = %v", err)
	}
	if _, err := PrepareSourceHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: "SELECT {{value}}", Variables: variables}); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("missing histogram source error = %v", err)
	}
}
