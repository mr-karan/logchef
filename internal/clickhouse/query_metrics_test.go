package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	vmmetrics "github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestQueryProgressAccumulatesScannedWork(t *testing.T) {
	var progress queryProgress
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				progress.add(&ch.Progress{Rows: 10, Bytes: 80, TotalRows: 999999, WroteRows: 50})
				progress.apply(&models.QueryStats{})
			}
		})
	}
	wg.Wait()
	stats := models.QueryStats{RowsReturned: 1}
	progress.apply(&stats)
	if stats.RowsRead != 4000 || stats.BytesRead != 32000 || stats.RowsReturned != 1 {
		t.Fatalf("incorrect scanned work: %+v", stats)
	}
}

func TestQueryProgressOverflow(t *testing.T) {
	for _, value := range []uint64{0, 1, math.MaxInt - 1, math.MaxInt, uint64(math.MaxInt) + 1, math.MaxUint64} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			var progress queryProgress
			progress.add(&ch.Progress{Rows: value, Bytes: value})
			var stats models.QueryStats
			progress.apply(&stats)
			want := min(value, uint64(math.MaxInt))
			if stats.RowsRead < 0 || stats.BytesRead < 0 || uint64(stats.RowsRead) != want || uint64(stats.BytesRead) != want {
				t.Fatalf("input=%d stats=%+v want=%d", value, stats, want)
			}
			progress.add(&ch.Progress{Rows: math.MaxUint64, Bytes: math.MaxUint64})
			progress.add(&ch.Progress{Rows: 1, Bytes: 1})
			progress.apply(&stats)
			if stats.RowsRead != math.MaxInt || stats.BytesRead != math.MaxInt {
				t.Fatalf("accumulation overflowed: %+v", stats)
			}
		})
	}
}

func queryCounter(source *models.Source, queryType, result string) *vmmetrics.Counter {
	return vmmetrics.GetOrCreateCounter(fmt.Sprintf(`logchef_query_total{source_id="%d",source_name=%q,database=%q,table=%q,query_type=%q,result=%q,user_email="",user_role=""}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, queryType, result))
}

type failedQueryHook struct{}

func (failedQueryHook) BeforeQuery(context.Context, string) (context.Context, error) {
	return nil, errors.New("rejected by hook")
}

func (failedQueryHook) AfterQuery(context.Context, string, error, time.Duration) {}

func TestQueryMetricsCountFailuresOnce(t *testing.T) {
	for _, hookFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(hookFailure), func(t *testing.T) {
			source := &models.Source{Name: t.Name()}
			client := &Client{metrics: metrics.NewClickHouseMetrics(source), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			if hookFailure {
				client.AddQueryHook(failedQueryHook{})
			}
			counter := queryCounter(source, "select", "failure")
			before := counter.Get()
			timeouts := vmmetrics.GetOrCreateCounter(fmt.Sprintf(`logchef_query_timeouts_total{source_id="%d",source_name=%q,database=%q,table=%q,query_type="select"}`,
				source.ID, source.Name, source.Connection.Database, source.Connection.TableName))
			beforeTimeouts := timeouts.Get()
			err := client.executeQueryWithHooks(context.Background(), "SELECT 1", func(context.Context) error {
				if hookFailure {
					t.Fatal("query ran after hook rejected it")
				}
				return context.DeadlineExceeded
			})
			if err == nil || counter.Get()-before != 1 {
				t.Fatalf("err=%v, counter increment=%d", err, counter.Get()-before)
			}
			wantTimeouts := uint64(1)
			if hookFailure {
				wantTimeouts = 0
			}
			if timeouts.Get()-beforeTimeouts != wantTimeouts {
				t.Fatalf("timeout count=%d, want %d", timeouts.Get()-beforeTimeouts, wantTimeouts)
			}
		})
	}
}

type statsRowWriter struct {
	testRowWriter
	stats models.QueryStats
}

func (w *statsRowWriter) Finish(stats models.QueryStats) error {
	w.stats = stats
	w.finished = true
	return nil
}

func TestQueryScannedWorkClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	source := &models.Source{Name: t.Name()}
	client, err := NewClient(ClientOptions{Host: addr, Source: source}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	table := fmt.Sprintf("logchef_query_metrics_%d", time.Now().UnixNano())
	createCounter := queryCounter(source, "create", "success")
	beforeCreate := createCounter.Get()
	if _, err := client.Query(ctx, "CREATE TABLE "+table+" (number UInt64) ENGINE=Memory"); err != nil {
		t.Fatal(err)
	}
	defer client.connection().Exec(ctx, "DROP TABLE "+table)
	if createCounter.Get()-beforeCreate != 1 {
		t.Fatalf("DDL counted %d times", createCounter.Get()-beforeCreate)
	}
	if err := client.connection().Exec(ctx, "INSERT INTO "+table+" SELECT number FROM numbers(100000)"); err != nil {
		t.Fatal(err)
	}
	var profiles atomic.Int64
	ctx = ch.Context(ctx, ch.WithQueryID("logchef-scanned-work-test"), ch.WithProfileInfo(func(*ch.ProfileInfo) { profiles.Add(1) }))
	query := "SELECT sum(number), initialQueryID() FROM " + table
	counter := queryCounter(source, "select", "success")
	before := counter.Get()
	result, err := client.QueryWithOptions(ctx, query, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertScannedWork := func(stats models.QueryStats) {
		t.Helper()
		if stats.RowsRead != 100000 || stats.BytesRead != 800000 || stats.RowsReturned != 1 {
			t.Fatalf("scanned work must differ from the single aggregate result: %+v", stats)
		}
	}
	assertScannedWork(result.Stats)
	if result.Logs[0]["initialQueryID()"] != "logchef-scanned-work-test" {
		t.Fatalf("driver context query ID lost: %+v", result.Logs)
	}
	writer := &statsRowWriter{}
	stats, err := client.QueryStream(ctx, query, QueryOptions{}, writer)
	if err != nil {
		t.Fatal(err)
	}
	assertScannedWork(stats)
	assertScannedWork(writer.stats)
	if !writer.finished || profiles.Load() < 2 || counter.Get()-before != 2 {
		t.Fatalf("finished=%v profiles=%d query count=%d", writer.finished, profiles.Load(), counter.Get()-before)
	}

	// Filtering all input still scans rows even though no result rows survive.
	empty, err := client.Query(ctx, "SELECT number FROM "+table+" WHERE number = 100001")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Stats.RowsRead != 100000 || empty.Stats.BytesRead != 800000 || empty.Stats.RowsReturned != 0 {
		t.Fatalf("empty result lost scanned work: %+v", empty.Stats)
	}

	failed := queryCounter(source, "select", "failure")
	beforeFailure := failed.Get()
	if _, err := client.Query(ctx, "SELECT unknown_logchef_test_function()"); err == nil {
		t.Fatal("expected SQL failure")
	}
	if _, err := client.QueryStream(ctx, query, QueryOptions{}, &testRowWriter{err: io.ErrClosedPipe}); err == nil {
		t.Fatal("expected writer failure")
	}
	if failed.Get()-beforeFailure != 2 {
		t.Fatalf("failures counted %d times, want 2", failed.Get()-beforeFailure)
	}
}
