package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// exportAuditEnv is an audit env with a ClickHouse source at a fixed
// address. newExportAuditEnv points it at an address that nothing listens
// on: the client connects lazily, so a query that starts fails, and a test
// can tell whether the export reached the query.
type exportAuditEnv struct {
	*auditRouteEnv
	streamPath, jobPath string
	sourceID            models.SourceID
}

func newExportAuditEnv(t *testing.T, options ...auditEnvOption) *exportAuditEnv {
	t.Helper()
	return newExportAuditEnvAt(t, "127.0.0.1:1", options...)
}

// newExportAuditEnvAt adds the export config, the source at host and its
// ClickHouse client before the server starts; options run after, so they
// can change the export config.
func newExportAuditEnvAt(t *testing.T, host string, options ...auditEnvOption) *exportAuditEnv {
	t.Helper()
	var base string
	var sourceID models.SourceID
	withSource := func(s *auditEnvSetup) {
		s.opts.Config.Export = config.ExportConfig{
			MaxRows: 100, DefaultTimeoutSeconds: 5, MaxTimeoutSeconds: 10,
			ArtifactTTL: time.Hour, Formats: []string{"csv", "ndjson"},
		}
		manager := clickhouse.NewManager(s.opts.Logger)
		t.Cleanup(func() { _ = manager.Close() })
		ctx := context.Background()
		source := &models.Source{Name: "export-audit", SourceType: models.SourceTypeClickHouse,
			Connection: models.ConnectionInfo{Host: host, Database: "default", TableName: "logs"}}
		if err := s.db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if err := manager.AddSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		team := &models.Team{Name: "export-audit"}
		if err := s.db.CreateTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
		if err := s.db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatal(err)
		}
		s.opts.ClickHouse = manager
		sourceID = source.ID
		base = fmt.Sprintf("/api/v1/teams/%d/sources/%d", team.ID, source.ID)
	}
	e := newAuditRouteEnv(t, append([]auditEnvOption{withSource}, options...)...)
	return &exportAuditEnv{auditRouteEnv: e, streamPath: base + "/logs/export", jobPath: base + "/exports", sourceID: sourceID}
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
	var spy *jobSpyStore
	e := newExportAuditEnv(t,
		withAuditStore(func(db *sqlite.DB) store.Store {
			spy = &jobSpyStore{auditDownStore: auditDownStore{Store: db}}
			return spy
		}),
		withAuditLogLines(),
		// One slot per user: a slot that a refused export kept would turn
		// the second attempt into a 429.
		withConfig(func(cfg *config.Config) { cfg.Export.MaxConcurrentPerUser = 1 }),
	)
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

