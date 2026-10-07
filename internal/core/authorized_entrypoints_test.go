package core

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestAlertQueryForPrincipalDeniesUnauthorizedCallers keeps the alert-test
// rule: alerts:write plus source visibility through a team, with no global
// admin bypass.
func TestAlertQueryForPrincipalDeniesUnauthorizedCallers(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	member := newTestUser(t, db, "alert-test-member@example.com", "Member")
	outsider := newTestUser(t, db, "alert-test-outsider@example.com", "Outsider")
	admin := &models.User{Email: "alert-test-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	src := newTestSource(t, db, "alert_test_src")
	team, err := CreateTeam(ctx, db, log, "alert-test", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := AddTeamMember(ctx, db, log, team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	ds := newFakeDatasourceService(db, log, nil)
	readOnly := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeAlertsRead}}
	writer := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeAlertsWrite}}

	for _, tc := range []struct {
		name    string
		p       access.Principal
		wantErr error
	}{
		{"non-member", access.SessionPrincipal(outsider), ErrSourceAccessDenied},
		{"global admin, not a member", access.SessionPrincipal(admin), ErrSourceAccessDenied},
		{"member token without alerts:write", access.APITokenPrincipal(member, readOnly), access.ErrInsufficientScope},
		{"unknown auth method", access.Principal{User: member}, access.ErrInsufficientScope},
		{"member session", access.SessionPrincipal(member), nil},
		{"member token with alerts:write", access.APITokenPrincipal(member, writer), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &models.TestAlertQueryRequest{
				Query:             "SELECT count() FROM logs",
				QueryLanguage:     models.QueryLanguageClickHouseSQL,
				EditorMode:        models.AlertEditorModeNative,
				ThresholdOperator: models.AlertThresholdGreaterThan,
				ThresholdValue:    10,
			}
			resp, err := TestAlertQueryForPrincipal(ctx, db, ds, tc.p, src.ID, req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || resp != nil {
					t.Fatalf("got %v, %v; want %v", resp, err, tc.wantErr)
				}
				return
			}
			if err != nil || resp == nil {
				t.Fatalf("authorized call: %v, %v", resp, err)
			}
		})
	}
}

// TestInspectSourceActivityAsAdminDeniesUnauthorizedCallers keeps the admin
// route rule: global admin plus sources:read. The team route takes an
// access.AuthorizedSource, so only the compiler can reject its callers.
func TestInspectSourceActivityAsAdminDeniesUnauthorizedCallers(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	member := newTestUser(t, db, "activity-member@example.com", "Member")
	admin := &models.User{Email: "activity-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	src := newTestSource(t, db, "activity_src")
	ds := newFakeDatasourceService(db, log, nil)
	noSources := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}

	for _, tc := range []struct {
		name    string
		p       access.Principal
		wantErr error
	}{
		{"member session", access.SessionPrincipal(member), access.ErrGlobalAdminRequired},
		{"no user", access.Principal{Method: access.AuthSession}, access.ErrGlobalAdminRequired},
		{"admin token without sources:read", access.APITokenPrincipal(admin, noSources), access.ErrInsufficientScope},
		{"admin, unknown auth method", access.Principal{User: admin}, access.ErrInsufficientScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := InspectSourceActivityAsAdmin(ctx, ds, tc.p, src.ID, false); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// An authorized admin reaches the datasource. The test provider has no
	// activity support, so the call ends there, not at authorization.
	_, err := InspectSourceActivityAsAdmin(ctx, ds, access.SessionPrincipal(admin), src.ID, false)
	if errors.Is(err, access.ErrGlobalAdminRequired) || errors.Is(err, access.ErrInsufficientScope) {
		t.Fatalf("authorized admin denied: %v", err)
	}
}

// TestRunLogchefQLDeadlineCoversPreparation runs the real ClickHouse provider
// through a TCP proxy in front of a real ClickHouse. The proxy forwards
// everything until the client asks for system.columns (the schema fetch that
// LogchefQL compilation needs), then stops forwarding on that connection.
// The health ping and table check still work, so only preparation stalls.
// The query timeout must end the whole call. The shared inspection fill has
// its own 5 s deadline, so a deadline that starts after preparation would
// take at least that long.
func TestRunLogchefQLDeadlineCoversPreparation(t *testing.T) {
	t.Parallel()
	upstream := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if upstream == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx := context.Background()
	proxyAddr := stallSchemaProxy(t, upstream)

	db := newTestDB(t)
	log := discardLogger()
	admin := &models.User{Email: "run-logchefql@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	source := &models.Source{
		Name:       "stalled",
		SourceType: models.SourceTypeClickHouse,
		// system.one exists on every server, so the test needs no table of its own.
		Connection: models.ConnectionInfo{Host: proxyAddr, Username: "default", Database: "system", TableName: "one"},
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	team, err := CreateTeam(ctx, db, log, "stalled", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, source.ID, models.TokenScopeLogsRead)
	if err != nil {
		t.Fatalf("AuthorizeTeamSource: %v", err)
	}

	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))

	timeout := 1
	cfg := config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60}
	started := time.Now()
	_, _, err = RunLogchefQL(ctx, ds, cfg, src, LogchefQLQueryRequest{
		Query:        `level="error"`,
		StartTime:    "2026-04-08 00:00:00",
		EndTime:      "2026-04-08 01:00:00",
		QueryTimeout: &timeout,
	})
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunLogchefQL error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("RunLogchefQL took %s with a 1 s timeout", elapsed)
	}
}

// stallSchemaProxy forwards TCP traffic to upstream. When a client sends a
// query that reads system.columns, the proxy stops forwarding in both
// directions on that connection and keeps it open until the test ends.
func stallSchemaProxy(t *testing.T, upstream string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = listener.Close()
	})
	marker := []byte("system.columns")
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = client.Close()
				continue
			}
			stalled := make(chan struct{})
			go func() {
				<-done
				_ = client.Close()
				_ = server.Close()
			}()
			go func() {
				// Server to client stops once the client asks for the schema.
				buf := make([]byte, 32*1024)
				for {
					n, err := server.Read(buf)
					if err != nil {
						return
					}
					select {
					case <-stalled:
						return
					default:
					}
					if _, err := client.Write(buf[:n]); err != nil {
						return
					}
				}
			}()
			go func() {
				buf := make([]byte, 32*1024)
				// tail keeps the last bytes seen, so a marker split across
				// reads is still found.
				var tail []byte
				for {
					n, err := client.Read(buf)
					if err != nil {
						return
					}
					tail = append(tail, buf[:n]...)
					if bytes.Contains(tail, marker) {
						close(stalled)
						return
					}
					if _, err := server.Write(buf[:n]); err != nil {
						return
					}
					keep := min(len(tail), len(marker)-1)
					tail = append(tail[:0], tail[len(tail)-keep:]...)
				}
			}()
		}
	}()
	return listener.Addr().String()
}
