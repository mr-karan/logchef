package audit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// memStore is an AuditStore that keeps inserted events, or fails every insert.
type memStore struct {
	events []models.AuditEvent
	fail   error
	// deadlines holds how long each insert had before its context expired.
	deadlines []time.Duration
}

func (m *memStore) InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error {
	if d, ok := ctx.Deadline(); ok {
		m.deadlines = append(m.deadlines, time.Until(d))
	}
	if m.fail != nil {
		return m.fail
	}
	if err := e.Validate(); err != nil {
		return err
	}
	m.events = append(m.events, *e)
	return nil
}

func (m *memStore) ListAuditEvents(context.Context, models.AuditFilter) ([]models.AuditEvent, error) {
	return m.events, nil
}

func (m *memStore) CountAuditEvents(context.Context, models.AuditFilter, int) (int, error) {
	return len(m.events), nil
}

func (m *memStore) PruneAuditEvents(context.Context, time.Time, int, int) (int, error) {
	return 0, nil
}

// harness is a Recorder whose log lines land in a buffer.
type harness struct {
	rec   *audit.Recorder
	store *memStore
	logs  *bytes.Buffer
}

func newHarness(st store.AuditStore, opts ...func(*audit.Options)) (*audit.Recorder, *bytes.Buffer) {
	var buf bytes.Buffer
	o := audit.Options{Store: st, Logger: slog.New(slog.NewJSONHandler(&buf, nil)), LogEvents: true}
	for _, opt := range opts {
		opt(&o)
	}
	return audit.New(o), &buf
}

func newMemHarness() harness {
	st := &memStore{}
	rec, buf := newHarness(st)
	return harness{rec: rec, store: st, logs: buf}
}

// auditLines returns the "audit" log lines, decoded.
func auditLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for raw := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		if line["msg"] == "audit" {
			lines = append(lines, line)
		}
	}
	return lines
}

func event(action audit.Action) audit.Event {
	return audit.Event{
		Action:  action,
		Outcome: models.AuditOutcomeSuccess,
		Actor:   audit.Actor{Type: models.AuditActorUser, ID: new(models.UserID(7)), Label: "admin@test.dev", AuthMethod: models.AuditAuthSession},
		Target:  audit.Target{Type: "source", ID: "3", Label: "logs.app", SourceID: new(models.SourceID(3))},
		Request: audit.Request{IP: "10.0.0.1", UserAgent: "test", Method: "PUT", Route: "/api/v1/admin/sources/:sourceID", Status: 200},
	}
}

func decode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestActionRegistry(t *testing.T) {
	spec, ok := audit.Lookup("team_member.add")
	if !ok || spec.Action != audit.TeamMemberAdd || spec.TargetType != "team_member" || !spec.Staged {
		t.Errorf("Lookup(team_member.add) = %+v, %v", spec, ok)
	}
	for _, name := range []string{"", "source", "source.frobnicate", "Source.update", "source.update.x", "source.update "} {
		if _, ok := audit.Lookup(name); ok {
			t.Errorf("Lookup(%q) accepted an unregistered name", name)
		}
	}
}

func TestRecordDiffKeepsOnlyChangedKeys(t *testing.T) {
	h := newMemHarness()
	e := event(audit.SourceUpdate)
	e.Before = audit.NewFields().Set("name", "a").Set("ttl_days", 30).Set("tags", []string{"x"}).Set("removed", true)
	e.After = audit.NewFields().Set("name", "b").Set("ttl_days", 30).Set("tags", []string{"x"}).Set("added", models.SourceType("victorialogs"))
	h.rec.Record(context.Background(), e)

	if len(h.store.events) != 1 {
		t.Fatalf("stored %d events, want 1", len(h.store.events))
	}
	got := decode(t, h.store.events[0].Changes)
	want := map[string]any{
		"name":    map[string]any{"from": "a", "to": "b"},
		"removed": map[string]any{"from": true},
		"added":   map[string]any{"to": "victorialogs"},
	}
	if mustJSON(t, got) != mustJSON(t, want) {
		t.Errorf("changes = %s, want %s", mustJSON(t, got), mustJSON(t, want))
	}

	create := event(audit.SourceCreate)
	create.After = audit.NewFields().Set("name", "n").Set("ttl_days", 7)
	h.rec.Record(context.Background(), create)
	if got := decode(t, h.store.events[1].Changes); mustJSON(t, got) != `{"name":{"to":"n"},"ttl_days":{"to":7}}` {
		t.Errorf("create changes = %s", mustJSON(t, got))
	}

	del := event(audit.SourceDelete)
	del.Before = audit.NewFields().Set("name", "n")
	h.rec.Record(context.Background(), del)
	if got := decode(t, h.store.events[2].Changes); mustJSON(t, got) != `{"name":{"from":"n"}}` {
		t.Errorf("delete changes = %s", mustJSON(t, got))
	}
}

