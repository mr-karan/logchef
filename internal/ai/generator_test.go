package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockProvider is a scripted Provider: it returns the next response from resps
// on each Complete call and records how many calls were made.
type mockProvider struct {
	resps []string
	err   error
	calls int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Complete(_ context.Context, _ CompletionRequest) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	idx := m.calls
	m.calls++
	if idx < len(m.resps) {
		return m.resps[idx], nil
	}
	// Default to the last response if the generator asks for more than scripted.
	if len(m.resps) > 0 {
		return m.resps[len(m.resps)-1], nil
	}
	return "", nil
}

func newTestGenerator(p Provider) *Generator {
	return NewGenerator(p, GeneratorConfig{}, testLogger())
}

// deadlineRecorder records the context deadline seen on each Complete call.
type deadlineRecorder struct {
	resps     []string
	calls     int
	deadlines []time.Time
}

func (d *deadlineRecorder) Name() string { return "deadline-recorder" }

func (d *deadlineRecorder) Complete(ctx context.Context, _ CompletionRequest) (string, error) {
	dl, _ := ctx.Deadline()
	d.deadlines = append(d.deadlines, dl)
	idx := d.calls
	d.calls++
	if idx < len(d.resps) {
		return d.resps[idx], nil
	}
	return d.resps[len(d.resps)-1], nil
}

// TestGenerateQueryPerAttemptTimeout guards the fix for the shared-context bug:
// each attempt (first + repair) must get its OWN timeout window, so a slow first
// attempt can't starve the repair. With one shared callCtx both calls saw an
// identical deadline; with a fresh window per call the repair's deadline is
// strictly later (computed at a later instant).
func TestGenerateQueryPerAttemptTimeout(t *testing.T) {
	// First response is invalid ClickHouse SQL (triggers a repair), second valid.
	p := &deadlineRecorder{resps: []string{"not valid sql ((", "SELECT * FROM default.logs LIMIT 10"}}
	g := newTestGenerator(p)
	if _, err := g.GenerateQuery(context.Background(), GenerateQueryInput{Target: TargetClickHouseSQL, NaturalLanguageQuery: "all logs"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.deadlines) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(p.deadlines))
	}
	if !p.deadlines[1].After(p.deadlines[0]) {
		t.Fatalf("repair call must get a fresh timeout window (deadline later than the first call); got first=%v repair=%v", p.deadlines[0], p.deadlines[1])
	}
}

func TestGenerateQueryRepairLoop(t *testing.T) {
	ctx := context.Background()

	t.Run("clickhouse valid on first attempt (no repair)", func(t *testing.T) {
		p := &mockProvider{resps: []string{"SELECT * FROM default.logs LIMIT 10"}}
		g := newTestGenerator(p)
		out, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetClickHouseSQL, NaturalLanguageQuery: "all logs"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out == "" {
			t.Fatal("expected non-empty query")
		}
		if p.calls != 1 {
			t.Fatalf("expected exactly 1 provider call, got %d", p.calls)
		}
	})

	t.Run("clickhouse invalid then repaired ok (2 calls)", func(t *testing.T) {
		p := &mockProvider{resps: []string{"this is not sql", "SELECT * FROM default.logs LIMIT 10"}}
		g := newTestGenerator(p)
		out, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetClickHouseSQL, NaturalLanguageQuery: "all logs"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out == "" {
			t.Fatal("expected non-empty repaired query")
		}
		if p.calls != 2 {
			t.Fatalf("expected exactly 2 provider calls (1 repair), got %d", p.calls)
		}
	})

	t.Run("clickhouse still invalid after repair -> error, capped at 2 calls", func(t *testing.T) {
		p := &mockProvider{resps: []string{"nope not sql", "still not sql at all"}}
		g := newTestGenerator(p)
		_, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetClickHouseSQL, NaturalLanguageQuery: "all logs"})
		if !errors.Is(err, ErrInvalidSQLGeneratedByAI) {
			t.Fatalf("expected ErrInvalidSQLGeneratedByAI, got %v", err)
		}
		if p.calls != 2 {
			t.Fatalf("expected exactly 2 provider calls (repair cap), got %d", p.calls)
		}
	})

	t.Run("logchefql valid on first attempt", func(t *testing.T) {
		p := &mockProvider{resps: []string{`level="error" and service="api"`}}
		g := newTestGenerator(p)
		out, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetLogchefQL, NaturalLanguageQuery: "errors"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `level="error" and service="api"` {
			t.Fatalf("unexpected output: %q", out)
		}
		if p.calls != 1 {
			t.Fatalf("expected 1 provider call, got %d", p.calls)
		}
	})

	t.Run("logchefql invalid then repaired ok", func(t *testing.T) {
		p := &mockProvider{resps: []string{`level=="error" and and`, `level="error"`}}
		g := newTestGenerator(p)
		out, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetLogchefQL, NaturalLanguageQuery: "errors"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `level="error"` {
			t.Fatalf("unexpected output: %q", out)
		}
		if p.calls != 2 {
			t.Fatalf("expected 2 provider calls, got %d", p.calls)
		}
	})

	t.Run("logsql not repairable: empty -> error, only 1 call", func(t *testing.T) {
		p := &mockProvider{resps: []string{"   "}}
		g := newTestGenerator(p)
		_, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetLogsQL, NaturalLanguageQuery: "errors"})
		if !errors.Is(err, ErrInvalidSQLGeneratedByAI) {
			t.Fatalf("expected ErrInvalidSQLGeneratedByAI, got %v", err)
		}
		if p.calls != 1 {
			t.Fatalf("expected exactly 1 provider call (LogsQL not repairable), got %d", p.calls)
		}
	})

	t.Run("logsql best-effort accepts non-empty on first attempt", func(t *testing.T) {
		p := &mockProvider{resps: []string{`service:="api" | stats count() as value`}}
		g := newTestGenerator(p)
		out, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetLogsQL, NaturalLanguageQuery: "count api"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `service:="api" | stats count() as value` {
			t.Fatalf("unexpected output: %q", out)
		}
		if p.calls != 1 {
			t.Fatalf("expected 1 provider call, got %d", p.calls)
		}
	})

	t.Run("provider transport error is surfaced (not repaired)", func(t *testing.T) {
		p := &mockProvider{err: errors.New("network down")}
		g := newTestGenerator(p)
		_, err := g.GenerateQuery(ctx, GenerateQueryInput{Target: TargetClickHouseSQL, NaturalLanguageQuery: "all logs"})
		if err == nil {
			t.Fatal("expected transport error")
		}
		if errors.Is(err, ErrInvalidSQLGeneratedByAI) {
			t.Fatalf("transport error should not be ErrInvalidSQLGeneratedByAI, got %v", err)
		}
	})
}

