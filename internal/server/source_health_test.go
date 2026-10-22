package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestSourceListHandlersCachedHealth(t *testing.T) {
	var stalled atomic.Bool
	var health atomic.Int32
	env := newErrorResponseServer(t, func(w http.ResponseWriter, r *http.Request) {
		health.Add(1)
		if stalled.Load() {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	source, err := env.s.sqlite.GetSource(t.Context(), env.vlSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.s.datasources.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	stalled.Store(true)
	for _, path := range []string{"/api/v1/admin/sources", fmt.Sprintf("/api/v1/teams/%d/sources", env.team)} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			req.AddCookie(env.cookie)
			before := health.Load()
			started := time.Now()
			resp, err := env.s.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var result struct {
				Status string           `json:"status"`
				Data   []*models.Source `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(started)
			t.Logf("HTTP status=%d rows=%d backend_requests=%d elapsed=%s", resp.StatusCode, len(result.Data), health.Load()-before, elapsed)
			if resp.StatusCode != http.StatusOK || result.Status != "success" || len(result.Data) != 2 {
				t.Fatalf("listing response %d %+v", resp.StatusCode, result)
			}
			for _, got := range result.Data {
				if got.ID == source.ID && !got.IsConnected {
					t.Error("listing lost cached healthy status")
				}
			}
			if health.Load() != before {
				t.Error("listing called stalled backend")
			}
			if elapsed > time.Second {
				t.Errorf("listing blocked for %s", elapsed)
			}
		})
	}
}

func TestSingleSourceHandlerKeepsLiveHealth(t *testing.T) {
	var down atomic.Bool
	var health atomic.Int32
	env := newErrorResponseServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			health.Add(1)
			if down.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, `{"values":[]}`)
	})
	source, err := env.s.sqlite.GetSource(t.Context(), env.vlSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.s.datasources.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	before := health.Load()
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d", env.team, env.vlSource)
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.AddCookie(env.cookie)
	resp, err := env.s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Data models.Source `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || result.Data.ID != source.ID || result.Data.IsConnected {
		t.Fatalf("single-source response %d %+v", resp.StatusCode, result.Data)
	}
	if health.Load() != before+1 {
		t.Fatal("single-source GET did not check live health")
	}
}