func TestSecretSerializesNoValue(t *testing.T) {
	h := newMemHarness()
	e := event(audit.SourceUpdate)
	e.Before = audit.NewFields().Secret("connection.password", "OLD-SENTINEL").Secret("connection.auth.token", "SAME-SENTINEL").Secret("unset", "")
	e.After = audit.NewFields().Secret("connection.password", "NEW-SENTINEL").Secret("connection.auth.token", "SAME-SENTINEL")
	e.Detail = audit.NewFields().Secret("webhook_url", "DETAIL-SENTINEL")
	h.rec.Record(context.Background(), e)

	ev := h.store.events[0]
	if got := mustJSON(t, decode(t, ev.Changes)); got != `{"connection.password":{"changed":true,"secret":true}}` {
		t.Errorf("changes = %s, want only the changed secret", got)
	}
	if got := mustJSON(t, decode(t, ev.Detail)); got != `{"webhook_url":{"secret":true}}` {
		t.Errorf("detail = %s", got)
	}
	everything := string(ev.Changes) + string(ev.Detail) + h.logs.String()
	for _, sentinel := range []string{"OLD-SENTINEL", "NEW-SENTINEL", "SAME-SENTINEL", "DETAIL-SENTINEL"} {
		if strings.Contains(everything, sentinel) {
			t.Errorf("%s reached the event or the log", sentinel)
		}
	}

	set := event(audit.SourceCreate)
	set.After = audit.NewFields().Secret("connection.password", "x").Secret("connection.auth.token", "")
	h.rec.Record(context.Background(), set)
	if got := mustJSON(t, decode(t, h.store.events[1].Changes)); got != `{"connection.password":{"changed":true,"secret":true}}` {
		t.Errorf("create with one set secret = %s", got)
	}
}

func TestDeniedKeysAreRedacted(t *testing.T) {
	h := newMemHarness()
	e := event(audit.SettingUpdate)
	e.Before = audit.NewFields().Set("smtp_password", "P1-SENTINEL").Set("X-Authorization", "A1-SENTINEL").Set("name", "kept")
	e.After = audit.NewFields().Set("smtp_password", "P2-SENTINEL").Set("X-Authorization", "A2-SENTINEL").Set("name", "kept2")
	e.Detail = audit.NewFields().Set("session_cookie", "C-SENTINEL").Set("dsn", "D-SENTINEL")
	h.rec.Record(context.Background(), e)

	ev := h.store.events[0]
	changes, detail := decode(t, ev.Changes), decode(t, ev.Detail)
	want := map[string]any{"from": "[redacted]", "to": "[redacted]"}
	for _, key := range []string{"smtp_password", "X-Authorization"} {
		if mustJSON(t, changes[key]) != mustJSON(t, want) {
			t.Errorf("changes[%s] = %v, want redacted both sides", key, changes[key])
		}
	}
	if mustJSON(t, changes["name"]) != `{"from":"kept","to":"kept2"}` {
		t.Errorf("an allowed key was changed: %v", changes["name"])
	}
	if detail["session_cookie"] != "[redacted]" || detail["dsn"] != "[redacted]" {
		t.Errorf("detail = %v", detail)
	}
	everything := string(ev.Changes) + string(ev.Detail) + h.logs.String()
	for _, sentinel := range []string{"P1-SENTINEL", "P2-SENTINEL", "A1-SENTINEL", "A2-SENTINEL", "C-SENTINEL", "D-SENTINEL"} {
		if strings.Contains(everything, sentinel) {
			t.Errorf("%s reached the event or the log", sentinel)
		}
	}
}

