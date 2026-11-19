package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// newAuditLoggingEnv is an audit route env whose recorder also writes the
// audit log line to the env's log buffer.
func newAuditLoggingEnv(t *testing.T) *auditRouteEnv {
	t.Helper()
	return newAuditRouteEnv(t, withAuditLogLines())
}

func changesOf(t *testing.T, ev models.AuditEvent) map[string]map[string]any {
	t.Helper()
	var changes map[string]map[string]any
	if err := json.Unmarshal(ev.Changes, &changes); err != nil {
		t.Fatalf("changes %s: %v", ev.Changes, err)
	}
	return changes
}

// wantNoText fails when text appears in the stored event or in any log line.
func (e *auditRouteEnv) wantNoText(texts ...string) {
	e.t.Helper()
	var stored strings.Builder
	events := e.events()
	for i := range events {
		fmt.Fprintf(&stored, "%s%s%s", events[i].Changes, events[i].Detail, events[i].TargetLabel)
	}
	for _, text := range texts {
		if strings.Contains(stored.String(), text) {
			e.t.Errorf("%q reached a stored event", text)
		}
		if strings.Contains(e.logs.String(), text) {
			e.t.Errorf("%q reached the log", text)
		}
	}
}

func wantSecretChanged(t *testing.T, changes map[string]map[string]any, key string) {
	t.Helper()
	if c, ok := changes[key]; !ok || c["secret"] != true || c["changed"] != true || c["from"] != nil || c["to"] != nil {
		t.Errorf("%s change = %v, want a secret marker", key, changes[key])
	}
}

func TestAuditSourceEventsRecordFields(t *testing.T) {
	t.Parallel()
	e := newAuditLoggingEnv(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[]}`)
	}))
	t.Cleanup(backend.Close)
	connection := func(password string) string {
		return fmt.Sprintf(`{"base_url":%q,"auth":{"mode":"basic","username":"reader","password":%q}}`, backend.URL, password)
	}

	resp := e.do(http.MethodPost, "/api/v1/admin/sources",
		`{"name":"vl-fields","source_type":"victorialogs","meta_ts_field":"_time","connection":`+connection("first-pw-sentinel")+`}`, e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var created struct {
		Data struct {
			ID models.SourceID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &created); err != nil || created.Data.ID == 0 {
		t.Fatalf("decode created source: %v", err)
	}
	sourceID := strconv.Itoa(int(created.Data.ID))
	ev := e.only()
	checkActorAndTarget(t, ev, "source.create", e.admin, "source", sourceID)
	changes := changesOf(t, ev)
	if c := changes["name"]; c["to"] != "vl-fields" || c["from"] != nil {
		t.Errorf("create name change = %v", c)
	}
	wantSecretChanged(t, changes, "connection.auth.password")
	e.wantNoText("first-pw-sentinel")
	e.reset()

	// A password change records that the password changed, and only that.
	wantStatus(t, e.do(http.MethodPut, "/api/v1/admin/sources/"+sourceID,
		`{"connection":`+connection("second-pw-sentinel")+`}`, e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "source.update", e.admin, "source", sourceID)
	changes = changesOf(t, ev)
	if len(changes) != 1 {
		t.Errorf("password update changes = %s, want only the password", ev.Changes)
	}
	wantSecretChanged(t, changes, "connection.auth.password")
	if !strings.Contains(e.logs.String(), `"connection.auth.password":{"secret":true,"changed":true}`) {
		t.Errorf("the audit log line has no password change marker: %s", e.logs)
	}
	e.wantNoText("first-pw-sentinel", "second-pw-sentinel")
	e.reset()

	// A plain field records its old and new value.
	wantStatus(t, e.do(http.MethodPut, "/api/v1/admin/sources/"+sourceID, `{"description":"now described"}`, e.cookie), http.StatusOK)
	if c := changesOf(t, e.only())["description"]; c["from"] != "" || c["to"] != "now described" {
		t.Errorf("description change = %v", c)
	}
	e.reset()

	wantStatus(t, e.do(http.MethodDelete, "/api/v1/admin/sources/"+sourceID, ``, e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "source.delete", e.admin, "source", sourceID)
	if ev.TargetLabel != "vl-fields" {
		t.Errorf("delete target label %q", ev.TargetLabel)
	}
	if c := changesOf(t, ev)["name"]; c["from"] != "vl-fields" || c["to"] != nil {
		t.Errorf("delete name change = %v", c)
	}
	e.wantNoText("second-pw-sentinel")
}

func TestAuditSettingEventsRecordFields(t *testing.T) {
	t.Parallel()
	e := newAuditLoggingEnv(t)
	put := func(key, body string) {
		t.Helper()
		wantStatus(t, e.do(http.MethodPut, "/api/v1/admin/settings/"+key, body, e.cookie), http.StatusOK)
	}

	// A sensitive setting records that its value changed, never the value.
	put("ai.custom_value", `{"value":"first-value-sentinel","value_type":"string","category":"ai","is_sensitive":true}`)
	ev := e.only()
	checkActorAndTarget(t, ev, "setting.update", e.admin, "setting", "ai.custom_value")
	wantSecretChanged(t, changesOf(t, ev), "value")
	e.reset()
	put("ai.custom_value", `{"value":"second-value-sentinel","value_type":"string","category":"ai"}`)
	changes := changesOf(t, e.only())
	if len(changes) != 1 {
		t.Errorf("sensitive update changes = %v, want only the value", changes)
	}
	wantSecretChanged(t, changes, "value")
	e.wantNoText("first-value-sentinel", "second-value-sentinel")
	e.reset()

	// A plain setting records its old and new value.
	put("ai.model", `{"value":"model-a","value_type":"string","category":"ai"}`)
	e.reset()
	put("ai.model", `{"value":"model-b","value_type":"string","category":"ai"}`)
	if c := changesOf(t, e.only())["value"]; c["from"] != "model-a" || c["to"] != "model-b" {
		t.Errorf("plain value change = %v", c)
	}
	e.reset()

	wantStatus(t, e.do(http.MethodDelete, "/api/v1/admin/settings/ai.model", ``, e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "setting.delete", e.admin, "setting", "ai.model")
	if c := changesOf(t, ev)["value"]; c["from"] != "model-b" || c["to"] != nil {
		t.Errorf("delete value change = %v", c)
	}
	e.reset()

	// Deleting a key that does not exist answers 404 and records nothing.
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/admin/settings/ai.model", ``, e.cookie), http.StatusNotFound)
	if got := e.events(); len(got) != 0 {
		t.Errorf("a missing key stored %d events", len(got))
	}
}
