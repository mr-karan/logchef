package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// logIdentityAudit makes the env's recorder store through st and write the
// audit log line to the env's log buffer.
func logIdentityAudit(t *testing.T, e *auditRouteEnv, st store.AuditStore) {
	t.Helper()
	e.s.audit = audit.New(audit.Options{
		Store:     strictAuditStore{AuditStore: st, t: t},
		Logger:    slog.New(slog.NewJSONHandler(e.logs, nil)),
		LogEvents: true,
	})
}

func identityChanges(t *testing.T, ev models.AuditEvent) map[string]map[string]any {
	t.Helper()
	var changes map[string]map[string]any
	if err := json.Unmarshal(ev.Changes, &changes); err != nil {
		t.Fatalf("changes %s: %v", ev.Changes, err)
	}
	return changes
}

func wantChange(t *testing.T, ev models.AuditEvent, key string, from, to any) {
	t.Helper()
	c := identityChanges(t, ev)[key]
	if c["from"] != from || c["to"] != to {
		t.Errorf("%s %s change = %v, want from %v to %v", ev.Action, key, c, from, to)
	}
}

func TestAuditUserEventsRecordFields(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	logIdentityAudit(t, e, e.db)
	user := mkTestUser(t, e.db, "subject@example.com", models.UserRoleMember)
	path := fmt.Sprintf("/api/v1/admin/users/%d", user.ID)

	wantStatus(t, e.do(http.MethodPut, path, `{"role":"admin"}`, e.cookie), http.StatusOK)
	ev := e.only()
	checkActorAndTarget(t, ev, "user.update", e.admin, "user", strconv.Itoa(int(user.ID)))
	wantChange(t, ev, "role", "member", "admin")
	if got := identityChanges(t, ev); len(got) != 1 {
		t.Errorf("role change records %s, want only the role", ev.Changes)
	}
	e.reset()

	wantStatus(t, e.do(http.MethodDelete, path, ``, e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "user.delete", e.admin, "user", strconv.Itoa(int(user.ID)))
	wantChange(t, ev, "email", "subject@example.com", nil)
}

// Two concurrent updates of one user each record only their own change: the
// before of each comes from the transaction that writes, not from a read
// that the other update can overtake.
func TestAuditConcurrentUserUpdatesRecordOwnChange(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	user := mkTestUser(t, e.db, "contended@example.com", models.UserRoleMember)
	path := fmt.Sprintf("/api/v1/admin/users/%d", user.ID)

	const rounds = 10
	for i := range rounds {
		bodies := []string{
			fmt.Sprintf(`{"full_name":"Name %c"}`, 'A'+i),
			map[bool]string{true: `{"role":"admin"}`, false: `{"role":"member"}`}[i%2 == 0],
		}
		var wg sync.WaitGroup
		for _, body := range bodies {
			wg.Go(func() { wantStatus(t, e.do(http.MethodPut, path, body, e.cookie), http.StatusOK) })
		}
		wg.Wait()
	}

	events := e.events()
	if len(events) != 2*rounds {
		t.Fatalf("stored %d events, want %d", len(events), 2*rounds)
	}
	for i := range events {
		changes := identityChanges(t, events[i])
		_, name := changes["full_name"]
		_, role := changes["role"]
		if len(changes) != 1 || name == role {
			t.Errorf("event %d changes %s, want exactly one of full_name and role", i, events[i].Changes)
		}
	}
}

func TestAuditTeamAndMembershipEventsRecordFields(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	team := &models.Team{Name: "audit-identity"}
	if err := e.db.CreateTeam(context.Background(), team); err != nil {
		t.Fatal(err)
	}
	member := mkTestUser(t, e.db, "teammate@example.com", models.UserRoleMember)
	teamPath := fmt.Sprintf("/api/v1/teams/%d", team.ID)
	memberTarget := strconv.Itoa(int(member.ID))

	wantStatus(t, e.do(http.MethodPut, teamPath, `{"name":"audit-renamed"}`, e.cookie), http.StatusOK)
	ev := e.only()
	checkActorAndTarget(t, ev, "team.update", e.admin, "team", strconv.Itoa(int(team.ID)))
	wantChange(t, ev, "name", "audit-identity", "audit-renamed")
	e.reset()

	add := func(role string) {
		t.Helper()
		wantStatus(t, e.do(http.MethodPost, teamPath+"/members", fmt.Sprintf(`{"user_id":%d,"role":%q}`, member.ID, role), e.cookie), http.StatusOK)
	}
	add("member")
	wantChange(t, e.only(), "role", nil, "member")
	e.reset()
	// Adding an existing member changes the role.
	add("admin")
	ev = e.only()
	checkActorAndTarget(t, ev, "team_member.add", e.admin, "team_member", memberTarget)
	wantChange(t, ev, "role", "member", "admin")
	e.reset()

	wantStatus(t, e.do(http.MethodDelete, teamPath+"/members/"+memberTarget, ``, e.cookie), http.StatusOK)
	wantChange(t, e.only(), "role", "admin", nil)
	e.reset()
	// Removing a user who is not a member is a no-op and records nothing.
	wantStatus(t, e.do(http.MethodDelete, teamPath+"/members/"+memberTarget, ``, e.cookie), http.StatusOK)
	if got := e.events(); len(got) != 0 {
		t.Errorf("removing a non-member stored %d events", len(got))
	}
}

func TestAuditAPITokenEventsRecordNoToken(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	logIdentityAudit(t, e, e.db)

	resp := e.do(http.MethodPost, "/api/v1/me/tokens", `{"name":"ci","scopes":["profile:read"]}`, e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var created struct {
		Data models.CreateAPITokenResponse `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &created); err != nil || created.Data.Token == "" {
		t.Fatalf("decode created token: %v", err)
	}
	ev := e.only()
	checkActorAndTarget(t, ev, "api_token.create", e.admin, "api_token", strconv.Itoa(created.Data.APIToken.ID))
	wantChange(t, ev, "name", nil, "ci")
	stored := string(ev.Changes) + string(ev.Detail) + e.logs.String()
	if strings.Contains(stored, created.Data.Token) || strings.Contains(stored, created.Data.APIToken.Prefix) {
		t.Error("the token or its prefix reached the event or the log")
	}
	if !strings.Contains(e.logs.String(), `"stored":true`) {
		t.Error("no audit log line after the commit")
	}
	e.reset()

	tokenPath := fmt.Sprintf("/api/v1/me/tokens/%d", created.Data.APIToken.ID)
	wantStatus(t, e.do(http.MethodDelete, tokenPath, ``, e.cookie), http.StatusOK)
	wantChange(t, e.only(), "name", "ci", nil)
	e.reset()
	wantStatus(t, e.do(http.MethodDelete, tokenPath, ``, e.cookie), http.StatusNotFound)
	if got := e.events(); len(got) != 0 {
		t.Errorf("deleting a deleted token stored %d events", len(got))
	}
}

var errAuditDown = errors.New("audit table unavailable")

// auditDownStore fails every audit insert, on the root store and inside
// WithTx.
type auditDownStore struct{ store.Store }

func (auditDownStore) InsertAuditEvent(context.Context, *models.AuditEvent) error {
	return errAuditDown
}

func (s auditDownStore) WithTx(ctx context.Context, fn func(store.StoreOps) error) error {
	return s.Store.WithTx(ctx, func(tx store.StoreOps) error { return fn(auditDownOps{tx}) })
}

type auditDownOps struct{ store.StoreOps }

func (auditDownOps) InsertAuditEvent(context.Context, *models.AuditEvent) error {
	return errAuditDown
}

// Token creation fails closed: without a stored event no token exists.
func TestAuditTokenCreationFailsClosed(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	down := auditDownStore{Store: e.db}
	e.s.store = down
	e.s.audit = audit.New(audit.Options{
		Store:     down,
		Logger:    slog.New(slog.NewJSONHandler(e.logs, nil)),
		LogEvents: true,
	})
	account, err := core.CreateServiceAccount(context.Background(), e.db, e.s.log, "audit-bot")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path  string
		owner models.UserID
	}{
		{"/api/v1/me/tokens", e.admin.ID},
		{fmt.Sprintf("/api/v1/admin/service-accounts/%d/tokens", account.ID), account.ID},
	} {
		wantStatus(t, e.do(http.MethodPost, tc.path, `{"name":"blocked","scopes":["profile:read"]}`, e.cookie), http.StatusServiceUnavailable)
		tokens, err := e.db.ListAPITokensForUser(context.Background(), tc.owner)
		if err != nil {
			t.Fatal(err)
		}
		if len(tokens) != 0 {
			t.Errorf("%s: %d tokens exist after the audit insert failed", tc.path, len(tokens))
		}
	}
	if strings.Contains(e.logs.String(), `"stored":true`) {
		t.Errorf("a log line says stored true: %s", e.logs)
	}
}
