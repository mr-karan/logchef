package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestQueryPreparationErrorStatus(t *testing.T) {
	for _, operation := range []string{"query", "histogram"} {
		for _, failure := range []struct {
			name   string
			status int
		}{
			{"missing source", http.StatusNotFound},
			{"closed store", http.StatusInternalServerError},
			{"cancelled", http.StatusRequestTimeout},
			{"invalid variable", http.StatusBadRequest},
		} {
			t.Run(operation+"/"+failure.name, func(t *testing.T) {
				s := newDashboardTestServer(t)
				s.config = &config.Config{Query: config.QueryConfig{DefaultTimeoutSeconds: 10}}
				s.datasources = datasource.NewService(s.store, s.log)
				source := &models.Source{Name: "preparation", SourceType: models.SourceTypeClickHouse}
				if err := s.store.CreateSource(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				user, src := authorizeTestSource(t, s.store, source.ID)
				handler := s.handleQueryLogs
				if operation == "histogram" {
					handler = s.handleGetHistogram
				}
				app := fiber.New()
				withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/query", user, func(c fiber.Ctx) error {
					switch failure.name {
					case "missing source":
						if err := s.store.DeleteSource(t.Context(), source.ID); err != nil {
							t.Fatal(err)
						}
					case "closed store":
						if err := s.store.Close(); err != nil {
							t.Fatal(err)
						}
					case "cancelled":
						ctx, cancel := context.WithCancel(c.Context())
						cancel()
						c.SetContext(ctx)
					}
					return handler(c)
				})
				variableName := "v"
				if failure.name == "invalid variable" {
					variableName = "invalid name"
				}
				body := fmt.Sprintf(`{"query_text":"SELECT {{v}}","variables":[{"name":%q,"type":"string","value":"safe"}]}`, variableName)
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/teams/%d/sources/%d/query", src.TeamID(), source.ID), strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != failure.status {
					t.Fatalf("status=%d, want %d", resp.StatusCode, failure.status)
				}
			})
		}
	}
}
