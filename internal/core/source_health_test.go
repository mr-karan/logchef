package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestSourceListsCachedHealth(t *testing.T) {
	for _, teamOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("team=%t", teamOnly), func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			log := discardLogger()
			var stalled atomic.Bool
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if stalled.Load() {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()
			team, err := CreateTeam(ctx, db, log, "cached-health", "")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL})
			if err != nil {
				t.Fatal(err)
			}
			source := &models.Source{Name: "stalled", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
			if err := db.CreateSource(ctx, source); err != nil {
				t.Fatal(err)
			}
			if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
				t.Fatal(err)
			}
			provider := victorialogs.NewProvider(log)
			defer provider.Close()
			if err := provider.InitializeSource(ctx, source); err != nil {
				t.Fatal(err)
			}
			ds := datasource.NewService(db, log)
			ds.Register(provider)
			stalled.Store(true)
			before := calls.Load()
			started := time.Now()
			var sources []*models.Source
			if teamOnly {
				sources, err = ListTeamSources(ctx, db, ds, log, team.ID)
			} else {
				sources, err = ListSources(ctx, db, ds)
			}
			elapsed := time.Since(started)
			t.Logf("one stalled source, rows=%d error=%v backend_requests=%d elapsed=%s", len(sources), err, calls.Load()-before, elapsed)
			if err != nil {
				t.Fatal(err)
			}
			if len(sources) != 1 {
				t.Fatalf("sources=%d, want 1", len(sources))
			}
			if !sources[0].IsConnected {
				t.Error("listing lost the last successful health check")
			}
			if !sources[0].SupportsQueryLanguage(models.QueryLanguageLogsQL) {
				t.Error("listing lost provider metadata")
			}
			if got := calls.Load() - before; got != 0 {
				t.Errorf("listing made %d backend requests, want 0", got)
			}
			if elapsed > time.Second {
				t.Errorf("cached listing blocked for %s", elapsed)
			}
		})
	}
}

func TestSourceListsUninitializedHealth(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	log := discardLogger()
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	team, err := CreateTeam(ctx, db, log, "pending-health", "")
	if err != nil {
		t.Fatal(err)
	}
	provider := victorialogs.NewProvider(log)
	defer provider.Close()
	ds := datasource.NewService(db, log)
	ds.Register(provider)
	for i := range 41 {
		raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: fmt.Sprintf("%s/source-%d", backend.URL, i)})
		if err != nil {
			t.Fatal(err)
		}
		source := &models.Source{Name: fmt.Sprintf("pending-%02d", i), SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: raw}
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, teamOnly := range []bool{false, true} {
		var sources []*models.Source
		if teamOnly {
			sources, err = ListTeamSources(ctx, db, ds, log, team.ID)
		} else {
			sources, err = ListSources(ctx, db, ds)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(sources) != 41 {
			t.Fatalf("sources=%d, want 41", len(sources))
		}
		for _, source := range sources {
			if source.IsConnected {
				t.Errorf("pending source %d reported connected", source.ID)
			}
			if !source.SupportsQueryLanguage(models.QueryLanguageLogsQL) {
				t.Errorf("source %d lost provider metadata", source.ID)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("listing made %d health requests for pending sources", calls.Load())
	}
}
