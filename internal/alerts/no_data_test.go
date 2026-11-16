package alerts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// withPolicy stores the fixture's rule with the given policy and group_by.
func (f *groupedFixture) withPolicy(t *testing.T, policy models.AlertNoDataPolicy, notifyOnResolve bool, groupBy ...string) {
	t.Helper()
	f.alert.NoDataPolicy = policy
	f.alert.NotifyOnResolve = notifyOnResolve
	f.alert.GroupBy = groupBy
	if len(groupBy) == 0 {
		f.alert.Query = "SELECT count() AS value FROM logs"
	}
	if err := f.db.UpdateAlert(t.Context(), f.alert); err != nil {
		t.Fatal(err)
	}
}

// tickAt evaluates the stored rule at at and returns the notifications it
// sent as "status service".
func (f *groupedFixture) tickAt(t *testing.T, at time.Time, result *models.QueryResult) []string {
	t.Helper()
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.results = append(f.provider.results, result)
	f.sent = nil
	if err := f.manager.evaluateAlert(t.Context(), alert, at); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := make([]string, 0, len(f.sent))
	for i := range f.sent {
		got = append(got, strings.TrimSpace(string(f.sent[i].Status)+" "+f.sent[i].Group["service"]))
	}
	slices.Sort(got)
	return got
}

func (f *groupedFixture) lastState(t *testing.T) models.AlertState {
	t.Helper()
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	return alert.LastState
}

func ungroupedResult(rows ...map[string]any) *models.QueryResult {
	return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "value", Type: "UInt64"}}, Logs: rows}
}

func expectSent(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s sent %v, want %v", step, got, want)
	}
}

// TestNoDataPolicyUngrouped: under no_data, an empty result opens one no_data
// episode and notifies once; data below the threshold resolves it.
func TestNoDataPolicyUngrouped(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	expectSent(t, "first empty tick", f.tickAt(t, at, ungroupedResult()), "no_data")
	expectSent(t, "second empty tick", f.tickAt(t, at.Add(time.Minute), ungroupedResult()))
	if got := f.lastState(t); got != models.AlertStateNoData {
		t.Fatalf("last_state = %s, want no_data", got)
	}
	instances, err := f.db.ListAlertInstances(t.Context(), f.alert.ID)
	if err != nil || len(instances) != 1 || instances[0].State != models.AlertInstanceStateNoData || instances[0].Value != nil {
		t.Fatalf("instances = %+v, %v; want one no_data instance without a value", instances, err)
	}
	history, err := f.db.ListAlertHistory(t.Context(), f.alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusNoData || history[0].Value != nil {
		t.Fatalf("history = %+v, %v; want one open no_data episode", history, err)
	}

	expectSent(t, "tick with data below the threshold", f.tickAt(t, at.Add(2*time.Minute), ungroupedResult(map[string]any{"value": uint64(3)})), "resolved")
	if got := f.lastState(t); got != models.AlertStateResolved {
		t.Fatalf("last_state = %s, want resolved", got)
	}
	instances, err = f.db.ListAlertInstances(t.Context(), f.alert.ID)
	if err != nil || len(instances) != 1 || instances[0].State != models.AlertInstanceStateOK || instances[0].LastSeenAt == nil || !instances[0].LastSeenAt.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("instances = %+v, %v; want the ok group tracked, last seen at the tick's due time", instances, err)
	}
}

// TestNoDataEpisodeMovesState: a firing group that stops reporting keeps its
// episode, which moves to no_data and notifies once; data that meets the
// threshold moves it back to firing.
func TestNoDataEpisodeMovesState(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true, "service")
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	expectSent(t, "fire", f.tickAt(t, at, groupedResult(row("checkout", 12))), "triggered checkout")
	expectSent(t, "silent", f.tickAt(t, at.Add(time.Minute), groupedResult()), "no_data checkout")
	expectSent(t, "still silent", f.tickAt(t, at.Add(2*time.Minute), groupedResult()))
	expectSent(t, "back", f.tickAt(t, at.Add(3*time.Minute), groupedResult(row("checkout", 20))), "triggered checkout")

	history, err := f.db.ListAlertHistory(t.Context(), f.alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusTriggered || history[0].ResolvedAt != nil {
		t.Fatalf("history = %+v, %v; want one open episode, firing again", history, err)
	}
	if got := f.lastState(t); got != models.AlertStateFiring {
		t.Fatalf("last_state = %s, want firing", got)
	}
}

// TestNotifyOnResolveOff: a rule with notify_on_resolve off closes its
// episode without a resolved notification, also on manual resolve.
func TestNotifyOnResolveOff(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataZero, false, "service")
	expectSent(t, "fire", f.tick(t, groupedResult(row("checkout", 12), row("search", 30))), "triggered checkout", "triggered search")
	expectSent(t, "resolve", f.tick(t, groupedResult(row("checkout", 1), row("search", 30))))
	f.sent = nil
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, nil, "fixed"); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("manual resolve sent %d notifications, want none", len(f.sent))
	}
	open, err := f.db.ListOpenAlertHistory(t.Context(), f.alert.ID)
	if err != nil || len(open) != 0 {
		t.Fatalf("open episodes = %d, %v; want none", len(open), err)
	}
}

