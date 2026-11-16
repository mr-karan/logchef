package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/pkg/models"
)

const silencesPath = "/api/v1/alert-silences"

func silenceID(t *testing.T, data json.RawMessage) models.AlertSilenceID {
	t.Helper()
	var s models.AlertSilence
	if err := json.Unmarshal(data, &s); err != nil || s.ID == 0 {
		t.Fatalf("silence response %s: %v", data, err)
	}
	return s.ID
}

// TestSilenceAccess walks the silence access table of RFC 0006: anyone who
// sees the source lists silences and, with alerts:write, creates one; only
// the creator and global admins edit or delete it.
func TestSilenceAccess(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	db := e.s.store
	admin := mkTestUser(t, db, "silence-admin@example.com", models.UserRoleAdmin)
	creator := mkTestUser(t, db, "silence-creator@example.com", models.UserRoleMember)
	oncall := mkTestUser(t, db, "silence-oncall@example.com", models.UserRoleMember)
	outsider := mkTestUser(t, db, "silence-outsider@example.com", models.UserRoleMember)
	_, src := mkTestTeam(t, db, "silence-team", admin, creator, oncall)
	write := []models.TokenScope{models.TokenScopeAlertsRead, models.TokenScopeAlertsWrite}
	tok := map[*models.User]string{}
	for _, u := range []*models.User{admin, creator, oncall, outsider} {
		tok[u] = e.token(u, write...)
	}
	readOnly := e.token(oncall, models.TokenScopeAlertsRead)

	start := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	end := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	body := func(comment string) string {
		return fmt.Sprintf(`{"source_id":%d,"matchers":{"service":"checkout"},"comment":%q,"schedule_kind":"once","starts_at":%q,"ends_at":%q}`, src.ID, comment, start, end)
	}

	id := silenceID(t, e.expect(tok[creator], http.MethodPost, silencesPath, body("deploy"), http.StatusCreated))
	e.expect(tok[outsider], http.MethodPost, silencesPath, body("not mine"), http.StatusForbidden)
	e.expect(readOnly, http.MethodPost, silencesPath, body("read only"), http.StatusForbidden)
	oncallID := silenceID(t, e.expect(tok[oncall], http.MethodPost, silencesPath, body("on-call mutes a rule they did not write"), http.StatusCreated))

	list := func(token string, want int) []models.AlertSilence {
		var out []models.AlertSilence
		data := e.expect(token, http.MethodGet, fmt.Sprintf("%s?source_id=%d", silencesPath, src.ID), "", want)
		if want == http.StatusOK {
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	if got := list(readOnly, http.StatusOK); len(got) != 2 || !got[0].Active || got[0].CreatedBy == nil || *got[0].CreatedBy != creator.ID {
		t.Fatalf("listed = %+v, want two active silences, the first by its creator", got)
	}
	list(tok[outsider], http.StatusForbidden)
	e.expect(tok[creator], http.MethodGet, silencesPath, "", http.StatusBadRequest)

	at := func(id models.AlertSilenceID) string { return fmt.Sprintf("%s/%d", silencesPath, id) }
	e.expect(tok[oncall], http.MethodPut, at(id), body("taken over"), http.StatusForbidden)
	e.expect(tok[outsider], http.MethodPut, at(id), body("hidden"), http.StatusNotFound)
	e.expect(tok[creator], http.MethodPut, at(id), body("deploy, extended"), http.StatusOK)
	e.expect(tok[admin], http.MethodPut, at(id), body("deploy, by an admin"), http.StatusOK)
	e.expect(tok[oncall], http.MethodDelete, at(id), "", http.StatusForbidden)
	e.expect(tok[outsider], http.MethodDelete, at(id), "", http.StatusNotFound)
	e.expect(tok[admin], http.MethodDelete, at(oncallID), "", http.StatusOK)
	e.expect(tok[creator], http.MethodDelete, at(id), "", http.StatusOK)
	e.expect(tok[creator], http.MethodDelete, at(id), "", http.StatusNotFound)

	events, err := db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, ev := range events {
		if !strings.HasPrefix(ev.Action, "alert_silence.") || ev.Outcome != models.AuditOutcomeSuccess {
			continue
		}
		counts[ev.Action]++
		if ev.TargetType != "alert_silence" || ev.SourceID == nil || *ev.SourceID != src.ID {
			t.Errorf("event %s target = %s/%s source %v, want an alert_silence on source %d", ev.Action, ev.TargetType, ev.TargetID, ev.SourceID, src.ID)
		}
	}
	want := map[string]int{string(audit.AlertSilenceCreate): 2, string(audit.AlertSilenceUpdate): 2, string(audit.AlertSilenceDelete): 2}
	for action, n := range want {
		if counts[action] != n {
			t.Errorf("successful %s events = %d, want %d (all: %v)", action, counts[action], n, counts)
		}
	}
}

// rejected posts body and returns the error message of a 400.
func rejected(t *testing.T, e *collectionRouteEnv, token, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, silencesPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST %s = %d, want 400 (message %q)", silencesPath, resp.StatusCode, envelope.Message)
	}
	return envelope.Message
}

// TestSilenceValidation: each limit of a silence is a 400 that names the
// field.
func TestSilenceValidation(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	db := e.s.store
	user := mkTestUser(t, db, "silence-limits@example.com", models.UserRoleMember)
	_, src := mkTestTeam(t, db, "silence-limits", user)
	_, other := mkTestTeam(t, db, "silence-other", user)
	tok := e.token(user, models.TokenScopeAlertsRead, models.TokenScopeAlertsWrite)
	alert := &models.Alert{SourceID: other.ID, Name: "elsewhere", Query: "SELECT 1", QueryLanguage: models.QueryLanguageClickHouseSQL,
		EditorMode: models.AlertEditorModeNative, LookbackSeconds: 60, FrequencySeconds: 60, ThresholdOperator: models.AlertThresholdGreaterThan,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved}
	if err := db.CreateAlert(context.Background(), alert); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	rfc := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	once := func(extra string, from, to time.Duration) string {
		return fmt.Sprintf(`{"source_id":%d,"comment":"why","schedule_kind":"once","starts_at":%q,"ends_at":%q%s}`, src.ID, rfc(from), rfc(to), extra)
	}
	weekly := func(window string) string {
		return fmt.Sprintf(`{"source_id":%d,"comment":"batch","schedule_kind":"weekly","weekly":%s}`, src.ID, window)
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no comment", strings.Replace(once("", 0, time.Hour), `"why"`, `" "`, 1), "comment"},
		{"once longer than 30 days", once("", 0, 31*24*time.Hour), "30 days"},
		{"ends before it starts", once("", time.Hour, time.Minute), "after starts_at"},
		{"ended already", once("", -2*time.Hour, -time.Hour), "in the past"},
		{"six matchers", once(`,"matchers":{"a":"1","b":"2","c":"3","d":"4","e":"5","f":"6"}`, 0, time.Hour), "at most 5"},
		{"rule on another source", once(fmt.Sprintf(`,"alert_id":%d`, alert.ID), 0, time.Hour), "alert_id"},
		{"unknown schedule", strings.Replace(once("", 0, time.Hour), `"once"`, `"daily"`, 1), "schedule_kind"},
		{"weekly window over 24 hours", weekly(`{"weekdays":[0],"start":"02:00","duration_minutes":1441,"timezone":"UTC"}`), "duration_minutes"},
		{"weekly day out of range", weekly(`{"weekdays":[7],"start":"02:00","duration_minutes":60,"timezone":"UTC"}`), "weekdays"},
		{"weekly bad start", weekly(`{"weekdays":[1],"start":"2am","duration_minutes":60,"timezone":"UTC"}`), "start"},
		{"weekly unknown timezone", weekly(`{"weekdays":[1],"start":"02:00","duration_minutes":60,"timezone":"Mars/Olympus"}`), "timezone"},
		{"weekly without a window", fmt.Sprintf(`{"source_id":%d,"comment":"x","schedule_kind":"weekly"}`, src.ID), "weekly is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := rejected(t, e, tok, tc.body); !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not name %q", msg, tc.want)
			}
		})
	}

	window := `{"weekdays":[6,0],"start":"23:00","duration_minutes":120,"timezone":"Asia/Kolkata"}`
	data := e.expect(tok, http.MethodPost, silencesPath, weekly(window), http.StatusCreated)
	var s models.AlertSilence
	if err := json.Unmarshal(data, &s); err != nil || s.Weekly == nil || fmt.Sprint(s.Weekly.Weekdays) != "[0 6]" || s.StartsAt != nil {
		t.Fatalf("weekly silence = %s, %v; want sorted weekdays and no starts_at", data, err)
	}

	for i := 1; i < models.MaxSilencesPerSource; i++ {
		e.expect(tok, http.MethodPost, silencesPath, once("", 0, time.Hour), http.StatusCreated)
	}
	if msg := rejected(t, e, tok, once("", 0, time.Hour)); !strings.Contains(msg, "at most 100") {
		t.Errorf("silence 101 error = %q, want the per-source limit", msg)
	}
}
