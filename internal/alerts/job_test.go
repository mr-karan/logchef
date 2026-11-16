package alerts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// replicaOpeners returns, per backend, a function that opens one database and
// then a new store on it at each call, like a separate replica. Postgres runs
// when LOGCHEF_TEST_POSTGRES_DSN is set.
func replicaOpeners() map[string]func(t *testing.T) func() store.Store {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return map[string]func(t *testing.T) func() store.Store{
		"sqlite": func(t *testing.T) func() store.Store {
			path := sqlitetest.Path(t)
			return func() store.Store {
				s, err := sqlite.New(context.Background(), sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: path}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			}
		},
		"postgres": func(t *testing.T) func() store.Store {
			dsn := postgrestest.DSN(t)
			return func() store.Store {
				s, err := postgres.New(context.Background(), postgres.Options{Logger: logger, Config: config.PostgresConfig{DSN: dsn}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			}
		},
	}
}

func upsertEvaluationJob(t *testing.T, s store.Store, alert *models.Alert) {
	t.Helper()
	if err := s.WithTx(t.Context(), func(tx store.StoreOps) error {
		return jobs.Upsert(t.Context(), tx, EvaluationJob(alert))
	}); err != nil {
		t.Fatal(err)
	}
}

func claimEvaluation(t *testing.T, s store.Store, owner string, lease time.Duration) jobs.Job {
	t.Helper()
	claimed, err := s.ClaimJobs(context.Background(), string(EvaluateKind), owner, lease, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim by %s = %v, %v; want one job", owner, claimed, err)
	}
	row := claimed[0]
	return jobs.Job{ID: row.ID, Kind: EvaluateKind, Key: row.Key, At: row.NextRunAt, ClaimedAt: *row.LastStartedAt, LeaseToken: row.LeaseToken}
}

// TestStalledEvaluationWritesNothingAfterTakeover: replica A claims the
// rule's tick and stalls in its query past the lease, replica B claims the
// tick, then A reaches its commit. A writes no instance, no history row and
// sends nothing, whether its query fired or failed. B's run commits.
func TestStalledEvaluationWritesNothingAfterTakeover(t *testing.T) {
	for name, opener := range replicaOpeners() {
		t.Run(name, func(t *testing.T) {
			open := opener(t)
			db, logger := open(), slog.New(slog.NewTextHandler(io.Discard, nil))
			alert := createManagerTestAlert(t, db)
			upsertEvaluationJob(t, db, alert)

			stale := claimEvaluation(t, db, "replica-a", 100*time.Millisecond)
			var current jobs.Job
			provider := &hookedProvider{duringQuery: func() {
				time.Sleep(300 * time.Millisecond)
				current = claimEvaluation(t, open(), "replica-b", time.Minute)
			}}
			sources := datasource.NewService(db, logger)
			sources.Register(provider)
			var sends atomic.Int32
			sender := alertSenderFunc(func(context.Context, AlertNotification) error { sends.Add(1); return nil })
			m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
			ctx := t.Context()

			if err := m.runEvaluation(ctx, stale); err != nil {
				t.Fatal(err)
			}
			provider.duringQuery, provider.err = nil, errors.New("query failed")
			if err := m.runEvaluation(ctx, stale); err == nil {
				t.Fatal("a failing query returned no error")
			}
			history, err := db.ListAlertHistory(ctx, alert.ID, 10)
			if err != nil || len(history) != 0 || sends.Load() != 0 {
				t.Fatalf("stale run wrote history %+v, sent %d, %v; want nothing", history, sends.Load(), err)
			}
			if instances, err := db.ListAlertInstances(ctx, alert.ID); err != nil || len(instances) != 0 {
				t.Fatalf("stale run wrote instances %+v, %v", instances, err)
			}
			if after, err := db.GetAlert(ctx, alert.ID); err != nil || after.LastEvaluatedAt != nil || after.EvalSeq != alert.EvalSeq {
				t.Fatalf("stale run changed the rule: %+v, %v", after, err)
			}

			provider.err = nil
			if err := m.runEvaluation(ctx, current); err != nil {
				t.Fatal(err)
			}
			history, err = db.ListAlertHistory(ctx, alert.ID, 10)
			if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusTriggered || sends.Load() != 1 {
				t.Fatalf("current run: history %+v, sent %d, %v; want one firing", history, sends.Load(), err)
			}
		})
	}
}

// countingProvider returns a value above the test rule's threshold and counts
// the queries.
type countingProvider struct {
	datasource.Provider
	queries *atomic.Int32
	hang    string // a query that blocks until its context ends
}

func (countingProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (p countingProvider) EvaluateAlert(ctx context.Context, _ *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
	p.queries.Add(1)
	if req.Query == p.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "value", Type: "UInt64"}},
		Logs:    []map[string]any{{"value": uint64(12)}},
	}, nil
}

