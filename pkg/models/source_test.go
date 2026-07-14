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
