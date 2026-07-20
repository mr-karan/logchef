package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRedactedConnectionConfigVictoriaLogsBlanksSecrets verifies the redacted
// connection config never emits credential-bearing values to source viewers:
// auth password/token are blanked, and custom header VALUES (which commonly
// hold secrets such as an X-API-Key for a fronting proxy) are blanked while
// their keys are preserved so the editor can show which headers exist (#98).
func TestRedactedConnectionConfigVictoriaLogsBlanksSecrets(t *testing.T) {
	t.Parallel()

	conn := VictoriaLogsConnectionInfo{
		BaseURL: "https://vl.example.com",
		Auth: VictoriaLogsAuth{
			Mode:     "basic",
			Username: "svc",
			Password: "super-secret",
			Token:    "tok-secret",
		},
		Headers: map[string]string{
			"X-API-Key":     "leaked-key-value",
			"Authorization": "Bearer downstream-secret",
		},
	}
	raw, err := json.Marshal(conn)
	if err != nil {
		t.Fatalf("marshal conn: %v", err)
	}

	source := &Source{
		SourceType:       SourceTypeVictoriaLogs,
		ConnectionConfig: raw,
	}

	redacted := source.RedactedConnectionConfig()

	// No secret value may appear anywhere in the serialized redacted config.
	for _, secret := range []string{"super-secret", "tok-secret", "leaked-key-value", "downstream-secret"} {
		if strings.Contains(string(redacted), secret) {
			t.Fatalf("redacted config leaked secret %q: %s", secret, redacted)
		}
	}

	var out VictoriaLogsConnectionInfo
	if err := json.Unmarshal(redacted, &out); err != nil {
		t.Fatalf("unmarshal redacted: %v", err)
	}
	if out.Auth.Password != "" || out.Auth.Token != "" {
		t.Fatalf("expected auth secrets blanked, got password=%q token=%q", out.Auth.Password, out.Auth.Token)
	}
	// Keys are preserved (so the editor knows which headers exist), values blank.
	if len(out.Headers) != 2 {
		t.Fatalf("expected header keys preserved, got %#v", out.Headers)
	}
	for key, value := range out.Headers {
		if value != "" {
			t.Fatalf("expected header %q value blanked, got %q", key, value)
		}
	}
	// Username is not a secret and may remain to identify the auth config.
	if out.Auth.Username != "svc" {
		t.Fatalf("expected username preserved, got %q", out.Auth.Username)
	}
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
func strPtr(v string) *string { return &v }

// TestClickHouseQuerySettingsValidate covers the value validation: numeric
// settings must be non-negative, readonly must be 0/1/2, and result_overflow_mode
// must be "throw" or "break".
func TestClickHouseQuerySettingsValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		s       *ClickHouseQuerySettings
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"empty is valid", &ClickHouseQuerySettings{}, false},
		{
			name: "all valid",
			s: &ClickHouseQuerySettings{
				MaxExecutionTime:   intPtr(30),
				MaxResultRows:      int64Ptr(1000),
				MaxResultBytes:     int64Ptr(1 << 20),
				MaxRowsToRead:      int64Ptr(5000),
				MaxBytesToRead:     int64Ptr(1 << 30),
				Readonly:           intPtr(2),
				ResultOverflowMode: strPtr("break"),
			},
			wantErr: false,
		},
		{"negative max_execution_time", &ClickHouseQuerySettings{MaxExecutionTime: intPtr(-1)}, true},
		{"negative max_result_rows", &ClickHouseQuerySettings{MaxResultRows: int64Ptr(-1)}, true},
		{"negative max_result_bytes", &ClickHouseQuerySettings{MaxResultBytes: int64Ptr(-1)}, true},
		{"negative max_rows_to_read", &ClickHouseQuerySettings{MaxRowsToRead: int64Ptr(-1)}, true},
		{"negative max_bytes_to_read", &ClickHouseQuerySettings{MaxBytesToRead: int64Ptr(-1)}, true},
		{"readonly too high", &ClickHouseQuerySettings{Readonly: intPtr(3)}, true},
		{"readonly negative", &ClickHouseQuerySettings{Readonly: intPtr(-1)}, true},
		{"readonly 0 ok", &ClickHouseQuerySettings{Readonly: intPtr(0)}, false},
		{"bad overflow mode", &ClickHouseQuerySettings{ResultOverflowMode: strPtr("halt")}, true},
		{"throw overflow mode", &ClickHouseQuerySettings{ResultOverflowMode: strPtr("throw")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.s.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestClickHouseQuerySettingsToSettingsMap verifies only-set settings are
// emitted and a nil/empty struct yields nil.
func TestClickHouseQuerySettingsToSettingsMap(t *testing.T) {
	t.Parallel()

	if m := (*ClickHouseQuerySettings)(nil).ToSettingsMap(); m != nil {
		t.Fatalf("nil settings map = %#v, want nil", m)
	}
	if m := (&ClickHouseQuerySettings{}).ToSettingsMap(); m != nil {
		t.Fatalf("empty settings map = %#v, want nil", m)
	}

	s := &ClickHouseQuerySettings{
		MaxResultRows:      int64Ptr(1000),
		Readonly:           intPtr(2),
		ResultOverflowMode: strPtr("throw"),
	}
	m := s.ToSettingsMap()
	if len(m) != 3 {
		t.Fatalf("settings map = %#v, want 3 entries", m)
	}
	if m["max_result_rows"] != int64(1000) || m["readonly"] != 2 || m["result_overflow_mode"] != "throw" {
		t.Fatalf("unexpected settings map: %#v", m)
	}
	if _, ok := m["max_execution_time"]; ok {
		t.Fatalf("unset setting present in map: %#v", m)
	}
}

// TestClickHouseSettingsRoundTripThroughConnectionConfig verifies settings
// persist into connection_config JSON via SyncConnectionConfig and are restored
// by HydrateConnection.
func TestClickHouseSettingsRoundTripThroughConnectionConfig(t *testing.T) {
	t.Parallel()

	src := &Source{
		SourceType: SourceTypeClickHouse,
		Connection: ConnectionInfo{
			Host:      "ch:9000",
			Database:  "logs",
			TableName: "app",
			Settings: &ClickHouseQuerySettings{
				MaxResultRows:      int64Ptr(500),
				MaxExecutionTime:   intPtr(15),
				ResultOverflowMode: strPtr("break"),
			},
		},
	}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	if !strings.Contains(string(src.ConnectionConfig), "max_result_rows") {
		t.Fatalf("connection_config missing settings: %s", src.ConnectionConfig)
	}

	// Rehydrate from the persisted JSON into a fresh source.
	loaded := &Source{SourceType: SourceTypeClickHouse, ConnectionConfig: src.ConnectionConfig}
	if err := loaded.HydrateConnection(); err != nil {
		t.Fatalf("HydrateConnection: %v", err)
	}
	got := loaded.Connection.Settings
	if got == nil {
		t.Fatal("settings not restored after hydrate")
	}
	if got.MaxResultRows == nil || *got.MaxResultRows != 500 {
		t.Fatalf("MaxResultRows = %v, want 500", got.MaxResultRows)
	}
	if got.MaxExecutionTime == nil || *got.MaxExecutionTime != 15 {
		t.Fatalf("MaxExecutionTime = %v, want 15", got.MaxExecutionTime)
	}
	if got.ResultOverflowMode == nil || *got.ResultOverflowMode != "break" {
		t.Fatalf("ResultOverflowMode = %v, want break", got.ResultOverflowMode)
	}
}

// TestRedactedConnectionConfigClickHousePreservesSettings verifies the redacted
// config drops the password but returns the (non-secret) settings so the UI can
// display and round-trip them.
func TestRedactedConnectionConfigClickHousePreservesSettings(t *testing.T) {
	t.Parallel()

	src := &Source{
		SourceType: SourceTypeClickHouse,
		Connection: ConnectionInfo{
			Host:      "ch:9000",
			Username:  "reader",
			Password:  "super-secret",
			Database:  "logs",
			TableName: "app",
			Settings:  &ClickHouseQuerySettings{MaxResultRows: int64Ptr(1000), Readonly: intPtr(2)},
		},
	}

	redacted := src.RedactedConnectionConfig()
	if strings.Contains(string(redacted), "super-secret") {
		t.Fatalf("redacted config leaked password: %s", redacted)
	}

	var out ConnectionInfoResponse
	if err := json.Unmarshal(redacted, &out); err != nil {
		t.Fatalf("unmarshal redacted: %v", err)
	}
	if !out.HasPassword {
		t.Fatal("expected has_password=true")
	}
	if out.Settings == nil || out.Settings.MaxResultRows == nil || *out.Settings.MaxResultRows != 1000 {
		t.Fatalf("settings not preserved in redacted config: %#v", out.Settings)
	}
	if out.Settings.Readonly == nil || *out.Settings.Readonly != 2 {
		t.Fatalf("readonly not preserved: %#v", out.Settings)
	}
}