// An export records its full query and time range on create, and how it
// ended on complete, under the same target.
func TestAuditExportRecordsQueryAndCompletion(t *testing.T) {
	t.Parallel()
	e := newExportAuditEnv(t, withAuditLogLines())
	query := `SELECT * FROM default.logs WHERE session_id = 'session-7f3a'`
	body, err := json.Marshal(map[string]any{"raw_sql": query, "format": "ndjson", "limit": 500,
		"start_time": "2026-09-01T00:00:00Z", "end_time": "2026-09-01T01:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path, mode string
		status     int
	}{
		{e.streamPath, "stream", http.StatusOK},
		{e.jobPath, "job", http.StatusAccepted},
	} {
		e.reset()
		wantStatus(t, e.do(http.MethodPost, tc.path, string(body), e.cookie), tc.status)
		// The ClickHouse client finds no server, so the export fails after
		// its create event; a job fails in the background.
		events := e.waitForEvents(2)
		create, complete := events[0], events[1]
		checkActorAndTarget(t, create, string(audit.LogExportCreate), e.admin, "log_export", create.TargetID)
		if create.TargetLabel != "export-audit" || complete.TargetLabel != "export-audit" {
			t.Errorf("%s: target labels %q and %q, want the source name", tc.mode, create.TargetLabel, complete.TargetLabel)
		}
		if create.TargetID == "" || create.Status != tc.status || detailOf(t, create)["mode"] != tc.mode || create.SourceID == nil || create.TeamID == nil {
			t.Errorf("%s: create target %q status %d detail %s", tc.mode, create.TargetID, create.Status, create.Detail)
		}
		c := changesOf(t, create)
		if c["query"]["to"] != query || c["language"]["to"] != string(models.QueryLanguageClickHouseSQL) || c["format"]["to"] != "ndjson" ||
			c["row_limit"]["to"] != float64(100) || c["time_from"]["to"] != "2026-09-01T00:00:00Z" || c["time_to"]["to"] != "2026-09-01T01:00:00Z" {
			t.Errorf("%s: changes %s", tc.mode, create.Changes)
		}
		if !strings.Contains(auditLogLines(e.logs.String()), "session-7f3a") {
			t.Errorf("%s: the audit log line does not carry the query", tc.mode)
		}
		if complete.Action != string(audit.LogExportComplete) || complete.TargetType != "log_export" || complete.TargetID != create.TargetID ||
			complete.ActorID == nil || *complete.ActorID != e.admin.ID || complete.Outcome != models.AuditOutcomeFailure || complete.Reason == "" {
			t.Errorf("%s: complete event %+v", tc.mode, complete)
		}
		if d := detailOf(t, complete); d["rows_exported"] != float64(0) || d["bytes_written"] == nil || d["duration_ms"] == nil {
			t.Errorf("%s: complete detail %s", tc.mode, complete.Detail)
		}
	}
}

// A completed export records its rows and bytes. It needs a ClickHouse
// server.
func TestAuditExportCompleteCountsRows(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	e := newExportAuditEnvAt(t, addr)
	body := `{"raw_sql":"SELECT number FROM numbers(5)","format":"ndjson"}`
	for _, tc := range []struct {
		path   string
		status int
	}{{e.streamPath, http.StatusOK}, {e.jobPath, http.StatusAccepted}} {
		e.reset()
		resp := e.do(http.MethodPost, tc.path, body, e.cookie)
		wantStatus(t, resp, tc.status)
		complete := e.waitForEvents(2)[1]
		d := detailOf(t, complete)
		if complete.Action != string(audit.LogExportComplete) || complete.Outcome != models.AuditOutcomeSuccess || d["rows_exported"] != float64(5) {
			t.Errorf("%s: complete %s %s detail %s", tc.path, complete.Action, complete.Outcome, complete.Detail)
		}
		bytes, _ := d["bytes_written"].(float64)
		if tc.status == http.StatusOK && int(bytes) != len(resp.body) {
			t.Errorf("stream: bytes_written %v, the client read %d", d["bytes_written"], len(resp.body))
		}
		if bytes <= 0 {
			t.Errorf("%s: bytes_written %v", tc.path, d["bytes_written"])
		}
	}
}

// A download records the artifact it served, once: a response that holds the
// file's first byte records it, and a Range request that resumes the
// download records nothing.
func TestAuditExportDownloadRecordsOncePerDownload(t *testing.T) {
	t.Parallel()
	e := newExportAuditEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "export.csv")
	content := "number\n" + strings.Repeat("0123456789\n", 20)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := &models.ExportJob{ID: uuid.NewString(), SourceID: e.sourceID, CreatedBy: e.admin.ID, Status: models.ExportJobStatusPending,
		Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := e.db.CreateExportJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := e.db.CompleteExportJob(ctx, job.ID, "logchef-export.csv", path, 20, int64(len(content)), now); err != nil {
		t.Fatal(err)
	}
	download := e.jobPath + "/" + job.ID + "/download"

	get := func(rangeHeader string, wantStatus int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, download, http.NoBody)
		req.AddCookie(e.cookie)
		if rangeHeader != "" {
			req.Header.Set(fiber.HeaderRange, rangeHeader)
		}
		resp, err := e.s.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("GET with Range %q: status %d, want %d", rangeHeader, resp.StatusCode, wantStatus)
		}
	}

	get("", http.StatusOK)
	ev := e.only()
	checkActorAndTarget(t, ev, string(audit.LogExportDownload), e.admin, "log_export", job.ID)
	if ev.TargetLabel != "export-audit" {
		t.Errorf("download target label %q, want the source name", ev.TargetLabel)
	}
	d := detailOf(t, ev)
	if d["format"] != "csv" || d["file_name"] != "logchef-export.csv" || d["rows_exported"] != float64(20) ||
		d["bytes_written"] != float64(len(content)) || d["created_by"] != float64(e.admin.ID) {
		t.Errorf("download detail %s", ev.Detail)
	}

	for _, tc := range []struct {
		rangeHeader string
		events      int
	}{
		{"bytes=100-", 0},
		{"bytes=-10", 0},
		{"bytes=0-0", 1},
		{"bytes=0-3", 1},
		// A suffix range longer than the file serves all of it.
		{"bytes=-999999999", 1},
	} {
		e.reset()
		get(tc.rangeHeader, http.StatusPartialContent)
		if got := e.events(); len(got) != tc.events {
			t.Errorf("Range %q recorded %d events, want %d", tc.rangeHeader, len(got), tc.events)
		}
	}
}

// waitForEvents waits for n stored events, for an event that a background
// export records after the request returned.
func (e *auditRouteEnv) waitForEvents(n int) []models.AuditEvent {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := e.events()
		if len(got) >= n {
			if len(got) > n {
				e.t.Fatalf("stored %d events, want %d: %+v", len(got), n, got)
			}
			return got
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("stored %d events after 10s, want %d: %+v", len(got), n, got)
		}
		time.Sleep(20 * time.Millisecond)
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
