package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// failingLokiProvider fails every query, histogram and log context call with
// one error, as the Loki provider does for each upstream outcome.
type failingLokiProvider struct {
	datasource.Provider
	err error
}

func (p *failingLokiProvider) Type() models.SourceType { return models.SourceTypeLoki }

func (p *failingLokiProvider) Describe() datasource.Descriptor {
	return datasource.Descriptor{
		Capabilities:   []datasource.Capability{datasource.CapabilityHistogram, datasource.CapabilityLogContext},
		QueryLanguages: []models.QueryLanguage{models.QueryLanguageLogQL},
	}
}

func (p *failingLokiProvider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	return nil, p.err
}

func (p *failingLokiProvider) Histogram(context.Context, *models.Source, datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	return nil, p.err
}

func (p *failingLokiProvider) GetLogContext(context.Context, *models.Source, datasource.LogContextRequest) (*models.LogContextResponse, error) {
	return nil, p.err
}

// Each upstream outcome has one public response on every query route, and
// none shows the upstream's own text.
func TestUpstreamErrorResponses(t *testing.T) {
	const detail = "loki returned status 503: querier 10.1.2.3 out of memory"
	tests := []struct {
		name    string
		err     error
		status  int
		message string
		errType models.ErrorType
	}{
		{"busy", &datasource.UpstreamError{Kind: datasource.UpstreamBusy, Status: 429, Message: "Loki is busy", Err: fmt.Errorf("%s", detail)},
			fiber.StatusTooManyRequests, "Loki is busy", models.ExternalServiceErrorType},
		{"unavailable", &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Status: 503, Message: "Loki is unavailable (status 503)", Err: fmt.Errorf("%s", detail)},
			fiber.StatusBadGateway, "Loki is unavailable (status 503)", models.ExternalServiceErrorType},
		{"credentials rejected", &datasource.UpstreamError{Kind: datasource.UpstreamAuth, Status: 401, Message: "The source rejected Logchef's credentials", Err: fmt.Errorf("%s", detail)},
			fiber.StatusBadGateway, "The source rejected Logchef's credentials", models.ExternalServiceErrorType},
		{"parse error", &datasource.ValidationError{Field: "query", Message: "parse error at line 1, col 25: syntax error", Err: fmt.Errorf("%s", detail)},
			fiber.StatusBadRequest, "parse error at line 1, col 25: syntax error", models.ValidationErrorType},
	}
	routes := []struct {
		path string
		body string
		h    func(*Server) fiber.Handler
	}{
		{"logs/query", `{"query_text":"{app=\"api\"}","start_time":"2026-11-01T00:00:00Z","end_time":"2026-11-01T01:00:00Z"}`, func(s *Server) fiber.Handler { return s.handleQueryLogs }},
		{"logs/histogram", `{"query_text":"{app=\"api\"}","start_time":"2026-11-01T00:00:00Z","end_time":"2026-11-01T01:00:00Z","window":"1m"}`, func(s *Server) fiber.Handler { return s.handleGetHistogram }},
		{"logs/context", `{"timestamp":1793500000000}`, func(s *Server) fiber.Handler { return s.handleGetLogContext }},
	}
	for _, tt := range tests {
		for _, route := range routes {
			t.Run(tt.name+" "+route.path, func(t *testing.T) {
				s, source := newAggregationTestServer(t, &failingLokiProvider{err: tt.err}, models.SourceTypeLoki)
				app := fiber.New()
				user, src := authorizeTestSource(t, s.store, source.ID)
				pattern := "/teams/:teamID/sources/:sourceID/" + route.path
				withAuthorizedSource(app, s, pattern, user, route.h(s))
				path := fmt.Sprintf("/teams/%d/sources/%d/%s", src.TeamID(), source.ID, route.path)

				status, _, parsed := postJSON(t, app, path, route.body)
				raw, _ := json.Marshal(parsed)
				if status != tt.status || parsed["message"] != tt.message || parsed["error_type"] != string(tt.errType) {
					t.Fatalf("status %d body %s, want %d %q %s", status, raw, tt.status, tt.message, tt.errType)
				}
				if strings.Contains(string(raw), "10.1.2.3") {
					t.Fatalf("response leaks the upstream detail: %s", raw)
				}
			})
		}
	}
}