func TestDigestAndURLDropSentinels(t *testing.T) {
	h := newMemHarness()
	query := "SELECT * FROM logs WHERE session_id = 'Q-SENTINEL'"
	e := event(audit.SavedQueryCreate)
	e.After = audit.NewFields().
		Digest("query", query).
		URL("webhook", "https://U-SENTINEL:PW-SENTINEL@hooks.example.com:8443/path/PATH-SENTINEL?k=QS-SENTINEL#F-SENTINEL").
		URL("not_url", "relative/REL-SENTINEL").
		Set("homepage", "https://u:SET-SENTINEL@docs.example.com/SET-PATH-SENTINEL").
		Set("note", "plain text")
	h.rec.Record(context.Background(), e)

	ev := h.store.events[0]
	changes := decode(t, ev.Changes)
	sum := sha256.Sum256([]byte(query))
	wantDigest := map[string]any{"to": map[string]any{"sha256": hex.EncodeToString(sum[:]), "bytes": float64(len(query))}}
	if mustJSON(t, changes["query"]) != mustJSON(t, wantDigest) {
		t.Errorf("query = %v, want %v", changes["query"], wantDigest)
	}
	for key, want := range map[string]string{
		"webhook":  "https://hooks.example.com:8443",
		"not_url":  "[redacted]",
		"homepage": "https://docs.example.com",
		"note":     "plain text",
	} {
		if got := changes[key].(map[string]any)["to"]; got != want {
			t.Errorf("%s = %v, want %q", key, got, want)
		}
	}
	everything := string(ev.Changes) + h.logs.String()
	if strings.Contains(everything, "SENTINEL") {
		t.Errorf("a sentinel reached the event or the log: %s", everything)
	}
}

// Query keeps query text whole up to 8 KiB, eight times a plain string, and
// cuts a longer one with a marker.
func TestQueryRecordsTextInFull(t *testing.T) {
	h := newMemHarness()
	long := "SELECT * FROM logs WHERE " + strings.Repeat("a = 1 AND ", 300) + "b = 2"
	huge := strings.Repeat("x", 9000)
	e := event(audit.QueryRun)
	e.Detail = audit.NewFields().Query("query", long).Query("huge", huge)
	h.rec.Record(context.Background(), e)

	got := decode(t, h.store.events[0].Detail)
	if got["query"] != long {
		t.Errorf("query is %d bytes, want the %d-byte text whole", len(got["query"].(string)), len(long))
	}
	if cut, _ := got["huge"].(string); len(cut) != 8*1024 || !strings.HasSuffix(cut, "...") {
		t.Errorf("huge is %d bytes, want 8192 ending in ...", len(cut))
	}
	if !strings.Contains(h.logs.String(), "a = 1 AND b = 2") {
		t.Error("the log line does not hold the query text")
	}
}