// TestForgetGroupKeep: under keep, a firing group without a row keeps firing
// for 24 hours, then is forgotten: its episode closes with the reason and its
// resolved notification goes out.
func TestForgetGroupKeep(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataKeep, true, "service")
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	expectSent(t, "fire", f.tickAt(t, at, groupedResult(row("checkout", 12), row("search", 1))), "triggered checkout")
	expectSent(t, "silent for a day less a minute", f.tickAt(t, at.Add(ForgetAfter-time.Minute), groupedResult(row("search", 1))))
	if got := f.instances(t); got["checkout"] != models.AlertInstanceStateFiring || got["search"] != models.AlertInstanceStateOK {
		t.Fatalf("instances = %v, want checkout firing and search ok", got)
	}
	expectSent(t, "silent for a day", f.tickAt(t, at.Add(ForgetAfter), groupedResult(row("search", 1))), "resolved checkout")
	if got := f.instances(t); len(got) != 1 || got["search"] != models.AlertInstanceStateOK {
		t.Fatalf("instances = %v, want only search", got)
	}
	history, err := f.db.ListAlertHistory(t.Context(), f.alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusResolved || history[0].Message != "group forgotten after 24 hours without data" {
		t.Fatalf("history = %+v, %v; want the episode closed as forgotten", history, err)
	}
}

// TestForgetGroupNoData: a forgotten no_data group sends its resolved; a
// forgotten ok group goes without a notification.
func TestForgetGroupNoData(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true, "service")
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	expectSent(t, "both report", f.tickAt(t, at, groupedResult(row("checkout", 1), row("search", 1))))
	expectSent(t, "checkout stops", f.tickAt(t, at.Add(time.Minute), groupedResult(row("search", 1))), "no_data checkout")
	expectSent(t, "a day later", f.tickAt(t, at.Add(ForgetAfter), groupedResult(row("search", 1))), "resolved checkout")
	if got := f.instances(t); len(got) != 1 || got["search"] != models.AlertInstanceStateOK {
		t.Fatalf("instances = %v, want only search", got)
	}
	// No evaluation ran for a day, so search is forgotten while ok.
	expectSent(t, "after a day of downtime", f.tickAt(t, at.Add(2*ForgetAfter), groupedResult(row("other", 1))))
	if got := f.instances(t); len(got) != 1 || got["other"] != models.AlertInstanceStateOK {
		t.Fatalf("instances = %v, want only other", got)
	}
}

// TestTrackedGroupCapRecordsError: a rule that would track more than 200
// groups records an error row and changes no instance.
func TestTrackedGroupCapRecordsError(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataKeep, true, "service")
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for i := range MaxTrackedGroups {
		service := fmt.Sprintf("svc-%d", i)
		if err := f.db.UpsertAlertInstance(t.Context(), &models.AlertInstance{
			AlertID: f.alert.ID, GroupKey: `["` + service + `"]`, Group: map[string]string{"service": service},
			State: models.AlertInstanceStateOK, StateSince: at, LastSeenAt: &at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.results = append(f.provider.results, groupedResult(row("new", 1)))
	if err := f.manager.evaluateAlert(t.Context(), alert, at.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "more than 200 groups") {
		t.Fatalf("evaluate err = %v, want the tracked-group cap", err)
	}
	if got := f.instances(t); len(got) != MaxTrackedGroups {
		t.Fatalf("instances = %d, want %d unchanged", len(got), MaxTrackedGroups)
	}
	history, err := f.db.ListAlertHistory(t.Context(), f.alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusError {
		t.Fatalf("history = %+v, %v; want one error row", history, err)
	}
}

// TestNoDataWebhookV1: a version 1 webhook receives a no_data episode as
// triggered, with annotations.reason = "no_data".
func TestNoDataWebhookV1(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true)
	var bodies []map[string]any
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	linkWebhooks(t, f.db, f.alert, receiver.URL)
	f.manager.sender = NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})

	f.tickAt(t, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), ungroupedResult())
	if len(bodies) != 1 {
		t.Fatalf("webhook received %d requests, want 1", len(bodies))
	}
	body := bodies[0]
	annotations, _ := body["annotations"].(map[string]any)
	if body["status"] != "triggered" || annotations["reason"] != "no_data" || annotations["status"] != "no_data" || !strings.Contains(body["message"].(string), "no data") {
		t.Fatalf("payload = %v, want status triggered with reason no_data and a message that says so", body)
	}
	if _, ok := annotations["value"]; ok {
		t.Errorf("annotations carry a value for a group without data: %v", annotations)
	}
}