// TestTwoReplicasEvaluateEachTickOnce runs two replicas, each with its own
// store, manager and job runner, over one database and one rule at the
// 10-second floor for one minute. The rule fires once: one history row and one
// notification, and each tick ran on one replica.
func TestTwoReplicasEvaluateEachTickOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("runs for one minute")
	}
	for name, opener := range replicaOpeners() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			open := opener(t)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			first := open()
			alert := createManagerTestAlert(t, first)
			alert.FrequencySeconds = MinFrequencySeconds
			if err := first.UpdateAlert(t.Context(), alert); err != nil {
				t.Fatal(err)
			}
			upsertEvaluationJob(t, first, alert)

			var queries, sends atomic.Int32
			sender := alertSenderFunc(func(context.Context, AlertNotification) error { sends.Add(1); return nil })
			var runners []*jobs.Runner
			for _, db := range []store.Store{first, open()} {
				sources := datasource.NewService(db, logger)
				sources.Register(countingProvider{queries: &queries})
				m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Sender: sender, Config: config.AlertsConfig{MaxConcurrentEvaluations: 4, HistoryLimit: 100}})
				runner := jobs.New(jobs.Options{Store: db, Logger: logger, PollInterval: 100 * time.Millisecond})
				runner.Register(m.EvaluationKind())
				runners = append(runners, runner)
			}
			for _, runner := range runners {
				runner.Start(t.Context())
			}
			time.Sleep(time.Minute)
			for _, runner := range runners {
				if err := runner.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			}

			history, err := first.ListAlertHistory(context.Background(), alert.ID, 100)
			if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusTriggered {
				t.Fatalf("history = %+v, %v; want one firing episode", history, err)
			}
			if sends.Load() != 1 {
				t.Errorf("notifications = %d, want 1", sends.Load())
			}
			// Ticks at 0, 10, ... 60 seconds: at most seven. A tick that ran on
			// both replicas would push the count past that. A starved runner
			// runs fewer, because a late claim runs only the latest due tick,
			// but a rule that never runs again after its first evaluation
			// stops at one.
			if n := queries.Load(); n < 2 || n > 7 {
				t.Errorf("evaluations = %d, want 2 to 7", n)
			}
		})
	}
}

// TestEvaluationTimeoutFreesTheRunnerSlot: a rule whose query never returns
// is cut at the kind's timeout and records an error, and the other rule still
// evaluates on the one slot.
func TestEvaluationTimeoutFreesTheRunnerSlot(t *testing.T) {
	db, hung, logger := newManagerTestAlert(t)
	hung.Query = "SELECT sleep(3600)"
	if err := db.UpdateAlert(t.Context(), hung); err != nil {
		t.Fatal(err)
	}
	other := *hung
	other.ID, other.Name, other.Query = 0, "other", "SELECT 12"
	if err := db.CreateAlert(t.Context(), &other); err != nil {
		t.Fatal(err)
	}
	upsertEvaluationJob(t, db, hung)
	upsertEvaluationJob(t, db, &other)

	var queries atomic.Int32
	sources := datasource.NewService(db, logger)
	sources.Register(countingProvider{queries: &queries, hang: hung.Query})
	m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Config: config.AlertsConfig{MaxConcurrentEvaluations: 1, HistoryLimit: 100}})
	kind := m.EvaluationKind()
	kind.Timeout = 200 * time.Millisecond
	runner := jobs.New(jobs.Options{Store: db, Logger: logger, PollInterval: 20 * time.Millisecond})
	runner.Register(kind)
	runner.Start(t.Context())
	defer func() { _ = runner.Stop(context.Background()) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		hungHistory, err1 := db.ListAlertHistory(t.Context(), hung.ID, 10)
		otherHistory, err2 := db.ListAlertHistory(t.Context(), other.ID, 10)
		if err := errors.Join(err1, err2); err != nil {
			t.Fatal(err)
		}
		if len(hungHistory) > 0 && len(otherHistory) > 0 {
			if hungHistory[0].Status != models.AlertStatusError || !strings.Contains(hungHistory[0].Message, "deadline exceeded") {
				t.Errorf("hung rule history = %+v, want a deadline error", hungHistory[0])
			}
			if otherHistory[0].Status != models.AlertStatusTriggered {
				t.Errorf("other rule history = %+v, want firing", otherHistory[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5s: hung rule history %d rows, other rule %d rows", len(hungHistory), len(otherHistory))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// windowProvider records the instant each evaluation stands for.
type windowProvider struct {
	datasource.Provider
	at chan time.Time
}

func (windowProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (p windowProvider) EvaluateAlert(_ context.Context, _ *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
	p.at <- req.At
	return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "value", Type: "UInt64"}}, Logs: []map[string]any{{"value": uint64(1)}}}, nil
}

// TestEvaluationWindowEndsAtTheDueOccurrence: the query window ends at the
// job's due occurrence, not at the claim or the wall clock, so a late claim
// does not shift it.
func TestEvaluationWindowEndsAtTheDueOccurrence(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	upsertEvaluationJob(t, db, alert)
	job := claimEvaluation(t, db, "replica-a", time.Minute)
	job.At = time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	provider := windowProvider{at: make(chan time.Time, 1)}
	sources := datasource.NewService(db, logger)
	sources.Register(provider)
	m := NewManager(Options{DB: db, Datasources: sources, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	if err := m.runEvaluation(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if got := <-provider.at; !got.Equal(job.At) {
		t.Errorf("evaluation window ends at %v, want the due occurrence %v", got, job.At)
	}
}
