package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestQuerySkipsSourcePreflight(t *testing.T) {
	var health, schema, queries atomic.Int32
	env := newErrorResponseServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			health.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/select/logsql/field_names":
			schema.Add(1)
			_, _ = io.WriteString(w, `{"values":[]}`)
		case "/select/logsql/query":
			queries.Add(1)
			_, _ = io.WriteString(w, `{"_msg":"query-result"}`+"\n")
		default:
			t.Errorf("unexpected backend request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.vlSource)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query_text":"*"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(env.cookie)
	resp, err := env.s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "query-result") {
		t.Fatalf("query returned %d %s", resp.StatusCode, body)
	}
	if queries.Load() != 1 || health.Load() != 0 || schema.Load() != 0 {
		t.Fatalf("backend requests query=%d health=%d schema=%d, want 1/0/0", queries.Load(), health.Load(), schema.Load())
	}
}

func TestQueryDisconnectedSourceError(t *testing.T) {
	env := newErrorResponseServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("disconnected ClickHouse query contacted VictoriaLogs")
	})
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.chSource)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query_text":"SELECT 1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(env.cookie)
	resp, err := env.s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError || result.Status != "error" || result.ErrorType != string(models.DatabaseErrorType) {
		t.Fatalf("disconnected query returned %d %+v", resp.StatusCode, result)
	}
}

func TestQueryDoesNotWaitForSourceStatus(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	env := newErrorResponseServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		_, _ = io.WriteString(w, `{"_msg":"query-result"}`+"\n")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	source, err := env.s.store.GetSource(ctx, env.vlSource)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		env.s.datasources.CheckSourceConnectionStatus(ctx, source)
	}()
	defer func() {
		close(release)
		<-finished
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("health check did not start")
	}
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.vlSource)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query_text":"*"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(env.cookie)
	resp, err := env.s.app.Test(req)
	if err != nil {
		t.Fatalf("query waited for the blocked status check: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "query-result") {
		t.Fatalf("query returned %d %s", resp.StatusCode, body)
	}
}
