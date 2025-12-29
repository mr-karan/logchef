package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/cli/config"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &config.Config{
				Server: config.ServerConfig{
					URL:     "https://example.com",
					Timeout: 30 * time.Second,
				},
				Auth: config.AuthConfig{
					Token: "test-token",
				},
			},
			wantErr: false,
		},
		{
			name: "missing URL",
			cfg: &config.Config{
				Server: config.ServerConfig{
					URL: "",
				},
			},
			wantErr: true,
		},
		{
			name: "URL with trailing slash",
			cfg: &config.Config{
				Server: config.ServerConfig{
					URL:     "https://example.com/",
					Timeout: 30 * time.Second,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("New() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("New() unexpected error: %v", err)
				return
			}
			if client == nil {
				t.Error("New() returned nil client")
			}
		})
	}
}

func TestClient_SetToken(t *testing.T) {
	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: "https://example.com"},
	})

	client.SetToken("new-token")

	if client.token != "new-token" {
		t.Errorf("SetToken() token = %q, want %q", client.token, "new-token")
	}
}

func TestClient_Do_AuthHeader(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "success"}`))
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{
			URL:     server.URL,
			Timeout: 10 * time.Second,
		},
		Auth: config.AuthConfig{
			Token: "test-bearer-token",
		},
	})

	_, err := client.Do(context.Background(), RequestOptions{
		Method: http.MethodGet,
		Path:   "/test",
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	expected := "Bearer test-bearer-token"
	if receivedAuth != expected {
		t.Errorf("Do() Authorization header = %q, want %q", receivedAuth, expected)
	}
}

func TestClient_DoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"id":   1,
				"name": "test",
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{
			URL:     server.URL,
			Timeout: 10 * time.Second,
		},
	})

	var result struct {
		Status string `json:"status"`
		Data   struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}

	err := client.DoJSON(context.Background(), RequestOptions{
		Method: http.MethodGet,
		Path:   "/test",
	}, &result)
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}

	if result.Status != "success" {
		t.Errorf("DoJSON() status = %q, want %q", result.Status, "success")
	}

	if result.Data.ID != 1 {
		t.Errorf("DoJSON() data.id = %d, want %d", result.Data.ID, 1)
	}

	if result.Data.Name != "test" {
		t.Errorf("DoJSON() data.name = %q, want %q", result.Data.Name, "test")
	}
}

func TestClient_DoJSON_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "error",
			"message":    "Invalid token",
			"error_type": "authentication_error",
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{
			URL:     server.URL,
			Timeout: 10 * time.Second,
		},
	})

	err := client.DoJSON(context.Background(), RequestOptions{
		Method: http.MethodGet,
		Path:   "/test",
	}, nil)

	if err == nil {
		t.Fatal("DoJSON() expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("DoJSON() error type = %T, want *APIError", err)
	}

	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}

	if apiErr.Message != "Invalid token" {
		t.Errorf("APIError.Message = %q, want %q", apiErr.Message, "Invalid token")
	}

	if apiErr.ErrorType != "authentication_error" {
		t.Errorf("APIError.ErrorType = %q, want %q", apiErr.ErrorType, "authentication_error")
	}
}

func TestClient_ListTeams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/me/teams" {
			t.Errorf("ListTeams() path = %q, want %q", r.URL.Path, "/api/v1/me/teams")
		}

		json.NewEncoder(w).Encode(TeamsResponse{
			Status: "success",
			Data: []Team{
				{ID: 1, Name: "team1", Description: "First team"},
				{ID: 2, Name: "team2", Description: "Second team"},
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	teams, err := client.ListTeams(context.Background())
	if err != nil {
		t.Fatalf("ListTeams() error = %v", err)
	}

	if len(teams) != 2 {
		t.Errorf("ListTeams() count = %d, want 2", len(teams))
	}

	if teams[0].Name != "team1" {
		t.Errorf("ListTeams()[0].Name = %q, want %q", teams[0].Name, "team1")
	}
}

func TestClient_ListSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/teams/1/sources" {
			t.Errorf("ListSources() path = %q, want %q", r.URL.Path, "/api/v1/teams/1/sources")
		}

		json.NewEncoder(w).Encode(SourcesResponse{
			Status: "success",
			Data: []Source{
				{ID: 1, Name: "source1", IsConnected: true},
				{ID: 2, Name: "source2", IsConnected: false},
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	sources, err := client.ListSources(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}

	if len(sources) != 2 {
		t.Errorf("ListSources() count = %d, want 2", len(sources))
	}

	if !sources[0].IsConnected {
		t.Error("ListSources()[0].IsConnected = false, want true")
	}
}

func TestClient_GetSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/teams/1/sources/2/schema" {
			t.Errorf("GetSchema() path = %q, want correct path", r.URL.Path)
		}

		json.NewEncoder(w).Encode(SchemaResponse{
			Status: "success",
			Data: []Column{
				{Name: "timestamp", Type: "DateTime"},
				{Name: "level", Type: "String"},
				{Name: "message", Type: "String"},
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	columns, err := client.GetSchema(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}

	if len(columns) != 3 {
		t.Errorf("GetSchema() column count = %d, want 3", len(columns))
	}

	if columns[0].Name != "timestamp" {
		t.Errorf("GetSchema()[0].Name = %q, want %q", columns[0].Name, "timestamp")
	}
}

func TestClient_Query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Query() method = %q, want POST", r.Method)
		}

		var req QueryRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Query != `level="error"` {
			t.Errorf("Query() request.Query = %q, want %q", req.Query, `level="error"`)
		}

		json.NewEncoder(w).Encode(queryAPIResponse{
			Status: "success",
			Data: QueryResponse{
				Logs: []map[string]any{
					{"level": "error", "message": "test error"},
				},
				Columns: []Column{
					{Name: "level", Type: "String"},
					{Name: "message", Type: "String"},
				},
				Stats: QueryStats{
					ExecutionTimeMs: 100,
					RowsRead:        1000,
					BytesRead:       50000,
				},
				GeneratedSQL: "SELECT * FROM logs WHERE level = 'error'",
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	resp, err := client.Query(context.Background(), 1, 2, QueryRequest{
		Query:     `level="error"`,
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-01T12:00:00Z",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(resp.Logs) != 1 {
		t.Errorf("Query() log count = %d, want 1", len(resp.Logs))
	}

	if resp.Stats.ExecutionTimeMs != 100 {
		t.Errorf("Query() stats.ExecutionTimeMs = %d, want 100", resp.Stats.ExecutionTimeMs)
	}

	if resp.GeneratedSQL == "" {
		t.Error("Query() GeneratedSQL is empty")
	}
}

func TestClient_Translate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(translateAPIResponse{
			Status: "success",
			Data: TranslateResponse{
				SQL:        "level = 'error'",
				Valid:      true,
				FieldsUsed: []string{"level"},
			},
		})
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	resp, err := client.Translate(context.Background(), 1, 2, TranslateRequest{
		Query: `level="error"`,
	})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}

	if !resp.Valid {
		t.Error("Translate() Valid = false, want true")
	}

	if resp.SQL != "level = 'error'" {
		t.Errorf("Translate() SQL = %q, want %q", resp.SQL, "level = 'error'")
	}

	if len(resp.FieldsUsed) != 1 || resp.FieldsUsed[0] != "level" {
		t.Errorf("Translate() FieldsUsed = %v, want [level]", resp.FieldsUsed)
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *APIError
		expected string
	}{
		{
			name: "with error type",
			err: &APIError{
				Message:   "Token expired",
				ErrorType: "auth_error",
			},
			expected: "auth_error: Token expired",
		},
		{
			name: "without error type",
			err: &APIError{
				Message: "Something went wrong",
			},
			expected: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.expected {
				t.Errorf("APIError.Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := New(&config.Config{
		Server: config.ServerConfig{URL: server.URL, Timeout: 10 * time.Second},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Do(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   "/test",
	})

	if err == nil {
		t.Error("Do() with cancelled context should return error")
	}
}
