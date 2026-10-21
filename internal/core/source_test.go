package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/victorialogs"
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

func TestListTeamSourcesConcurrentHealth(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/down-") {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)
	team, err := CreateTeam(ctx, db, log, "health-checks", "")
	if err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	for i := range 5 {
		path := fmt.Sprintf("/up-%d", i)
		if i < 2 {
			path = fmt.Sprintf("/down-%d", i)
		}
		connection, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL + path})
		if err != nil {
			t.Fatal(err)
		}
		source := &models.Source{Name: fmt.Sprintf("source-%d", i), SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: connection}
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatal(err)
		}
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	started := time.Now()
	sources, err := ListTeamSources(checkCtx, db, ds, log, team.ID)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 5 {
		t.Fatalf("got %d sources, want 5", len(sources))
	}
	connected := 0
	for _, source := range sources {
		wantConnected := source.Name != "source-0" && source.Name != "source-1"
		if source.IsConnected != wantConnected {
			t.Errorf("source %s connected=%t, want %t", source.Name, source.IsConnected, wantConnected)
		}
		if source.IsConnected {
			connected++
		}
		if !source.SupportsQueryLanguage(models.QueryLanguageLogsQL) {
			t.Errorf("source %d lost query languages", source.ID)
		}
	}
	if connected != 3 {
		t.Errorf("got %d connected sources, want 3", connected)
	}
	t.Logf("five sources, two stalled health endpoints, elapsed=%s", elapsed)
	if elapsed > 3*time.Second {
		t.Errorf("health checks took %s, want caller deadline plus scheduling margin below 3s", elapsed)
	}
}

func TestSourceListsBoundHealthConcurrency(t *testing.T) {
	for _, teamOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("team=%t", teamOnly), func(t *testing.T) {
			db := newTestDB(t)
			log := discardLogger()
			ctx := context.Background()
			var active, maximum atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current := active.Add(1)
				defer active.Add(-1)
				for previous := maximum.Load(); current > previous; previous = maximum.Load() {
					if maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				select {
				case <-time.After(50 * time.Millisecond):
					w.WriteHeader(http.StatusOK)
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(backend.Close)
			team, err := CreateTeam(ctx, db, log, "bounded-health", "")
			if err != nil {
				t.Fatal(err)
			}
			ds := datasource.NewService(db, log)
			ds.Register(victorialogs.NewProvider(log))
			for i := range 41 {
				raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: fmt.Sprintf("%s/source-%d", backend.URL, i)})
				if err != nil {
					t.Fatal(err)
				}
				source := &models.Source{Name: fmt.Sprintf("source-%02d", i), SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
				if err := db.CreateSource(ctx, source); err != nil {
					t.Fatal(err)
				}
				if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
					t.Fatal(err)
				}
			}
			var sources []*models.Source
			if teamOnly {
				sources, err = ListTeamSources(ctx, db, ds, log, team.ID)
			} else {
				sources, err = ListSources(ctx, db, ds)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(sources) != 41 || active.Load() != 0 {
				t.Fatalf("sources=%d active=%d, want 41/0", len(sources), active.Load())
			}
			if got := maximum.Load(); got < 2 || got > 32 {
				t.Errorf("maximum concurrent checks=%d, want between 2 and 32", got)
			}
			for _, source := range sources {
				if !source.IsConnected || !source.SupportsQueryLanguage(models.QueryLanguageLogsQL) {
					t.Errorf("source %d lost status or query languages", source.ID)
				}
			}
		})
	}
}

func TestListTeamSourcesSlowHealthyBackend(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer backend.Close()
	team, err := CreateTeam(ctx, db, log, "slow-health", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "slow-healthy", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	sources, err := ListTeamSources(ctx, db, ds, log, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || !sources[0].IsConnected {
		t.Fatalf("slow healthy source reported disconnected: %+v", sources)
	}
}

func TestListSourcesCancelledHealthNotCached(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			cancel()
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "cancelled-health", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
	if err := db.CreateSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	sources, err := ListSources(ctx, db, ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].IsConnected {
		t.Fatalf("cancelled health result: %+v", sources)
	}
	health, err := GetSourceHealth(context.Background(), ds, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != models.HealthStatusHealthy || calls.Load() != 2 {
		t.Fatalf("cancelled listing poisoned health cache: status=%s requests=%d", health.Status, calls.Load())
	}
}

func TestListSourcesWholeHealthDeadline(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer backend.Close()
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	for i := range 65 {
		raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: fmt.Sprintf("%s/source-%d", backend.URL, i)})
		if err != nil {
			t.Fatal(err)
		}
		source := &models.Source{Name: fmt.Sprintf("stalled-%d", i), SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	sources, err := ListSources(ctx, db, ds)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 65 {
		t.Fatalf("sources=%d, want 65", len(sources))
	}
	for _, source := range sources {
		if source.IsConnected {
			t.Errorf("stalled source %d connected", source.ID)
		}
	}
	t.Logf("65 stalled sources elapsed=%s", elapsed)
	if elapsed > 6500*time.Millisecond {
		t.Errorf("listing took %s, want 5s whole deadline plus 1.5s scheduling margin", elapsed)
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
