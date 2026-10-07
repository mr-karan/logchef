package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestVisibilityLoadersStopAfterNotFound pins that a saved query or alert the
// caller cannot see, or that does not exist, ends the request with 404 rather
// than continuing with a nil object (which previously produced 200 with null
// data, or a recovered nil-pointer panic on alert history).
func TestVisibilityLoadersStopAfterNotFound(t *testing.T) {
	db := newServerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Auth.APITokenSecret = authzTestTokenSecret
	cfg.Alerts.Enabled = true
	s := New(ServerOptions{
		Config:      cfg,
		SQLite:      db,
		Datasources: datasource.NewService(db, logger),
		Logger:      logger,
		FS:          http.Dir(t.TempDir()),
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	w := newAuthzWorld(t, db)
	query, err := db.CreateSavedQuery(context.Background(), w.source, &w.team, "visible to members", "",
		models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `level="error"`, &w.member.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	outsider := withSession(sessionCookie(t, db, w.outsider))
	member := withSession(sessionCookie(t, db, w.member))

	cases := []struct {
		name string
		path string
		auth func(*http.Request)
	}{
		{"missing saved query", "/api/v1/saved-queries/999999", member},
		{"invisible saved query", "/api/v1/saved-queries/" + strconv.Itoa(query.ID), outsider},
		{"missing alert", "/api/v1/alerts/999999", member},
		{"missing alert history", "/api/v1/alerts/999999/history", member},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAuthzRequest(t, s, http.MethodGet, tc.path, "", tc.auth)
			if resp.status != http.StatusNotFound {
				t.Fatalf("GET %s: status %d (%q), want 404", tc.path, resp.status, resp.message)
			}
		})
	}
}
