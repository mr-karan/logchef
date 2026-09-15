package datasource

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

type compilationQueryCounter struct{ calls atomic.Int64 }

func (h *compilationQueryCounter) BeforeQuery(ctx context.Context, _ string) (context.Context, error) {
	h.calls.Add(1)
	return ctx, nil
}

func (*compilationQueryCounter) AfterQuery(context.Context, string, error, time.Duration) {}

func TestCompileLogchefQLInspectionCache(t *testing.T) {
	host := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_HOST")
	if host == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_HOST to a local ClickHouse native address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "metadata.db")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	counter := &compilationQueryCounter{}
	manager.AddQueryHook(counter)
	source := &models.Source{
		Name: "compilation-cache", SourceType: models.SourceTypeClickHouse, MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: host, Database: "default", TableName: fmt.Sprintf("logchef_compile_cache_%d", time.Now().UnixNano())},
	}
	client, err := manager.CreateTemporaryClient(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	exec := func(query string) {
		t.Helper()
		if _, err := client.Query(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE " + source.GetFullTableName() + " (timestamp DateTime, attributes Map(String, String)) ENGINE = MergeTree ORDER BY timestamp")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = client.Query(cleanupCtx, "DROP TABLE IF EXISTS "+source.GetFullTableName())
	})
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, log)
	provider := NewClickHouseProvider(manager, log)
	service.Register(provider)
	if err := service.InitializeSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	req := LogchefQLCompileRequest{Query: `attributes.service = "api"`}
	compile := func(wantMap bool) {
		t.Helper()
		result, err := service.CompileLogchefQL(ctx, source.ID, req)
		if err != nil {
			t.Error(err)
			return
		}
		if !result.Valid || strings.Contains(result.FilterOnly, "['service']") != wantMap {
			t.Errorf("compiled SQL = %q, valid=%t, want map access=%t", result.FilterOnly, result.Valid, wantMap)
		}
	}
	// Concurrent cold compiles share one inspection fill.
	start := counter.calls.Load()
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { compile(true) })
	}
	wg.Wait()
	filled := counter.calls.Load()
	if filled <= start {
		t.Fatal("cold compilation did not inspect ClickHouse")
	}
	inspection, err := service.InspectSource(ctx, source.ID)
	if err != nil || inspection.Schema == nil {
		t.Fatalf("inspect source: %v", err)
	}
	compile(true)
	if got := counter.calls.Load(); got != filled {
		t.Fatalf("warm inspection/compilation issued %d queries", got-filled)
	}
	// A real schema change remains cached until the existing TTL expires.
	exec("ALTER TABLE " + source.GetFullTableName() + " DROP COLUMN attributes")
	exec("ALTER TABLE " + source.GetFullTableName() + " ADD COLUMN attributes String")
	compile(true)
	service.inspectionMu.Lock()
	entry := service.inspections[source.ID]
	entry.created = time.Now().Add(-inspectionCacheTTL)
	service.inspections[source.ID] = entry
	service.inspectionMu.Unlock()
	compile(false)
	refilled := counter.calls.Load()
	if refilled-filled != filled-start {
		t.Fatalf("cold burst queries=%d, single refresh queries=%d", filled-start, refilled-filled)
	}
	t.Logf("12 concurrent cold compiles: %d metadata queries; warm compile + inspection: 0; TTL refresh: %d", filled-start, refilled-filled)
	// A mismatched source revision must bypass even an unexpired entry.
	service.inspectionMu.Lock()
	entry = service.inspections[source.ID]
	entry.revision = entry.revision.Add(-time.Second)
	service.inspections[source.ID] = entry
	service.inspectionMu.Unlock()
	compile(false)
	if counter.calls.Load() <= refilled {
		t.Fatal("source revision mismatch reused stale inspection")
	}
	service.invalidateInspectionCache(source.ID)
	before := counter.calls.Load()
	compile(false)
	if counter.calls.Load() <= before {
		t.Fatal("explicit invalidation reused stale inspection")
	}
	// Direct provider compilation reads supplied columns without mutating them.
	source.Columns = []models.ColumnInfo{{Name: "attributes", Type: "Map(String, String)"}}
	for range 12 {
		wg.Go(func() {
			if _, err := provider.CompileLogchefQL(ctx, source, req); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if source.Columns[0].Type != "Map(String, String)" {
		t.Fatal("compilation changed source columns")
	}
	// Missing metadata must fail instead of executing a type-blind query.
	service.invalidateInspectionCache(source.ID)
	if err := provider.RemoveSource(source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompileLogchefQL(ctx, source.ID, req); err == nil {
		t.Fatal("expected compilation to fail when schema inspection is unavailable")
	}
}
