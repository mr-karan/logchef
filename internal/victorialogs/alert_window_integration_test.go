package victorialogs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestIntegrationAlertWindowAt: a native LogsQL alert evaluated at At one
// hour ago reads only the rows of the lookback that ends at At.
func TestIntegrationAlertWindowAt(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)
	now := time.Now().UTC().Truncate(time.Second)
	hourAgo := now.Add(-time.Hour)
	ingestFixtures(t, baseURL, runID, hourAgo, []fixtureRow{
		{msg: "old", level: "error", offset: 0},
		{msg: "old", level: "error", offset: 10 * time.Second},
	})
	ingestFixtures(t, baseURL, runID, now, []fixtureRow{{msg: "new", level: "error", offset: -10 * time.Second}})

	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf("test_run:=%q", runID)}})
	provider := newTestProvider(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := provider.InitializeSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	waitForFixtures(t, provider, source, queryWindow{start: hourAgo.Add(-time.Minute), end: now.Add(time.Minute)}, 3)

	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "an hour ago", at: hourAgo.Add(time.Minute), want: "2"},
		{name: "now", at: now, want: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, datasource.AlertQueryRequest{
				At:              tc.at,
				Language:        models.QueryLanguageLogsQL,
				Query:           "* | stats count() as value",
				LookbackSeconds: 300,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != 1 || result.Logs[0]["value"] != tc.want {
				t.Fatalf("result = %v, want count %s", result.Logs, tc.want)
			}
		})
	}
}
