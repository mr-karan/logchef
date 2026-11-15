package alerts

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

type hookedProvider struct {
	datasource.Provider
	duringQuery func()
	err         error
}

func (*hookedProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (p *hookedProvider) EvaluateAlert(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
	if p.duringQuery != nil {
		p.duringQuery()
	}
	if p.err != nil {
		return nil, p.err
	}
	return &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "value", Type: "UInt64"}},
		Logs:    []map[string]any{{"value": uint64(12)}},
	}, nil
}

// TestEditDuringEvaluationDiscardsResult: an edit advances eval_seq, so an
// evaluation of the rule as loaded before the edit writes no history, no
// instance and sends nothing. A freshly loaded rule fires normally.
func TestEditDuringEvaluationDiscardsResult(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	ctx := t.Context()
	provider := &hookedProvider{duringQuery: func() {
		if err := db.UpdateAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
	}}
	sources := datasource.NewService(db, logger)
	sources.Register(provider)
	sends := 0
	sender := alertSenderFunc(func(context.Context, AlertNotification) error { sends++; return nil })
	m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})

	if err := m.evaluateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	history, err := db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil || len(history) != 0 || sends != 0 {
		t.Fatalf("after a concurrent edit: history %d, sends %d, %v; want none", len(history), sends, err)
	}
	if instances, err := db.ListAlertInstances(ctx, alert.ID); err != nil || len(instances) != 0 {
		t.Fatalf("instances after a concurrent edit = %+v, %v; want none", instances, err)
	}

	// A rule loaded before an edit is stale even if the edit lands before its
	// evaluation starts.
	provider.duringQuery = nil
	if err := db.UpdateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	if err := m.evaluateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	if history, err := db.ListAlertHistory(ctx, alert.ID, 10); err != nil || len(history) != 0 || sends != 0 {
		t.Fatalf("after an edit before evaluation: history %d, sends %d, %v; want none", len(history), sends, err)
	}

	fresh, err := db.GetAlert(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.evaluateAlert(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	history, err = db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil || len(history) != 1 || sends != 1 {
		t.Fatalf("next tick: history %d, sends %d, %v; want one firing", len(history), sends, err)
	}
	instances, err := db.ListAlertInstances(ctx, alert.ID)
	if err != nil || len(instances) != 1 || instances[0].HistoryID == nil || *instances[0].HistoryID != history[0].ID || !instances[0].Notified || instances[0].LastSeenAt == nil {
		t.Fatalf("instance after firing = %+v, %v; want notified instance of history %d", instances, err, history[0].ID)
	}
}

// TestFiringEpisodeTracksValue: a firing tick after a delivered notification
// updates the instance's value and sends nothing; a resolve records its value.
func TestFiringEpisodeTracksValue(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	ctx := t.Context()
	var statuses []models.AlertStatus
	sender := alertSenderFunc(func(_ context.Context, n AlertNotification) error {
		statuses = append(statuses, n.Status)
		return nil
	})
	m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
	for _, value := range []float64{12, 15} {
		if err := m.handleTriggered(ctx, alert, value); err != nil {
			t.Fatal(err)
		}
	}
	instances, err := db.ListAlertInstances(ctx, alert.ID)
	if err != nil || len(instances) != 1 || instances[0].Value == nil || *instances[0].Value != 15 {
		t.Fatalf("instances = %+v, %v; want one instance with value 15", instances, err)
	}
	if err := m.handleResolved(ctx, alert, 3); err != nil {
		t.Fatal(err)
	}
	history, err := db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Message != "alert errors resolved with value 3.0000" {
		t.Fatalf("history = %+v, %v; want one episode resolved with value 3", history, err)
	}
	if want := []models.AlertStatus{models.AlertStatusTriggered, models.AlertStatusResolved}; !slices.Equal(statuses, want) {
		t.Errorf("notifications = %v, want %v", statuses, want)
	}
	if instances, err := db.ListAlertInstances(ctx, alert.ID); err != nil || len(instances) != 0 {
		t.Errorf("instances after resolve = %+v, %v; want none", instances, err)
	}
}

// TestEditDuringFailedEvaluationRecordsNothing: a query that fails after the
// rule was edited writes no error row and leaves last_evaluated_at alone, so
// the edited rule is due at the next cycle.
func TestEditDuringFailedEvaluationRecordsNothing(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	ctx := t.Context()
	provider := &hookedProvider{err: errors.New("query failed"), duringQuery: func() {
		if err := db.UpdateAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
	}}
	sources := datasource.NewService(db, logger)
	sources.Register(provider)
	m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	if err := m.evaluateAlert(ctx, alert); err == nil {
		t.Fatal("evaluation with a failing query returned no error")
	}
	history, err := db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("history after a failed stale evaluation = %+v, %v; want none", history, err)
	}
	after, err := db.GetAlert(ctx, alert.ID)
	if err != nil || after.LastEvaluatedAt != nil {
		t.Fatalf("last_evaluated_at after a failed stale evaluation = %v, %v; want unset", after.LastEvaluatedAt, err)
	}
}
