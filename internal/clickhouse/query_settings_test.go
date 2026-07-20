package clickhouse

import (
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// TestBuildQuerySettingsPrecedence verifies the settings merge used for every
// query context: the request timeout is the base, LogChef's per-query settings
// override it, and the per-source operator settings win over both so per-source
// caps / read-only / timeouts are enforced regardless of the request.
func TestBuildQuerySettingsPrecedence(t *testing.T) {
	t.Parallel()

	perQuery := clickhouse.Settings{
		"max_result_rows":      50000,
		"result_overflow_mode": "break",
	}
	source := clickhouse.Settings{
		"max_result_rows":   int64(1000), // hard cap wins over per-query limit
		"readonly":          2,
		"max_bytes_to_read": int64(1 << 30),
	}

	got := buildQuerySettings(30, perQuery, source)

	if got["max_execution_time"] != 30 {
		t.Fatalf("max_execution_time = %v, want 30", got["max_execution_time"])
	}
	// Source cap overrides the per-query limit.
	if got["max_result_rows"] != int64(1000) {
		t.Fatalf("max_result_rows = %v, want source cap 1000", got["max_result_rows"])
	}
	// Per-query-only setting survives.
	if got["result_overflow_mode"] != "break" {
		t.Fatalf("result_overflow_mode = %v, want break", got["result_overflow_mode"])
	}
	// Source-only settings are attached.
	if got["readonly"] != 2 {
		t.Fatalf("readonly = %v, want 2", got["readonly"])
	}
	if got["max_bytes_to_read"] != int64(1<<30) {
		t.Fatalf("max_bytes_to_read = %v, want 1073741824", got["max_bytes_to_read"])
	}
}

// TestBuildQuerySettingsNilMaps verifies the merge is safe with no per-query and
// no source settings: only the base timeout is present.
func TestBuildQuerySettingsNilMaps(t *testing.T) {
	t.Parallel()

	got := buildQuerySettings(60, nil, nil)
	if len(got) != 1 || got["max_execution_time"] != 60 {
		t.Fatalf("settings = %#v, want only max_execution_time=60", got)
	}
}

// TestClientQuerySettingsAttached verifies a client built with per-source
// QuerySettings attaches them to the query settings for every query.
func TestClientQuerySettingsAttached(t *testing.T) {
	t.Parallel()

	c := &Client{querySettings: clickhouse.Settings{"readonly": 2, "max_result_rows": int64(1000)}}
	got := buildQuerySettings(30, clickhouse.Settings{"max_result_rows": 50000}, c.querySettings)
	if got["readonly"] != 2 {
		t.Fatalf("readonly not attached from client: %#v", got)
	}
	if got["max_result_rows"] != int64(1000) {
		t.Fatalf("client cap did not override per-query limit: %#v", got)
	}
}
