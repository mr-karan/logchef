package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestTransitionTwoGroups: two groups of one rule fire at different ticks and
// resolve at different ticks, each on its own.
func TestTransitionTwoGroups(t *testing.T) {
	const (
		ok     = models.AlertInstanceStateOK
		firing = models.AlertInstanceStateFiring
	)
	rule := Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: models.AlertNoDataZero, Grouped: true}
	a := map[string]string{"service": "a"}
	b := map[string]string{"service": "b"}
	sample := func(key string, group map[string]string, value float64) Sample {
		return Sample{GroupKey: key, Group: group, Value: value}
	}

	var tracked []Instance
	tick := func(samples ...Sample) []Change {
		changes, err := Transition(rule, tracked, samples, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		tracked = tracked[:0]
		for _, change := range changes {
			if change.To == firing {
				tracked = append(tracked, Instance{GroupKey: change.GroupKey, Group: change.Group, State: firing})
			}
		}
		return changes
	}

	for _, step := range []struct {
		name    string
		samples []Sample
		want    []Change
	}{
		{
			name:    "a fires, b stays ok",
			samples: []Sample{sample(`["a"]`, a, 12), sample(`["b"]`, b, 3)},
			want: []Change{
				{GroupKey: `["a"]`, Group: a, From: ok, To: firing, Value: 12, Sampled: true},
				{GroupKey: `["b"]`, Group: b, From: ok, To: ok, Value: 3, Sampled: true, Untracked: true},
			},
		},
		{
			name:    "b fires while a keeps firing",
			samples: []Sample{sample(`["a"]`, a, 15), sample(`["b"]`, b, 20)},
			want: []Change{
				{GroupKey: `["a"]`, Group: a, From: firing, To: firing, Value: 15, Sampled: true},
				{GroupKey: `["b"]`, Group: b, From: ok, To: firing, Value: 20, Sampled: true},
			},
		},
		{
			name:    "a resolves, b keeps firing",
			samples: []Sample{sample(`["a"]`, a, 4), sample(`["b"]`, b, 30)},
			want: []Change{
				{GroupKey: `["a"]`, Group: a, From: firing, To: ok, Value: 4, Sampled: true, Untracked: true},
				{GroupKey: `["b"]`, Group: b, From: firing, To: firing, Value: 30, Sampled: true},
			},
		},
		{
			name:    "b stops reporting and compares 0",
			samples: []Sample{sample(`["a"]`, a, 1)},
			want: []Change{
				{GroupKey: `["a"]`, Group: a, From: ok, To: ok, Value: 1, Sampled: true, Untracked: true},
				{GroupKey: `["b"]`, Group: b, From: firing, To: ok, Untracked: true},
			},
		},
	} {
		if got := tick(step.samples...); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("%s: changes = %+v, want %+v", step.name, got, step.want)
		}
	}
}

func groupedResult(rows ...map[string]any) *models.QueryResult {
	return &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "service", Type: "String"}, {Name: "value", Type: "UInt64"}},
		Logs:    rows,
	}
}

