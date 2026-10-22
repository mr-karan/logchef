package core

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestLoadSourceStoredMetadata(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	log := discardLogger()
	stored := newTestSource(t, db, "stored-only")
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	source, err := LoadSource(ctx, ds, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if source.ID != stored.ID || source.Name != stored.Name || !source.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("stored metadata changed: %+v", source)
	}
	if !source.HasCapability(string(datasource.CapabilityExports)) || !source.SupportsQueryLanguage(models.QueryLanguageLogchefQL) {
		t.Fatal("source lost provider capabilities or query languages")
	}
	if _, err := LoadSource(ctx, ds, stored.ID+1); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("missing source error=%v, want ErrSourceNotFound", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = LoadSource(ctx, ds, stored.ID)
	if _, ok := errors.AsType[*datasource.StoreError](err); !ok {
		t.Fatalf("closed metadata store error=%v, want StoreError", err)
	}
}

func TestLoadSourceMetadataErrorContext(t *testing.T) {
	db := newTestDB(t)
	stored := newTestSource(t, db, "missing-provider")
	ds := datasource.NewService(db, discardLogger())
	_, err := LoadSource(context.Background(), ds, stored.ID)
	if err == nil || !strings.HasPrefix(err.Error(), "error annotating source features: ") {
		t.Fatalf("metadata error lacks context: %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("metadata error lost provider cause: %v", err)
	}
}

func TestListSourcesClickHouseCachedHealth(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	db := newTestDB(t)
	ctx := context.Background()
	log := discardLogger()
	proxy := delayedClickHouseProxy(t, addr, 10*time.Second)
	source := &models.Source{Name: "stalled", SourceType: models.SourceTypeClickHouse, MetaTSField: "event_time", Connection: models.ConnectionInfo{Host: proxy, Database: "system", TableName: "tables"}}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	started := time.Now()
	sources, err := ListSources(ctx, db, ds)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].IsConnected {
		t.Fatalf("pending source health=%+v", sources)
	}
	if elapsed > time.Second {
		t.Fatalf("listing blocked on pending ClickHouse check for %s", elapsed)
	}
}

func delayedClickHouseProxy(t *testing.T, upstream string, delay time.Duration) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			downstream, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer downstream.Close()
				remote, err := (&net.Dialer{}).DialContext(ctx, "tcp", upstream)
				if err != nil {
					return
				}
				defer remote.Close()
				stop := context.AfterFunc(ctx, func() {
					_ = downstream.Close()
					_ = remote.Close()
				})
				defer stop()
				var copyWorker sync.WaitGroup
				copyWorker.Go(func() {
					_, _ = io.Copy(remote, downstream)
					_ = remote.Close()
				})
				select {
				case <-time.After(delay):
					_, _ = io.Copy(downstream, remote)
				case <-ctx.Done():
				}
				_ = downstream.Close()
				_ = remote.Close()
				copyWorker.Wait()
			})
		}
	})
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		workers.Wait()
	})
	return listener.Addr().String()
}