func TestPayloadCapSetsTruncated(t *testing.T) {
	h := newMemHarness()
	e := event(audit.DashboardUpdate)
	detail := audit.NewFields()
	big := strings.Repeat("é", 1500)
	for i := range 40 {
		detail.Set("key_"+string(rune('A'+i)), big)
	}
	e.Detail = detail
	h.rec.Record(context.Background(), e)

	ev := h.store.events[0]
	if size := len(ev.Changes) + len(ev.Detail); size > 16*1024 {
		t.Errorf("changes plus detail is %d bytes, over 16 KiB", size)
	}
	got := decode(t, ev.Detail)
	if got["truncated"] != true {
		t.Fatalf("detail.truncated = %v, want true", got["truncated"])
	}
	first, ok := got["key_A"].(string)
	if !ok || len(first) > 1024 || !strings.HasSuffix(first, "...") {
		t.Errorf("key_A is %d bytes, want at most 1024 ending in ...", len(first))
	}
	if _, ok := got["key_"+string(rune('A'+39))]; ok {
		t.Error("the last key survived the cap")
	}

	// A key that JSON escapes grows six times when encoded; the cap counts
	// the encoded form.
	escaped := event(audit.DashboardUpdate)
	escaped.Detail = audit.NewFields().Set(strings.Repeat("<", 4000), "x").Set("after", "y")
	h.rec.Record(context.Background(), escaped)
	ev = h.store.events[1]
	if size := len(ev.Changes) + len(ev.Detail); size > 16*1024 {
		t.Errorf("escaped key made changes plus detail %d bytes, over 16 KiB", size)
	}
	if got := decode(t, ev.Detail); got["truncated"] != true || got["after"] != nil {
		t.Errorf("detail = %v, want the oversized key and every later key dropped", got)
	}
}

func TestEveryInsertHasTheTimeout(t *testing.T) {
	h := newMemHarness()
	ctx := context.Background()
	h.rec.Record(ctx, event(audit.SourceUpdate))
	if err := h.rec.RecordOrFail(ctx, event(audit.LogExportCreate)); err != nil {
		t.Fatal(err)
	}
	if err := h.rec.Batch(h.store).Add(ctx, event(audit.APITokenCreate)); err != nil {
		t.Fatal(err)
	}
	if len(h.store.deadlines) != 3 {
		t.Fatalf("%d of 3 inserts had a deadline", len(h.store.deadlines))
	}
	for i, d := range h.store.deadlines {
		if d <= 0 || d > 2*time.Second {
			t.Errorf("insert %d had %v left, want at most 2s", i, d)
		}
	}
}

func TestRecordWithFailingStoreLogsStoredFalse(t *testing.T) {
	st := &memStore{fail: errors.New("disk full")}
	rec, buf := newHarness(st)
	rec.Record(context.Background(), event(audit.SourceUpdate))

	lines := auditLines(t, buf)
	if len(lines) != 1 || lines[0]["stored"] != false || lines[0]["action"] != "source.update" {
		t.Fatalf("audit lines = %v, want one with stored false", lines)
	}
	if !strings.Contains(buf.String(), "disk full") {
		t.Error("the insert error was not logged")
	}
}

func TestRecordOrFailReturnsErrorAndWritesNoLine(t *testing.T) {
	st := &memStore{fail: errors.New("disk full")}
	rec, buf := newHarness(st)
	if err := rec.RecordOrFail(context.Background(), event(audit.LogExportCreate)); err == nil {
		t.Fatal("RecordOrFail returned nil for a failed insert")
	}
	if lines := auditLines(t, buf); len(lines) != 0 {
		t.Errorf("audit lines = %v, want none", lines)
	}

	h := newMemHarness()
	if err := h.rec.RecordOrFail(context.Background(), event(audit.LogExportCreate)); err != nil {
		t.Fatal(err)
	}
	if lines := auditLines(t, h.logs); len(h.store.events) != 1 || len(lines) != 1 || lines[0]["stored"] != true {
		t.Errorf("stored %d events and wrote %v", len(h.store.events), lines)
	}
}

func TestRecordLogLineCarriesTheEnvelope(t *testing.T) {
	h := newMemHarness()
	h.rec.Record(context.Background(), event(audit.SourceUpdate))
	lines := auditLines(t, h.logs)
	if len(lines) != 1 {
		t.Fatalf("audit lines = %v", lines)
	}
	l := lines[0]
	ev := h.store.events[0]
	if l["level"] != "INFO" || l["id"] != ev.ID || l["actor_id"] != float64(7) || l["source_id"] != float64(3) ||
		l["team_id"] != nil || l["route"] != "/api/v1/admin/sources/:sourceID" || l["stored"] != true {
		t.Errorf("log line = %v", l)
	}
	if _, ok := l["changes"].(map[string]any); !ok {
		t.Errorf("changes is %T in the log line, want an object", l["changes"])
	}

	quiet, buf := newHarness(&memStore{}, func(o *audit.Options) { o.LogEvents = false })
	quiet.Record(context.Background(), event(audit.SourceUpdate))
	if lines := auditLines(t, buf); len(lines) != 0 {
		t.Errorf("log_events false wrote %v", lines)
	}
}