func TestGroupedSamples(t *testing.T) {
	t.Run("one sample per row", func(t *testing.T) {
		samples, err := SamplesFromResult(groupedResult(
			map[string]any{"service": "checkout", "value": uint64(12)},
			map[string]any{"service": "search", "value": uint64(3)},
		), []string{"service"})
		if err != nil {
			t.Fatal(err)
		}
		want := []Sample{
			{GroupKey: `["checkout"]`, Group: map[string]string{"service": "checkout"}, Value: 12},
			{GroupKey: `["search"]`, Group: map[string]string{"service": "search"}, Value: 3},
		}
		if !reflect.DeepEqual(samples, want) {
			t.Fatalf("samples = %+v, want %+v", samples, want)
		}
	})

	t.Run("value column falls back to the first non-group column", func(t *testing.T) {
		result := &models.QueryResult{
			Columns: []models.ColumnInfo{{Name: "service"}, {Name: "region"}, {Name: "errors"}, {Name: "total"}},
			Logs:    []map[string]any{{"service": "api", "region": "eu", "errors": "7", "total": 100}},
		}
		samples, err := SamplesFromResult(result, []string{"region", "service"})
		if err != nil {
			t.Fatal(err)
		}
		if len(samples) != 1 || samples[0].Value != 7 || samples[0].GroupKey != `["eu","api"]` {
			t.Fatalf("samples = %+v, want value 7 under key [eu,api] in group_by order", samples)
		}
	})

	t.Run("a label left out of one series reads as empty", func(t *testing.T) {
		result := &models.QueryResult{
			Columns: []models.ColumnInfo{{Name: "value"}, {Name: "service"}},
			Logs:    []map[string]any{{"value": "4", "service": "api"}, {"value": "2"}},
		}
		samples, err := SamplesFromResult(result, []string{"service"})
		if err != nil || len(samples) != 2 || samples[1].GroupKey != `[""]` {
			t.Fatalf("samples = %+v, %v; want a second group with an empty value", samples, err)
		}
	})

	t.Run("long values", func(t *testing.T) {
		long := strings.Repeat("x", 600)
		samples, err := SamplesFromResult(groupedResult(map[string]any{"service": long, "value": 1}), []string{"service"})
		if err != nil {
			t.Fatal(err)
		}
		if key := samples[0].GroupKey; len(key) != 64 || strings.Contains(key, "x") {
			t.Errorf("group key = %q, want the SHA-256 hex of a key over 512 bytes", key)
		}
		if got := samples[0].Group["service"]; got != long[:256] {
			t.Errorf("stored group value has %d characters, want 256", len(got))
		}
	})

	for _, tc := range []struct {
		name    string
		result  *models.QueryResult
		groupBy []string
		want    string
	}{
		{
			name: "duplicate group",
			result: groupedResult(
				map[string]any{"service": "checkout", "value": 1},
				map[string]any{"service": "checkout", "value": 2},
			),
			groupBy: []string{"service"},
			want:    "more than one row for the group service=checkout",
		},
		{
			name:    "missing column",
			result:  groupedResult(map[string]any{"service": "checkout", "value": 1}),
			groupBy: []string{"region"},
			want:    `no column "region" named in group_by`,
		},
		{
			name: "truncated result",
			result: &models.QueryResult{
				Columns: groupedResult().Columns,
				Logs:    []map[string]any{{"service": "checkout", "value": 1}},
				Stats:   models.QueryStats{Truncation: models.TruncationRows},
			},
			groupBy: []string{"service"},
			want:    "more than 100 groups",
		},
		{
			name:    "more rows than groups allowed",
			result:  groupedResult(manyGroups(101)...),
			groupBy: []string{"service"},
			want:    "more than 100 groups",
		},
		{
			name:    "no value column",
			result:  &models.QueryResult{Columns: []models.ColumnInfo{{Name: "service"}}, Logs: []map[string]any{{"service": "a"}}},
			groupBy: []string{"service"},
			want:    "no value column",
		},
		{
			name:    "value that is not a number",
			result:  groupedResult(map[string]any{"service": "checkout", "value": "many"}),
			groupBy: []string{"service"},
			want:    "the value of group service=checkout",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples, err := SamplesFromResult(tc.result, tc.groupBy)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("samples = %+v, err = %v; want an error containing %q", samples, err, tc.want)
			}
		})
	}
}

func manyGroups(n int) []map[string]any {
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{"service": strings.Repeat("s", i+1), "value": i}
	}
	return rows
}

// scriptedProvider returns the next result for each evaluation.
type scriptedProvider struct {
	datasource.Provider
	results []*models.QueryResult
}

