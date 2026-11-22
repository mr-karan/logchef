package server

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// todayFilter selects today's rows of the daily rollups.
func todayFilter() models.QueryStatsFilter {
	today := time.Now().UTC().Format(time.DateOnly)
	return models.QueryStatsFilter{PriorSince: today, Since: today, Until: today}
}

// wantRollups checks today's daily rollups after the server stopped: runs
// successful runs in query_stats_daily, each with a latency bucket, and the
// failed runs per reason.
func (e *queryAuditEnv) wantRollups(runs int64, failures map[string]int64) {
	e.t.Helper()
	ctx := context.Background()
	summary, err := e.db.QueryStatsSummary(ctx, todayFilter())
	if err != nil {
		e.t.Fatal(err)
	}
	cost, err := e.db.QueryCostSummary(ctx, todayFilter())
	if err != nil {
		e.t.Fatal(err)
	}
	if summary.Current.QueryCount != runs || cost.Current.Latency.Count() != runs {
		e.t.Errorf("daily stats count %d runs, %d with a latency, want %d", summary.Current.QueryCount, cost.Current.Latency.Count(), runs)
	}
	reasons, err := e.db.QueryFailuresByReason(ctx, todayFilter())
	if err != nil {
		e.t.Fatal(err)
	}
	got := map[string]int64{}
	for _, r := range reasons {
		got[r.Reason] = r.Count
	}
	if failures == nil {
		failures = map[string]int64{}
	}
	if !reflect.DeepEqual(got, failures) {
		e.t.Errorf("failure rollup %v, want %v", got, failures)
	}
}

// scanStats is what scanQueryProvider's queries read.
var scanStats = models.QueryStats{ExecutionTimeMs: 1_500, RowsRead: 4_200, BytesRead: 98_765, RowsReturned: 1, LimitApplied: 100}

// scanQueryProvider answers like succeedingQueryProvider with scanStats, and
// counts the queries it runs.
type scanQueryProvider struct {
	succeedingQueryProvider
	runs int
}

func (p *scanQueryProvider) QueryLogs(ctx context.Context, s *models.Source, r datasource.QueryRequest) (*models.QueryResult, error) {
	p.runs++
	result, err := p.succeedingQueryProvider.QueryLogs(ctx, s, r)
	if err == nil {
		result.Stats = scanStats
	}
	return result, err
}

func (p *scanQueryProvider) QueryLogsStream(_ context.Context, _ *models.Source, _ datasource.QueryRequest, w datasource.StreamWriter) (models.QueryStats, error) {
	p.runs++
	if err := w.Begin(previewTestColumns); err != nil {
		return models.QueryStats{}, err
	}
	if err := w.WriteRow(previewTestRow); err != nil {
		return models.QueryStats{}, err
	}
	return scanStats, w.Finish(scanStats)
}

// A dashboard panel whose request fills the cache ran its query: its
// query.run event carries the result stats like any query, and the run
// counts in the daily rollups with its latency and scan. A hit ran nothing:
// its event has the cache status only and no rollup counts it.
func TestDashboardCacheFillCountsAndHitDoesNot(t *testing.T) {
	t.Parallel()
	for _, sourceType := range []models.SourceType{models.SourceTypeClickHouse, models.SourceTypeVictoriaLogs} {
		t.Run(string(sourceType), func(t *testing.T) {
			t.Parallel()
			provider := &scanQueryProvider{succeedingQueryProvider: succeedingQueryProvider{sourceType: sourceType}}
			e := newQueryAuditEnvWith(t, provider, withConfig(func(c *config.Config) {
				c.DashboardCache = config.DashboardCacheConfig{Enabled: true, MaxTTL: time.Minute, MaxBytes: 1 << 20, MaxEntryBytes: 1 << 16, MaxConcurrentFills: 4}
			}))
			body := `{"query_text":"SELECT * FROM logs","limit":50,"cache":{"scope":"dashboard","ttl_seconds":30}}`
			for _, want := range []string{"MISS", "HIT"} {
				resp := e.do(http.MethodPost, e.base+"/logs/query", body, e.cookie)
				wantStatus(t, resp, http.StatusOK)
				if got := resp.header.Get("X-Logchef-Cache"); got != want {
					t.Fatalf("cache status %q, want %s", got, want)
				}
			}
			if provider.runs != 1 {
				t.Fatalf("the provider ran %d queries, want the fill's one", provider.runs)
			}

			events := e.settledEvents()
			if len(events) != 2 {
				t.Fatalf("stored %d events, want a fill and a hit", len(events))
			}
			byCache := map[any]map[string]any{}
			for _, ev := range events {
				d := detailOf(t, ev)
				byCache[d["cache"]] = d
			}
			fill, hit := byCache["miss"], byCache["hit"]
			if fill == nil || hit == nil {
				t.Fatalf("events %v, want cache miss and hit", byCache)
			}
			wantFill := map[string]any{"rows_returned": float64(1), "rows_read": float64(4_200), "bytes_read": float64(98_765), "limit_applied": float64(100), "result_truncated": false, "duration_ms": float64(1_500)}
			for key, want := range wantFill {
				if fill[key] != want {
					t.Errorf("fill event %s = %v, want %v", key, fill[key], want)
				}
			}
			for _, key := range []string{"rows_returned", "rows_read", "bytes_read", "limit_applied", "result_truncated"} {
				if v, ok := hit[key]; ok {
					t.Errorf("hit event has %s = %v, want none", key, v)
				}
			}

			e.wantRollups(1, nil)
			cost, err := e.db.QueryCostSummary(context.Background(), todayFilter())
			if err != nil {
				t.Fatal(err)
			}
			want := models.RunCost{Latency: models.LatencyHistogramOf(1_500), RowsRead: 4_200, BytesRead: 98_765}
			if cost.Current != want {
				t.Errorf("rollup cost %+v, want the fill's %+v", cost.Current, want)
			}
			if got := e.historyRows(); got != 0 {
				t.Errorf("history rows %d, want none for a dashboard panel", got)
			}
		})
	}
}

// A run that its windowed search continues counts once: the first page.
func TestWindowedPagesCountOnce(t *testing.T) {
	t.Parallel()
	e := newQueryAuditEnv(t, models.SourceTypeVictoriaLogs, nil)
	first := `{"query_language":"logsql","query_text":"error","limit":50}`
	wantStatus(t, e.do(http.MethodPost, e.base+"/logs/windowed", first, e.cookie), http.StatusOK)
	next := fmt.Sprintf(`{"query_language":"logsql","query_text":"error","limit":50,"cursor":%q}`, "page-2")
	wantStatus(t, e.do(http.MethodPost, e.base+"/logs/windowed", next, e.cookie), http.StatusOK)
	if events := e.settledEvents(); len(events) != 2 {
		t.Fatalf("stored %d events, want one per page", len(events))
	}
	e.wantRollups(1, nil)
}
