package victorialogs

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func waitForCachedHealth(t *testing.T, provider *Provider, id models.SourceID, connected bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for provider.CachedSourceConnectionStatus(id) != connected {
		if time.Now().After(deadline) {
			t.Fatalf("cached connected=%t, want %t", !connected, connected)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBackgroundHealthTracksRecoveryAndRemoval(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(status.Load())) }))
	defer server.Close()
	provider := newTestProvider(server)
	defer provider.Close()
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
	if provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("uninitialized source reported connected")
	}
	if err := provider.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if !provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("initial health result was not cached")
	}
	status.Store(http.StatusServiceUnavailable)
	provider.StartBackgroundHealthChecks(10 * time.Millisecond)
	waitForCachedHealth(t, provider, source.ID, false)
	status.Store(http.StatusOK)
	waitForCachedHealth(t, provider, source.ID, true)
	if err := provider.RemoveSource(source.ID); err != nil {
		t.Fatal(err)
	}
	if provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("removed source retained healthy status")
	}
	status.Store(http.StatusServiceUnavailable)
	if err := provider.InitializeSource(t.Context(), source); err == nil {
		t.Fatal("initialization ignored unhealthy backend")
	}
	if provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("replacement retained old healthy status")
	}
	status.Store(http.StatusOK)
	waitForCachedHealth(t, provider, source.ID, true)
}

func TestCloseCancelsBackgroundHealth(t *testing.T) {
	var stalled atomic.Bool
	entered := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stalled.Load() {
			entered <- struct{}{}
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var logs bytes.Buffer
	provider := newTestProvider(server)
	provider.log = slog.New(slog.NewJSONHandler(&logs, nil))
	defer provider.Close()
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
	if err := provider.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	stalled.Store(true)
	provider.StartBackgroundHealthChecks(time.Hour)
	provider.StartBackgroundHealthChecks(time.Hour)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background check did not reach backend")
	}
	if !provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("pending check overwrote last healthy result")
	}
	started := time.Now()
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Close waited %s for stalled health check", elapsed)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Close left a live health request")
	}
	if strings.Contains(logs.String(), "victorialogs health check failed") {
		t.Fatal("shutdown cancellation logged a health failure")
	}
	if provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("closed provider retained healthy state")
	}
	if err := provider.InitializeSource(context.Background(), source); err == nil {
		t.Fatal("closed provider accepted source initialization")
	}
	provider.StartBackgroundHealthChecks(time.Millisecond)
}

func TestExplicitHealthCheckUpdatesCache(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	provider := newTestProvider(server)
	defer provider.Close()
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
	if err := provider.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	status.Store(http.StatusServiceUnavailable)
	before := calls.Load()
	if provider.CheckSourceConnectionStatus(t.Context(), source) {
		t.Fatal("explicit check used stale healthy cache")
	}
	if calls.Load() != before+1 {
		t.Fatal("explicit check did not call backend")
	}
	if provider.CachedSourceConnectionStatus(source.ID) {
		t.Fatal("live failure did not update cache")
	}
}
