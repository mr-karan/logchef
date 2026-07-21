package ai

import (
	"context"
	"errors"
	"testing"
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

func TestGenerateSQLDelegatesToClickHouseTarget(t *testing.T) {
	p := &mockProvider{resps: []string{"```sql\nSELECT * FROM default.logs LIMIT 5\n```"}}
	g := newTestGenerator(p)
	out, err := g.GenerateSQL(context.Background(), "all logs", "[]", "default.logs", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty SQL from GenerateSQL wrapper")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls)
	}
}