// requestRecorder returns resp and records every request it receives.
type requestRecorder struct {
	resp string
	reqs []CompletionRequest
}

func (r *requestRecorder) Name() string { return "request-recorder" }

func (r *requestRecorder) Complete(_ context.Context, req CompletionRequest) (string, error) {
	r.reqs = append(r.reqs, req)
	return r.resp, nil
}

func TestGenerateQueryLogQL(t *testing.T) {
	schema := `[{"name":"_time","type":"DateTime"},{"name":"_msg","type":"String"},{"name":"service_name","type":"label"},{"name":"status","type":"int"}]`

	t.Run("the prompt states the selector rule, the field classes and the parser", func(t *testing.T) {
		p := &requestRecorder{resp: "```logql\n{service_name=~\".+\"} | json | status>=500\n```"}
		out, err := newTestGenerator(p).GenerateQuery(context.Background(), GenerateQueryInput{
			Target: TargetLogQL, NaturalLanguageQuery: "server errors", Schema: schema,
			StreamSelector: `{service_name=~".+"}`, Parser: "json",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out != `{service_name=~".+"} | json | status>=500` {
			t.Fatalf("output %q", out)
		}
		if len(p.reqs) != 1 {
			t.Fatalf("%d provider calls", len(p.reqs))
		}
		system := p.reqs[0].System
		for _, want := range []string{
			`Start from this source's default stream selector: {service_name=~".+"}`,
			"Keep all of its matchers",
			"at least one matcher that does not match the empty string",
			`Stream labels have type "label". Only stream labels may appear inside the selector braces.`,
			"_msg is the log line. Filter it with a line filter",
			"Every other field is structured metadata or a field that the parser extracts",
			"This source parses lines with | json. Put | json after the line filters",
			"Do NOT add a time range",
			"Provided schema:\n" + schema,
		} {
			if !strings.Contains(system, want) {
				t.Errorf("system prompt lacks %q:\n%s", want, system)
			}
		}
		if !strings.HasPrefix(p.reqs[0].User, "Generate a LogQL query to answer this question: server errors.") {
			t.Errorf("user prompt %q", p.reqs[0].User)
		}
	})

	t.Run("a source without a parser gets no parser stage", func(t *testing.T) {
		p := &requestRecorder{resp: `{service_name="api"} | status>=500`}
		if _, err := newTestGenerator(p).GenerateQuery(context.Background(), GenerateQueryInput{
			Target: TargetLogQL, NaturalLanguageQuery: "server errors", Schema: schema, StreamSelector: `{service_name="api"}`,
		}); err != nil {
			t.Fatal(err)
		}
		system := p.reqs[0].System
		if !strings.Contains(system, "This source has no parser. Do not add a parser stage") || strings.Contains(system, "parses lines with") {
			t.Errorf("system prompt:\n%s", system)
		}
	})

	t.Run("an empty answer is invalid and not repaired", func(t *testing.T) {
		p := &requestRecorder{resp: "  "}
		_, err := newTestGenerator(p).GenerateQuery(context.Background(), GenerateQueryInput{Target: TargetLogQL, NaturalLanguageQuery: "errors", StreamSelector: `{a="b"}`})
		if !errors.Is(err, ErrInvalidSQLGeneratedByAI) || len(p.reqs) != 1 {
			t.Fatalf("err %v after %d calls", err, len(p.reqs))
		}
	})
}
