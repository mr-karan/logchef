package datasource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func completenessClickHouse(t *testing.T) (addr string, admin ch.Conn) {
	t.Helper()
	addr = os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	admin, err := ch.Open(&ch.Options{Addr: []string{addr}, Protocol: ch.Native})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return addr, admin
}

// TestEvaluateAlertCompletenessClickHouse evaluates count() over 100000
// rows where a limit could end the query early: in the query's SETTINGS and
// in the ClickHouse user's settings profile. The evaluation fails rather than
// compare a partial count to the threshold.
func TestEvaluateAlertCompletenessClickHouse(t *testing.T) {
	addr, admin := completenessClickHouse(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	const count = "SELECT count() AS value FROM numbers(100000)"

	result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: count + " SETTINGS max_rows_to_read = 1000000, read_overflow_mode = 'throw'"})
	if err != nil || result.Logs[0]["value"] != uint64(100000) {
		t.Fatalf("EvaluateAlert() = %v, %v; want value 100000", result, err)
	}
	for _, settings := range []string{
		"max_rows_to_read = 1000, read_overflow_mode = 'break', max_threads = 1, max_block_size = 100",
		"max_rows_to_read_leaf = 1000, read_overflow_mode_leaf = 'break'",
		"max_rows_to_read_leaf = 1000, `read_overflow_mode_leaf` = 'break'",
		"\"offset\" = 1",
		"skip_unavailable_shards = 1",
	} {
		result, err = provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: count + " SETTINGS " + settings})
		if _, ok := errors.AsType[*ValidationError](err); !ok {
			t.Fatalf("EvaluateAlert() under SETTINGS %s = %v, %v; want a validation error", settings, result, err)
		}
	}

	user := fmt.Sprintf("logchef_test_%d", time.Now().UnixNano())
	if err := admin.Exec(ctx, "CREATE USER "+user+" IDENTIFIED WITH no_password SETTINGS max_rows_to_read = 1000, read_overflow_mode = 'break', max_threads = 1, max_block_size = 100"); err != nil {
		t.Skipf("this ClickHouse server does not allow creating users: %v", err)
	}
	defer func() { _ = admin.Exec(context.Background(), "DROP USER IF EXISTS "+user) }()
	profiled := &models.Source{ID: 2, Connection: models.ConnectionInfo{Host: addr, Username: user, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, profiled); err != nil {
		t.Fatal(err)
	}
	result, err = provider.EvaluateAlert(ctx, profiled, AlertQueryRequest{Query: count})
	if err == nil {
		t.Fatalf("EvaluateAlert() under a profile read cap in break mode = %v, want an error", result.Logs)
	}
}

// TestTailCompletenessClickHouse tails a table through a source whose
// max_result_rows is below the tail's batch, so every page fills that cap.
// The tail pages through all of them. A filter whose SETTINGS could end a
// poll early is rejected.
func TestTailCompletenessClickHouse(t *testing.T) {
	addr, admin := completenessClickHouse(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	table := fmt.Sprintf("tail_completeness_%d", time.Now().UnixNano())
	if err := admin.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(9, 'UTC'), msg String) ENGINE = MergeTree ORDER BY (ts, msg)"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Exec(context.Background(), "DROP TABLE "+table) }()
	if err := admin.Exec(ctx, "INSERT INTO "+table+" SELECT now64(9, 'UTC') + INTERVAL 1 SECOND, toString(number) FROM numbers(350)"); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	maxResultRows := int64(100)
	source := &models.Source{ID: 1, MetaTSField: "ts", Connection: models.ConnectionInfo{
		Host: addr, Database: "default", TableName: table,
		Settings: &models.ClickHouseQuerySettings{MaxResultRows: &maxResultRows},
	}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)

	tailCtx, stop := context.WithTimeout(ctx, 6*time.Second)
	defer stop()
	seen := map[string]struct{}{}
	err := provider.TailLogs(tailCtx, source, TailRequest{PollInterval: 10 * time.Millisecond, LookbackMargin: 5 * time.Second}, func(rows []map[string]any) error {
		for _, row := range rows {
			seen[row["msg"].(string)] = struct{}{}
		}
		if len(seen) == 350 {
			stop()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(seen) != 350 {
		t.Fatalf("tail emitted %d of 350 rows (%v)", len(seen), err)
	}

	for _, settings := range []string{
		"max_rows_to_read = 5, read_overflow_mode = 'break'",
		"max_rows_to_read_leaf = 5, `read_overflow_mode_leaf` = 'break'",
		"`limit` = 5",
	} {
		filterCtx, stopFilter := context.WithTimeout(ctx, 3*time.Second)
		err = provider.TailLogs(filterCtx, source, TailRequest{
			Query:        "1 = 1) ORDER BY ts SETTINGS " + settings + " --",
			PollInterval: 10 * time.Millisecond,
		}, func([]map[string]any) error { return nil })
		stopFilter()
		if _, ok := errors.AsType[*ValidationError](err); !ok {
			t.Fatalf("TailLogs() with a filter whose SETTINGS %s end a poll early = %v, want a validation error", settings, err)
		}
	}
}