func TestUnregisteredActionIsNotStored(t *testing.T) {
	h := newMemHarness()
	h.rec.Record(context.Background(), event(audit.Action("source.frobnicate")))
	if err := h.rec.RecordOrFail(context.Background(), event(audit.Action("nope"))); err == nil {
		t.Error("RecordOrFail accepted an unregistered action")
	}
	if len(h.store.events) != 0 || len(auditLines(t, h.logs)) != 0 {
		t.Errorf("unregistered action stored %d events", len(h.store.events))
	}
}

func TestDemoModeWritesTheLineOnly(t *testing.T) {
	st := &memStore{fail: errors.New("must not be called")}
	rec, buf := newHarness(st, func(o *audit.Options) { o.DemoReadOnly = true })
	rec.Record(context.Background(), event(audit.SourceUpdate))
	if err := rec.RecordOrFail(context.Background(), event(audit.LogExportCreate)); err != nil {
		t.Errorf("RecordOrFail in demo mode = %v", err)
	}
	lines := auditLines(t, buf)
	if len(lines) != 2 || lines[0]["stored"] != false || lines[1]["stored"] != false {
		t.Errorf("audit lines = %v, want two with stored false", lines)
	}
}

func TestSetRejectsUnsupportedTypes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Set accepted a struct")
		}
	}()
	audit.NewFields().Set("connection", models.ConnectionInfo{Password: "x"})
}

// failCommit is a TxRunner whose commit fails after the callback succeeds.
type failCommit struct{ store.Store }

var errCommit = errors.New("commit failed")

func (f failCommit) WithTx(ctx context.Context, fn func(store.StoreOps) error) error {
	return f.Store.WithTx(ctx, func(tx store.StoreOps) error {
		if err := fn(tx); err != nil {
			return err
		}
		return errCommit
	})
}

func TestBatchPublishesOnlyAfterCommit(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: sqlitetest.Path(t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rec, buf := newHarness(db)
	errCallback := errors.New("later step failed")

	run := func(runner store.TxRunner, after error) error {
		var b *audit.Batch
		err := runner.WithTx(ctx, func(tx store.StoreOps) error {
			b = rec.Batch(tx)
			for range 2 {
				if err := b.Add(ctx, event(audit.APITokenCreate)); err != nil {
					return err
				}
			}
			if len(auditLines(t, buf)) != 0 {
				t.Error("Add wrote a log line before the commit")
			}
			return after
		})
		if err == nil {
			b.Publish(ctx)
		}
		return err
	}
	count := func() int {
		n, err := db.CountAuditEvents(ctx, models.AuditFilter{Action: string(audit.APITokenCreate)}, 100)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := run(db, errCallback); !errors.Is(err, errCallback) {
		t.Fatalf("callback error run = %v", err)
	}
	if err := run(failCommit{db}, nil); !errors.Is(err, errCommit) {
		t.Fatalf("failed commit run = %v", err)
	}
	if n, lines := count(), auditLines(t, buf); n != 0 || len(lines) != 0 {
		t.Fatalf("aborted batches left %d rows and %d lines", n, len(lines))
	}

	if err := run(db, nil); err != nil {
		t.Fatal(err)
	}
	lines := auditLines(t, buf)
	if n := count(); n != 2 || len(lines) != 2 {
		t.Fatalf("committed batch left %d rows and %d lines, want 2 and 2", n, len(lines))
	}
	for _, l := range lines {
		if l["stored"] != true {
			t.Errorf("published line = %v, want stored true", l)
		}
	}
}
