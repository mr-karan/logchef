package victorialogs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestIntegrationBucketAlignmentAcrossDaylightSaving is the VictoriaLogs half
// of the "Bucket alignment" check of RFC 0003. Europe/Berlin leaves summer
// time on 2026-10-25. VictoriaLogs takes one fixed offset, read at the start
// of the range, so its day buckets keep starting at 22:00 UTC. That is local
// midnight before the change and 23:00 local time after it. The result form
// with {{__tz_offset}} lands on the same buckets as count mode.
//
// The fixture is in October 2026, so the instance needs a retention period
// that still holds it (-retentionPeriod=100y).
func TestIntegrationBucketAlignmentAcrossDaylightSaving(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)
	start := time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC) // 2026-10-23 00:00 +02:00
	end := time.Date(2026, 10, 27, 23, 0, 0, 0, time.UTC)   // 2026-10-28 00:00 +01:00

	// One row every six hours, from the start of the range to its last hour.
	var rows []fixtureRow
	for offset := time.Duration(0); start.Add(offset).Before(end); offset += 6 * time.Hour {
		rows = append(rows, fixtureRow{msg: fmt.Sprintf("row at +%s", offset), level: "info", service: "api", offset: offset})
	}
	ingestFixtures(t, baseURL, runID, start, rows)

	conn := models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf(`test_run:=%q`, runID)}}
	source := mustSource(t, conn)
	provider := newTestProvider(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	visible := 0
	for range 40 {
		result, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 100, MaxLimit: 1000})
		if err == nil {
			visible = len(result.Logs)
		}
		if visible == len(rows) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if visible != len(rows) {
		t.Fatalf("VictoriaLogs holds %d of the %d fixture rows from October 2026: start it with -retentionPeriod=100y", visible, len(rows))
	}

	type bucket struct {
		start string
		count int
	}
	// Every bucket starts at 22:00 UTC. After 2026-10-25 that is 23:00 in
	// Berlin, one hour before local midnight, so the last local hour of
	// 2026-10-27 falls into a sixth bucket.
	want := []bucket{
		{"2026-10-22T22:00:00Z", 4}, {"2026-10-23T22:00:00Z", 4}, {"2026-10-24T22:00:00Z", 4},
		{"2026-10-25T22:00:00Z", 4}, {"2026-10-26T22:00:00Z", 4}, {"2026-10-27T22:00:00Z", 1},
	}
	assert := func(t *testing.T, got []bucket) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %d buckets %v, want %v", len(got), got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("bucket %d = %v, want %v", i, got[i], want[i])
			}
		}
	}

	t.Run("count mode", func(t *testing.T) {
		result, err := provider.Histogram(ctx, source, datasource.HistogramRequest{Query: "*", StartTime: &start, EndTime: &end, Window: "24h", Timezone: "Europe/Berlin"})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]bucket, len(result.Data))
		for i, row := range result.Data {
			got[i] = bucket{row.Bucket.UTC().Format(time.RFC3339), row.LogCount}
		}
		assert(t, got)
	})

	t.Run("result mode", func(t *testing.T) {
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			t.Fatal(err)
		}
		interval, _ := template.ParseInterval("24h")
		query, err := template.LogsQL.Substitute(
			`_time:[{{__from}}, {{__to}}) | stats by (_time:{{__interval}} offset {{__tz_offset}}) count() as n | sort by (_time)`,
			nil, template.Builtins{From: &start, To: &end, Location: berlin, Interval: interval},
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("rendered LogsQL: %s", query)
		result, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{RawQuery: query, StartTime: &start, EndTime: &end, Limit: 100, MaxLimit: 1000})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]bucket, len(result.Logs))
		for i, row := range result.Logs {
			var count int
			if _, err := fmt.Sscan(fmt.Sprint(row["n"]), &count); err != nil {
				t.Fatalf("row %d = %#v", i, row)
			}
			got[i] = bucket{fmt.Sprint(row["_time"]), count}
		}
		assert(t, got)
	})
}
