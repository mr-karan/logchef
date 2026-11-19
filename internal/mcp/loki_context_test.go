package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/loki"
	"github.com/mr-karan/logchef/pkg/models"
)

// get_log_context on a Loki source reads the target row's stream, which the
// row's stream labels identify. It runs against a real Loki, such as the dev
// stack's loki-local, when LOGCHEF_TEST_LOKI_URL is set.
func TestLokiLogContextUsesStreamLabels(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("LOGCHEF_TEST_LOKI_URL"))
	if url == "" {
		t.Skip("LOGCHEF_TEST_LOKI_URL is not set; skipping the Loki log context test")
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	runID := "mcp" + hex.EncodeToString(buf)

	// Two streams that differ only in pod, with their lines interleaved.
	base := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	stream := func(pod string, offsets ...int) map[string]any {
		values := make([][2]string, len(offsets))
		for i, offset := range offsets {
			values[i] = [2]string{strconv.FormatInt(base.Add(time.Duration(offset)*time.Second).UnixNano(), 10), pod + strconv.Itoa(i+1)}
		}
		return map[string]any{"stream": map[string]string{"test_run": runID, "app": "api", "pod": pod}, "values": values}
	}
	pushLoki(t, url, runID, []map[string]any{stream("a", 0, 2, 4), stream("b", 1, 3, 5)})

	w := newWorld(t)
	w.ds.Register(loki.NewProvider(discardLogger()))
	conn, err := json.Marshal(models.LokiConnectionInfo{BaseURL: url, TenantID: runID, Selector: `{test_run="` + runID + `"}`})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "loki", SourceType: models.SourceTypeLoki, MetaTSField: "_time", ConnectionConfig: conn}
	if err := w.db.CreateSource(context.Background(), source); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	w.link(t, w.team, source.ID)
	if err := w.ds.InitializeSource(context.Background(), source); err != nil {
		t.Fatalf("InitializeSource: %v", err)
	}

	// An agent reads a row, then sends its value for every label field of the
	// schema. Loki adds service_name to these streams, so no hand-written
	// label set is complete.
	ids := map[string]any{"team_id": w.team, "source_id": source.ID}
	start, end := base.Add(-time.Minute).Format(time.RFC3339), base.Add(time.Minute).Format(time.RFC3339)
	var rows QueryResult
	for deadline := time.Now().Add(30 * time.Second); ; {
		result := callTool(t, w.server(), session(w.member), "query_logs", map[string]any{"team_id": w.team, "source_id": source.ID,
			"raw_sql": `{test_run="` + runID + `", pod="a"}`, "start_time": start, "end_time": end})
		if result.IsError {
			t.Fatalf("query_logs: %s", result.text())
		}
		if err := json.Unmarshal(result.StructuredContent, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows.Logs) == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(rows.Logs) != 3 || rows.Logs[1]["_msg"] != "a2" {
		t.Fatalf("rows of pod a = %v", rows.Logs)
	}
	var schema []SchemaColumnResult
	if result := callTool(t, w.server(), session(w.member), "get_source_schema", ids); result.IsError ||
		json.Unmarshal(result.StructuredContent, &schema) != nil {
		t.Fatalf("get_source_schema: %s", result.text())
	}
	labels := map[string]any{}
	for _, column := range schema {
		if column.Type == "label" {
			labels[column.Name] = rows.Logs[1][column.Name]
		}
	}

	result := callTool(t, w.server(), session(w.member), "get_log_context", map[string]any{"team_id": w.team, "source_id": source.ID,
		"timestamp": base.Add(2 * time.Second).UnixMilli(), "stream_labels": labels})
	if result.IsError {
		t.Fatalf("get_log_context with labels %v: %s", labels, result.text())
	}
	var window struct {
		BeforeLogs []map[string]any `json:"before_logs"`
		AfterLogs  []map[string]any `json:"after_logs"`
	}
	if err := json.Unmarshal([]byte(result.text()), &window); err != nil {
		t.Fatal(err)
	}

	var lines []string
	for _, row := range append(window.BeforeLogs, window.AfterLogs...) {
		if row["pod"] != "a" || row["app"] != "api" || row["test_run"] != runID {
			t.Fatalf("row %v lacks the stream labels of pod a", row)
		}
		lines = append(lines, row["_msg"].(string))
	}
	if got := strings.Join(lines, ","); got != "a1,a2,a3" || len(window.AfterLogs) != 1 {
		t.Fatalf("context lines = %s with %d after, want a1,a2,a3 with 1 after", got, len(window.AfterLogs))
	}
}

func pushLoki(t *testing.T, url, tenant string, streams any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"streams": streams})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", tenant)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("push returned %d", resp.StatusCode)
	}
}
