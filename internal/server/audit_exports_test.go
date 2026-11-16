package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// exportAuditEnv is an audit env with a ClickHouse source that nothing
// listens for: the client connects lazily, so a query that starts fails,
// and a test can tell whether the export reached the query.
type exportAuditEnv struct {
	*auditRouteEnv
	streamPath, jobPath string
}

func newExportAuditEnv(t *testing.T) *exportAuditEnv {
	t.Helper()
	e := newAuditRouteEnv(t)
	e.s.config.Export = config.ExportConfig{
		MaxRows: 100, DefaultTimeoutSeconds: 5, MaxTimeoutSeconds: 10,
		ArtifactTTL: time.Hour, Formats: []string{"csv", "ndjson"},
	}
	e.s.clickhouse = clickhouse.NewManager(e.s.log)
	t.Cleanup(func() { _ = e.s.clickhouse.Close() })
	ctx := context.Background()
	source := &models.Source{Name: "export-audit", SourceType: models.SourceTypeClickHouse,
		Connection: models.ConnectionInfo{Host: "127.0.0.1:1", Database: "default", TableName: "logs"}}
	if err := e.db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := e.s.clickhouse.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	team := &models.Team{Name: "export-audit"}
	if err := e.db.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/v1/teams/%d/sources/%d", team.ID, source.ID)
	return &exportAuditEnv{auditRouteEnv: e, streamPath: base + "/logs/export", jobPath: base + "/exports"}
}

// jobSpyStore fails every audit insert, like auditDownStore, and records the
// ID of each export job created inside WithTx.
type jobSpyStore struct {
	auditDownStore
	mu  sync.Mutex
	ids []string
}

func (s *jobSpyStore) WithTx(ctx context.Context, fn func(store.StoreOps) error) error {
	return s.auditDownStore.WithTx(ctx, func(tx store.StoreOps) error { return fn(jobSpyOps{StoreOps: tx, spy: s}) })
}

type jobSpyOps struct {
	store.StoreOps
	spy *jobSpyStore
}

func (o jobSpyOps) CreateExportJob(ctx context.Context, job *models.ExportJob) error {
	o.spy.mu.Lock()
	o.spy.ids = append(o.spy.ids, job.ID)
	o.spy.mu.Unlock()
	return o.StoreOps.CreateExportJob(ctx, job)
}

// Exports fail closed: without a stored event no query runs and no job
// exists.
//
// Not parallel: the query tracker is process-wide and keyed by user ID, and
// every test database numbers its first user 1, so a parallel export would
// hold the slot this test counts.
func TestAuditExportsFailClosed(t *testing.T) {
	e := newExportAuditEnv(t)
	spy := &jobSpyStore{auditDownStore: auditDownStore{Store: e.db}}
	e.s.store = spy
	e.s.audit = audit.New(audit.Options{
		Store:     spy,
		Logger:    slog.New(slog.NewJSONHandler(e.logs, nil)),
		LogEvents: true,
	})
	// One slot per user: a slot that a refused export kept would turn the
	// second attempt into a 429.
	e.s.config.Export.MaxConcurrentPerUser = 1
	body := `{"raw_sql":"SELECT * FROM default.logs","format":"ndjson"}`

	for range 2 {
		resp := e.do(http.MethodPost, e.streamPath, body, e.cookie)
		wantStatus(t, resp, http.StatusServiceUnavailable)
		if strings.Contains(string(resp.body), `"type":"meta"`) || strings.Contains(string(resp.body), `"type":"error"`) {
			t.Errorf("the stream started without a stored event: %s", resp.body)
		}
	}
	if strings.Contains(e.logs.String(), "failed to stream export") {
		t.Error("the export query ran without a stored event")
	}

	for range 2 {
		wantStatus(t, e.do(http.MethodPost, e.jobPath, body, e.cookie), http.StatusServiceUnavailable)
	}
	spy.mu.Lock()
	ids := append([]string(nil), spy.ids...)
	spy.mu.Unlock()
	if len(ids) != 2 {
		t.Fatalf("CreateExportJob ran %d times, want 2", len(ids))
	}
	for _, id := range ids {
		if _, err := e.db.GetExportJob(context.Background(), id); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("export job %s exists after the audit insert failed: %v", id, err)
		}
	}
	if strings.Contains(e.logs.String(), `"stored":true`) {
		t.Errorf("a log line says stored true: %s", e.logs)
	}
}

// An export records the query as a digest: a literal inside it reaches
// neither the row nor the log line.
func TestAuditExportRecordsQueryDigest(t *testing.T) {
	t.Parallel()
	e := newExportAuditEnv(t)
	e.s.audit = audit.New(audit.Options{
		Store:     strictAuditStore{AuditStore: e.db, t: t},
		Logger:    slog.New(slog.NewJSONHandler(e.logs, nil)),
		LogEvents: true,
	})
	const sentinel = "SENTINEL-session-7f3a"
	query := `SELECT * FROM default.logs WHERE session_id = '` + sentinel + `'`
	sum := sha256.Sum256([]byte(query))
	body, err := json.Marshal(map[string]any{"raw_sql": query, "format": "ndjson", "limit": 500})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path, mode, targetType string
		status                 int
	}{
		{e.streamPath, "stream", "source", http.StatusOK},
		{e.jobPath, "job", "log_export", http.StatusAccepted},
	} {
		e.reset()
		wantStatus(t, e.do(http.MethodPost, tc.path, string(body), e.cookie), tc.status)
		ev := e.only()
		checkActorAndTarget(t, ev, string(audit.LogExportCreate), e.admin, tc.targetType, ev.TargetID)
		if ev.Status != tc.status || detailOf(t, ev)["mode"] != tc.mode || ev.SourceID == nil || ev.TeamID == nil {
			t.Errorf("%s: event status %d detail %s source %v team %v", tc.mode, ev.Status, ev.Detail, ev.SourceID, ev.TeamID)
		}
		c := changesOf(t, ev)
		digest, _ := c["query"]["to"].(map[string]any)
		if c["language"]["to"] != string(models.QueryLanguageClickHouseSQL) || c["format"]["to"] != "ndjson" || c["row_limit"]["to"] != float64(100) ||
			digest["sha256"] != hex.EncodeToString(sum[:]) || digest["bytes"] != float64(len(query)) {
			t.Errorf("%s: changes %s", tc.mode, ev.Changes)
		}
		if strings.Contains(string(ev.Changes)+string(ev.Detail)+auditLogLines(e.logs.String()), sentinel) {
			t.Errorf("%s: the query literal reached the event or its log line", tc.mode)
		}
	}
}

// auditLogLines keeps the "audit" lines of a JSON log. The ClickHouse client
// logs a failed query with its text, which is query logging, not the audit
// event.
func auditLogLines(logs string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, `"msg":"audit"`) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