func (*scriptedProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (p *scriptedProvider) EvaluateAlert(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

type groupedFixture struct {
	db       *sqlite.DB
	alert    *models.Alert
	manager  *Manager
	provider *scriptedProvider
	queue    *queueRecorder
	sent     []AlertNotification
}

func newGroupedFixture(t *testing.T) *groupedFixture {
	t.Helper()
	db, alert, logger := newManagerTestAlert(t)
	alert.GroupBy = []string{"service"}
	alert.Query = "SELECT service, count() AS value FROM logs GROUP BY service"
	if err := db.UpdateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	f := &groupedFixture{db: db, alert: alert, provider: &scriptedProvider{}}
	sources := datasource.NewService(db, logger)
	sources.Register(f.provider)
	f.queue = recordQueue(t, db, alert)
	f.manager = NewManager(Options{DB: db, Datasources: sources, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	return f
}

// tick evaluates the stored rule against one result and returns the
// notifications it sent as "status service".
func (f *groupedFixture) tick(t *testing.T, result *models.QueryResult) []string {
	t.Helper()
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.results = append(f.provider.results, result)
	if err := f.manager.evaluateAlert(t.Context(), alert, time.Now()); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	f.sent = f.queue.take()
	var got []string
	for i := range f.sent {
		n := &f.sent[i]
		if n.Labels["service"] != n.Group["service"] {
			t.Errorf("notification labels %v do not carry the group %v", n.Labels, n.Group)
		}
		got = append(got, string(n.Status)+" "+n.Group["service"])
	}
	slices.Sort(got)
	return got
}

func (f *groupedFixture) instances(t *testing.T) map[string]models.AlertInstanceState {
	t.Helper()
	instances, err := f.db.ListAlertInstances(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]models.AlertInstanceState, len(instances))
	for _, instance := range instances {
		states[instance.Group["service"]] = instance.State
	}
	return states
}

func row(service string, value int) map[string]any {
	return map[string]any{"service": service, "value": uint64(value)}
}

// TestGroupedEvaluation drives a rule grouped by service through the real
// store: two groups fire, one resolves while the other keeps firing, and each
// transition notifies once for its own group.
func TestGroupedEvaluation(t *testing.T) {
	f := newGroupedFixture(t)
	ctx := t.Context()

	if got, want := f.tick(t, groupedResult(row("checkout", 12), row("search", 30), row("auth", 2))), []string{"triggered checkout", "triggered search"}; !slices.Equal(got, want) {
		t.Fatalf("first tick sent %v, want %v", got, want)
	}
	if got, want := f.instances(t), map[string]models.AlertInstanceState{"checkout": "firing", "search": "firing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instances = %v, want %v", got, want)
	}

	if got := f.tick(t, groupedResult(row("checkout", 15), row("search", 31))); len(got) != 0 {
		t.Fatalf("a tick that changes no state sent %v", got)
	}

	if got, want := f.tick(t, groupedResult(row("checkout", 3), row("search", 40))), []string{"resolved checkout"}; !slices.Equal(got, want) {
		t.Fatalf("resolve tick sent %v, want %v", got, want)
	}
	if got, want := f.instances(t), map[string]models.AlertInstanceState{"search": "firing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instances after one resolve = %v, want %v", got, want)
	}
	alert, err := f.db.GetAlert(ctx, f.alert.ID)
	if err != nil || alert.LastState != models.AlertStateFiring {
		t.Fatalf("last_state with one group firing = %v, %v; want firing", alert.LastState, err)
	}

	history, err := f.db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	episodes := map[string]models.AlertStatus{}
	for _, entry := range history {
		episodes[entry.Group["service"]] = entry.Status
		if entry.GroupKey != `["`+entry.Group["service"]+`"]` {
			t.Errorf("history entry %d has key %q for group %v", entry.ID, entry.GroupKey, entry.Group)
		}
	}
	if want := map[string]models.AlertStatus{"checkout": "resolved", "search": "triggered"}; len(history) != 2 || !reflect.DeepEqual(episodes, want) {
		t.Fatalf("history = %v over %d rows, want %v", episodes, len(history), want)
	}

	// A group that stops reporting compares 0 under the zero policy.
	if got, want := f.tick(t, groupedResult()), []string{"resolved search"}; !slices.Equal(got, want) {
		t.Fatalf("empty tick sent %v, want %v", got, want)
	}
	if alert, err = f.db.GetAlert(ctx, alert.ID); err != nil || alert.LastState != models.AlertStateResolved {
		t.Fatalf("last_state with no group firing = %v, %v; want resolved", alert.LastState, err)
	}
}

// TestGroupedEvaluationErrorKeepsState: a duplicate group is an evaluation
// error. It records an error row and changes no instance.
func TestGroupedEvaluationErrorKeepsState(t *testing.T) {
	f := newGroupedFixture(t)
	f.tick(t, groupedResult(row("checkout", 12)))
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.results = append(f.provider.results, groupedResult(row("checkout", 1), row("checkout", 2)))
	f.queue.take()
	if err := f.manager.evaluateAlert(t.Context(), alert, time.Now()); err == nil || !strings.Contains(err.Error(), "more than one row") {
		t.Fatalf("evaluate with a duplicate group err = %v, want the duplicate named", err)
	}
	f.sent = f.queue.take()
	if len(f.sent) != 0 {
		t.Errorf("a failed evaluation sent %d notifications", len(f.sent))
	}
	if got, want := f.instances(t), map[string]models.AlertInstanceState{"checkout": "firing"}; !reflect.DeepEqual(got, want) {
		t.Errorf("instances after a failed evaluation = %v, want %v", got, want)
	}
	history, err := f.db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 2 || history[0].Status != models.AlertStatusError {
		t.Fatalf("history = %+v, %v; want an error row above the open episode", history, err)
	}
}

// TestManualResolveOneGroup closes one group, notifies for it alone and
// leaves the other firing.
func TestManualResolveOneGroup(t *testing.T) {
	f := newGroupedFixture(t)
	f.tick(t, groupedResult(row("checkout", 12), row("search", 30)))
	f.queue.take()
	key := `["checkout"]`
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, &key, "fixed"); err != nil {
		t.Fatal(err)
	}
	f.sent = f.queue.take()
	if len(f.sent) != 1 || f.sent[0].Status != models.AlertStatusResolved || f.sent[0].Group["service"] != "checkout" || f.sent[0].Annotations["resolved_by"] != "manual" {
		t.Fatalf("manual resolve sent %+v, want one manual resolved for checkout", f.sent)
	}
	if got, want := f.instances(t), map[string]models.AlertInstanceState{"search": "firing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instances after resolving checkout = %v, want %v", got, want)
	}
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, &key, "again"); !models.IsNotFound(err) {
		t.Errorf("second resolve of checkout err = %v, want not found", err)
	}
	f.queue.take()
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, nil, "all"); err != nil {
		t.Fatal(err)
	}
	f.sent = f.queue.take()
	if len(f.sent) != 1 || f.sent[0].Group["service"] != "search" {
		t.Fatalf("resolve all sent %+v, want one resolved for search", f.sent)
	}
}

