package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/loki"
	"github.com/mr-karan/logchef/pkg/models"
)

// A condition alert on level="error" tests successfully on a Loki source
// through the alert test route (RFC 0011 unit L10). It runs against a real
// Loki when LOGCHEF_TEST_LOKI_URL is set, as CI does.
func TestLokiConditionAlertTests(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("LOGCHEF_TEST_LOKI_URL"))
	if url == "" {
		t.Skip("LOGCHEF_TEST_LOKI_URL is not set; skipping the Loki alert test")
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	runID := "it" + hex.EncodeToString(buf)
	tenant := "logchef-" + runID

	now := time.Now().UTC()
	values := func(n int) [][2]string {
		out := make([][2]string, n)
		for i := range out {
			out[i] = [2]string{fmt.Sprint(now.Add(-time.Duration(n-i) * time.Second).UnixNano()), "line"}
		}
		return out
	}
	body, err := json.Marshal(map[string]any{"streams": []map[string]any{
		{"stream": map[string]string{"test_run": runID, "level": "error"}, "values": values(2)},
		{"stream": map[string]string{"test_run": runID, "level": "info"}, "values": values(3)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pushReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	pushReq.Header.Set("Content-Type", "application/json")
	pushReq.Header.Set("X-Scope-OrgID", tenant)
	resp, err := http.DefaultClient.Do(pushReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("push returned %d", resp.StatusCode)
	}

	s := newDashboardTestServer(t)
	s.config = &config.Config{}
	provider := loki.NewProvider(s.log)
	t.Cleanup(func() { _ = provider.Close() })
	s.datasources = datasource.NewService(s.store, s.log)
	s.datasources.Register(provider)
	conn, err := json.Marshal(models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{test_run="` + runID + `"}`})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "loki-alerts", SourceType: models.SourceTypeLoki, MetaTSField: "_time", ConnectionConfig: conn}
	if err := s.store.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	user, src := authorizeTestSource(t, s.store, source.ID)
	if err := s.store.AddTeamMember(t.Context(), src.TeamID(), user.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Post("/alerts/test", func(c fiber.Ctx) error {
		c.Locals("user", user)
		c.Locals("auth_method", "session")
		return c.Next()
	}, s.handleTestAlertQuery)

	// The query the alert form generates for the condition level="error".
	request := fmt.Sprintf(`{"source_id":%d,"query_language":"logql","editor_mode":"condition","condition_json":"level=\"error\"",`+
		`"query":"sum(count_over_time({test_run=\"%s\", level=\"error\"} [300s]))","lookback_seconds":300,"threshold_operator":"gte","threshold_value":2}`,
		source.ID, runID)
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, _, parsed := postJSON(t, app, "/alerts/test", request)
		if status != fiber.StatusOK {
			t.Fatalf("status %d: %v", status, parsed)
		}
		data, _ := parsed["data"].(map[string]any)
		if data["value"] == 2.0 && data["threshold_met"] == true && data["no_data"] == false {
			// Loki bounds the query to the lookback, so no time filter is missing.
			warnings, _ := data["warnings"].([]any)
			for _, w := range warnings {
				if text, _ := w.(string); strings.HasPrefix(text, "Query appears to lack a time-based filter") {
					t.Fatalf("warnings %v, want none about a time filter", data["warnings"])
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("test result %v, want value 2 with the threshold met", data)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
