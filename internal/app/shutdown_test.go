package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/server"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestShutdownCancelsAlertQueryWithinBudget(t *testing.T) {
	entered := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	}))
	defer remote.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "shutdown.db")}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	connection, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: remote.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "logs", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: connection}
	if err := db.CreateSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	alert := &models.Alert{SourceID: source.ID, Name: "slow", QueryLanguage: models.QueryLanguageLogsQL, EditorMode: models.AlertEditorModeNative, Query: "count()", LookbackSeconds: 60, ThresholdOperator: models.AlertThresholdGreaterThan, FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved}
	if err := db.CreateAlert(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(victorialogs.NewProvider(log))
	manager := alerts.NewManager(alerts.Options{DB: db, Datasources: ds, Logger: log, Config: config.AlertsConfig{Enabled: true}})
	cfg := &config.Config{}
	a := &App{Logger: log, SQLite: db, Alerts: manager, server: server.New(server.ServerOptions{Config: cfg, SQLite: db, Logger: log, FS: http.FS(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})}
	evaluationCtx, cancelEvaluation := context.WithTimeout(context.Background(), time.Second)
	defer cancelEvaluation()
	manager.Start(evaluationCtx)
	select {
	case <-entered:
	case <-evaluationCtx.Done():
		t.Fatal("alert query did not reach datasource")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := a.Shutdown(ctx); err != nil {
		t.Errorf("signal shutdown returned an error: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Errorf("shutdown took %s, exceeded budget", elapsed)
	}
}

func TestShutdownCancelsSourceHealthCheck(t *testing.T) {
	var stalled atomic.Bool
	entered := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stalled.Load() {
			entered <- struct{}{}
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := victorialogs.NewProvider(log)
	defer provider.Close()
	raw, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: remote.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{ID: 1, SourceType: models.SourceTypeVictoriaLogs, ConnectionConfig: raw}
	if err := provider.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	stalled.Store(true)
	provider.StartBackgroundHealthChecks(time.Hour)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background health check did not start")
	}
	app := &App{Logger: log, VictoriaLogs: provider}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("shutdown waited %s for stalled health check", elapsed)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("shutdown left a live health request")
	}
}
