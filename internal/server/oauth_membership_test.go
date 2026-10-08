package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// adminWorld is an admin user who is a member of team A only. Team B (with
// source B) exists, and another user owns the objects under test.
type adminWorld struct {
	e              *oauthEnv
	teamA, teamB   *models.Team
	srcA, srcB     *models.Source
	queryA, queryB *models.SavedQuery
	alertA, alertB *models.Alert
	collection     *models.Collection
	exportID       string
	oauthToken     string
	pat            string
}

func newAdminWorld(t *testing.T) *adminWorld {
	t.Helper()
	cfg := testOAuthConfig()
	cfg.Alerts.Enabled = true
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleAdmin)
	ctx := context.Background()
	other := mkTestUser(t, e.db, "owner@example.com", models.UserRoleMember)
	w := &adminWorld{e: e}
	w.teamA, w.srcA = mkTestTeam(t, e.db, "teama", e.user, other)
	w.teamB, w.srcB = mkTestTeam(t, e.db, "teamb", other)

	var err error
	mkQuery := func(src *models.Source, team *models.Team) *models.SavedQuery {
		q, err := e.db.CreateSavedQuery(ctx, src.ID, &team.ID, "q-"+src.Name, "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeNative, `level="error"`, &other.ID)
		if err != nil {
			t.Fatalf("CreateSavedQuery: %v", err)
		}
		return q
	}
	w.queryA, w.queryB = mkQuery(w.srcA, w.teamA), mkQuery(w.srcB, w.teamB)
	mkAlert := func(src *models.Source) *models.Alert {
		a := &models.Alert{
			SourceID: src.ID, Name: "alert-" + src.Name, QueryLanguage: models.QueryLanguageClickHouseSQL,
			EditorMode: models.AlertEditorModeNative, Query: "SELECT count() FROM logs", LookbackSeconds: 300,
			ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10, FrequencySeconds: 60,
			Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved, CreatedBy: &other.ID,
		}
		if err := e.db.CreateAlert(ctx, a); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
		return a
	}
	w.alertA, w.alertB = mkAlert(w.srcA), mkAlert(w.srcB)
	if w.collection, err = e.db.CreateCollection(ctx, "shared", "", false, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddCollectionMember(ctx, w.collection.ID, other.ID, models.CollectionRoleOwner, &other.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddCollectionMember(ctx, w.collection.ID, e.user.ID, models.CollectionRoleMember, &other.ID); err != nil {
		t.Fatal(err)
	}
	w.exportID = "export-of-other-user"
	if err := e.db.CreateExportJob(ctx, &models.ExportJob{
		ID: w.exportID, SourceID: w.srcA.ID, CreatedBy: other.ID, Status: models.ExportJobStatusPending,
		Format: "csv", RequestPayload: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	p := newPKCE()
	w.oauthToken = e.tokens(cliAuthorizeParams(p), p).AccessToken
	pat, err := core.CreateAPIToken(ctx, e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "pat", nil, []models.TokenScope{models.TokenScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	w.pat = pat.Token
	return w
}

func itoa[T ~int | ~int64](v T) string { return strconv.FormatInt(int64(v), 10) }

// Integration decision 1: an admin's OAuth token gets membership-only access
// on every HTTP route an OAuth scope reaches. The same admin's session and
// PAT keep the global-admin bypass.
func TestOAuthAdminIsMembershipOnlyHTTP(t *testing.T) {
	t.Parallel()
	w := newAdminWorld(t)
	e := w.e
	teamB := "/api/v1/teams/" + itoa(w.teamB.ID)
	exportPath := "/api/v1/teams/" + itoa(w.teamA.ID) + "/sources/" + itoa(w.srcA.ID) + "/exports/" + w.exportID
	membersPath := "/api/v1/collections/" + strconv.Itoa(w.collection.ID) + "/members"
	collectionTeamsPath := "/api/v1/collections/" + strconv.Itoa(w.collection.ID) + "/teams"

	for _, tc := range []struct {
		path              string
		oauth, privileged int
	}{
		{teamB, http.StatusForbidden, http.StatusOK},
		{teamB + "/sources", http.StatusForbidden, http.StatusOK},
		{teamB + "/members", http.StatusForbidden, http.StatusOK},
		{"/api/v1/saved-queries/" + strconv.Itoa(w.queryB.ID), http.StatusNotFound, http.StatusNotFound},
		{"/api/v1/alerts/" + itoa(w.alertB.ID), http.StatusNotFound, http.StatusNotFound},
		{exportPath, http.StatusForbidden, http.StatusOK},
		{membersPath, http.StatusForbidden, http.StatusOK},
		{collectionTeamsPath, http.StatusForbidden, http.StatusOK},
		{"/api/v1/teams/" + itoa(w.teamA.ID), http.StatusOK, http.StatusOK},
		{"/api/v1/saved-queries/" + strconv.Itoa(w.queryA.ID), http.StatusOK, http.StatusOK},
	} {
		if resp := e.apiGet(tc.path, w.oauthToken); resp.StatusCode != tc.oauth {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("OAuth GET %s status %d, want %d: %s", tc.path, resp.StatusCode, tc.oauth, body)
		}
		if resp := e.apiGet(tc.path, w.pat); resp.StatusCode != tc.privileged {
			t.Errorf("PAT GET %s status %d, want %d", tc.path, resp.StatusCode, tc.privileged)
		}
		if resp := e.consentRequest(http.MethodGet, tc.path, e.session, "", nil); resp.StatusCode != tc.privileged {
			t.Errorf("session GET %s status %d, want %d", tc.path, resp.StatusCode, tc.privileged)
		}
	}

	ids := func(path string, field string) []float64 {
		t.Helper()
		resp := e.apiGet(path, w.oauthToken)
		var out struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status %d err %v", path, resp.StatusCode, err)
		}
		var got []float64
		for _, row := range out.Data {
			got = append(got, row[field].(float64))
		}
		return got
	}
	if got := ids("/api/v1/me/teams", "id"); !slices.Equal(got, []float64{float64(w.teamA.ID)}) {
		t.Errorf("OAuth /me/teams ids %v, want only team A", got)
	}
	if got := ids("/api/v1/saved-queries", "id"); !slices.Equal(got, []float64{float64(w.queryA.ID)}) {
		t.Errorf("OAuth /saved-queries ids %v, want only query A", got)
	}
	if got := ids("/api/v1/alerts", "id"); !slices.Equal(got, []float64{float64(w.alertA.ID)}) {
		t.Errorf("OAuth /alerts ids %v, want only alert A", got)
	}
}

// Mapped collection errors and refused export reads used to be overwritten by
// a 500 (the error helpers return nil after sending). A plain member now gets
// the intended status.
func TestCollectionAndExportRefusalsKeepStatus(t *testing.T) {
	t.Parallel()
	w := newAdminWorld(t)
	e := w.e
	member, memberSession := e.newSessionUser("member@example.com")
	if err := e.db.AddTeamMember(context.Background(), w.teamA.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/collections/" + strconv.Itoa(w.collection.ID) + "/members", http.StatusNotFound},
		{"/api/v1/collections/" + strconv.Itoa(w.collection.ID), http.StatusNotFound},
		{"/api/v1/teams/" + itoa(w.teamA.ID) + "/sources/" + itoa(w.srcA.ID) + "/exports/" + w.exportID, http.StatusForbidden},
		{"/api/v1/teams/" + itoa(w.teamA.ID) + "/sources/" + itoa(w.srcA.ID) + "/exports/" + w.exportID + "/download", http.StatusForbidden},
	} {
		if resp := e.consentRequest(http.MethodGet, tc.path, memberSession, "", nil); resp.StatusCode != tc.want {
			t.Errorf("member GET %s status %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}
