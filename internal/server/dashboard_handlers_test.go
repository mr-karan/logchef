package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// newDashboardTestServer builds a Server backed by a fresh temp SQLite store.
func newDashboardTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "dash.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &Server{sqlite: s, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// withUser mounts a handler with the given user injected into c.Locals, standing
// in for the requireAuth middleware.
func withUser(app *fiber.App, method, path string, user *models.User, h fiber.Handler) {
	app.Add(method, path, func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return h(c)
	})
}

func mkTestUser(t *testing.T, s store.Store, email string, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: email, Role: role, Status: models.UserStatusActive}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

const validDashboardBody = `{"name":"Ops","description":"","panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"timeseries","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"},{"id":"p2","title":"b","type":"stat","team_id":1,"source_id":1,"query":"y","query_language":"logchefql"}]}}`

func TestHandleCreateDashboardValidation(t *testing.T) {
	s := newDashboardTestServer(t)
	user := mkTestUser(t, s.sqlite, "creator@test.dev", models.UserRoleMember)

	app := fiber.New()
	withUser(app, http.MethodPost, "/dashboards", user, s.handleCreateDashboard)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", validDashboardBody, http.StatusCreated},
		{"missing name", `{"name":"","panels":{"version":1,"layout":[],"panels":[]}}`, http.StatusBadRequest},
		{"invalid json body", `{not json`, http.StatusBadRequest},
		{"too many panels", tooManyPanelsBody(), http.StatusBadRequest},
		{"bad panel type", `{"name":"x","panels":{"version":1,"layout":[],"panels":[{"id":"p1","type":"pie","team_id":1,"source_id":1,"query_language":"logchefql"}]}}`, http.StatusBadRequest},
		{"bad width", `{"name":"x","panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":5,"h":2}],"panels":[]}}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/dashboards", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != tc.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func tooManyPanelsBody() string {
	panels := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		panels = append(panels, `{"id":"p`+strconv.Itoa(i)+`","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}`)
	}
	return `{"name":"x","panels":{"version":1,"layout":[],"panels":[` + strings.Join(panels, ",") + `]}}`
}

// TestHandleUpdateDashboardAuthz verifies edit/delete are gated to the creator
// or a global admin (403 otherwise). This is the path that cannot be curl-checked
// with the admin dev token alone.
func TestHandleUpdateDashboardAuthz(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.sqlite, "owner@test.dev", models.UserRoleMember)
	stranger := mkTestUser(t, s.sqlite, "stranger@test.dev", models.UserRoleMember)
	admin := mkTestUser(t, s.sqlite, "admin@test.dev", models.UserRoleAdmin)

	// Seed a dashboard owned by creator.
	dash := &models.Dashboard{
		Name:       "Ops",
		PanelsJSON: json.RawMessage(`{"version":1,"layout":[],"panels":[]}`),
		CreatedBy:  &creator.ID,
	}
	if err := s.sqlite.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	newApp := func(user *models.User) *fiber.App {
		app := fiber.New()
		withUser(app, http.MethodPut, "/dashboards/:dashboardID", user, s.handleUpdateDashboard)
		withUser(app, http.MethodDelete, "/dashboards/:dashboardID", user, s.handleDeleteDashboard)
		return app
	}

	path := "/dashboards/" + strconv.Itoa(dash.ID)
	body := `{"name":"Ops v2","panels":{"version":1,"layout":[],"panels":[]}}`

	// Stranger (non-creator, non-admin) is forbidden from updating and deleting.
	for _, m := range []struct {
		method string
		reader io.Reader
	}{
		{http.MethodPut, strings.NewReader(body)},
		{http.MethodDelete, nil},
	} {
		req := httptest.NewRequest(m.method, path, m.reader)
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(stranger).Test(req)
		if err != nil {
			t.Fatalf("app.Test(%s): %v", m.method, err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s by stranger: status = %d, want 403", m.method, resp.StatusCode)
		}
	}

	// Creator can update.
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := newApp(creator).Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("update by creator: status = %d, want 200 (body=%s)", resp.StatusCode, rb)
	}

	// Global admin can delete even though they are not the creator.
	req = httptest.NewRequest(http.MethodDelete, path, nil)
	resp, err = newApp(admin).Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete by admin: status = %d, want 200", resp.StatusCode)
	}
}

// TestHandleGetDashboardNotFound verifies a missing id yields 404, not a 500 or
// a mis-handled Send* nil return.
func TestHandleGetDashboardNotFound(t *testing.T) {
	s := newDashboardTestServer(t)
	user := mkTestUser(t, s.sqlite, "viewer@test.dev", models.UserRoleMember)

	app := fiber.New()
	withUser(app, http.MethodGet, "/dashboards/:dashboardID", user, s.handleGetDashboard)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/dashboards/99999", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