// TestManualResolveAllGroups closes every firing group and notifies each.
func TestManualResolveAllGroups(t *testing.T) {
	f := newGroupedFixture(t)
	f.tick(t, groupedResult(row("checkout", 12), row("search", 30)))
	f.queue.take()
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, nil, "all"); err != nil {
		t.Fatal(err)
	}
	f.sent = f.queue.take()
	var got []string
	for i := range f.sent {
		got = append(got, f.sent[i].Annotations["group"])
	}
	slices.Sort(got)
	if want := []string{"service=checkout", "service=search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve all notified groups %v, want %v", got, want)
	}
	if got := f.instances(t); len(got) != 0 {
		t.Fatalf("instances after resolve all = %v, want none", got)
	}
}

// TestGroupedWebhookPerGroup: the version 1 webhook gets one request per
// group, in the existing payload, with the group values among the labels.
func TestGroupedWebhookPerGroup(t *testing.T) {
	f := newGroupedFixture(t)
	var mu sync.Mutex
	var bodies []map[string]any
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	linkWebhooks(t, f.db, f.alert, receiver.URL)
	f.provider.results = append(f.provider.results, groupedResult(row("checkout", 12), row("search", 30)))
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.manager.evaluateAlert(t.Context(), alert, time.Now()); err != nil {
		t.Fatal(err)
	}
	drainQueue(t, f.db)

	var services []string
	for _, body := range bodies {
		labels, _ := body["labels"].(map[string]any)
		annotations, _ := body["annotations"].(map[string]any)
		services = append(services, labels["service"].(string))
		if annotations["group"] != "service="+labels["service"].(string) {
			t.Errorf("annotations.group = %v, want the group of labels %v", annotations["group"], labels)
		}
		if _, ok := body["group"]; ok {
			t.Errorf("version 1 payload gained a group key: %v", body)
		}
		if body["status"] != "triggered" || body["alert_name"] != "errors" {
			t.Errorf("payload = %v, want the existing triggered payload", body)
		}
	}
	slices.Sort(services)
	if want := []string{"checkout", "search"}; !slices.Equal(services, want) {
		t.Fatalf("webhook requests carried services %v, want one per group %v", services, want)
	}
}

// TestGroupedIncidentKeys: each group's notifications carry the group's
// incident key, logchef:alert:<id>: and 16 hex characters of the SHA-256 of
// the group key. A resolved shares its firing's key, so a PagerDuty resolve
// closes that incident, and a later episode of the group reuses it.
func TestGroupedIncidentKeys(t *testing.T) {
	f := newGroupedFixture(t)
	keys := func(result *models.QueryResult) map[string]string {
		t.Helper()
		alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.provider.results = append(f.provider.results, result)
		if err := f.manager.evaluateAlert(t.Context(), alert, time.Now()); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, row := range f.queue.rows() {
			var msg notify.Message
			if err := json.Unmarshal([]byte(row.MessageJSON), &msg); err != nil {
				t.Fatal(err)
			}
			out[string(msg.Event)+" "+msg.Group["service"]] = msg.DedupKey
		}
		return out
	}
	// printf '%s' '["checkout"]' | sha256sum, and the same for search.
	checkout := fmt.Sprintf("logchef:alert:%d:f9f77b923b27b024", f.alert.ID)
	search := fmt.Sprintf("logchef:alert:%d:f0d9e5f71a7c8bca", f.alert.ID)
	steps := []struct {
		result *models.QueryResult
		want   map[string]string
	}{
		{groupedResult(row("checkout", 12), row("search", 30)), map[string]string{"firing checkout": checkout, "firing search": search}},
		{groupedResult(row("checkout", 3), row("search", 30)), map[string]string{"resolved checkout": checkout}},
		{groupedResult(row("checkout", 20), row("search", 30)), map[string]string{"firing checkout": checkout}},
	}
	for i, step := range steps {
		if got := keys(step.result); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("tick %d enqueued %v, want %v", i+1, got, step.want)
		}
	}
}
