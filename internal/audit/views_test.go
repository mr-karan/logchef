package audit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/pkg/models"
)

// fillStrings sets every string field reachable from v, including map
// entries and string slices, to "<tag>:<path>;", so a test can tell which
// field leaked. The ";" keeps one path from being a prefix of another.
func fillStrings(v reflect.Value, tag, path string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillStrings(v.Elem(), tag, path)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillStrings(v.Field(i), tag, path+"."+v.Type().Field(i).Name)
			}
		}
	case reflect.String:
		v.SetString(tag + ":" + path + ";")
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			s := reflect.MakeSlice(v.Type(), 1, 1)
			s.Index(0).SetString(tag + ":" + path + "[0];")
			v.Set(s)
		}
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.String && v.Type().Elem().Kind() == reflect.String {
			m := reflect.MakeMap(v.Type())
			m.SetMapIndex(reflect.ValueOf(tag+":"+path+".key;"), reflect.ValueOf(tag+":"+path+".value;"))
			v.Set(m)
		}
	default:
	}
}

// viewCase builds one audited object filled with sentinels tagged tag, and
// lists the sentinels that must never reach an event.
type viewCase struct {
	name   string
	fields func(tag string) (*audit.Fields, []string)
}

var viewCases = []viewCase{
	{"session", func(tag string) (*audit.Fields, []string) {
		var x models.Session
		fillStrings(reflect.ValueOf(&x), tag, "Session")
		// SessionFields records only a time, so the probe gives the suite a
		// recorded string; the leak check on the session ID is what counts.
		return audit.SessionFields(&x).Set("probe", tag+":probe;"), []string{string(x.ID)}
	}},
	{"oauth consent", func(tag string) (*audit.Fields, []string) {
		var r models.OAuthAuthRequest
		fillStrings(reflect.ValueOf(&r), tag, "Request")
		return audit.OAuthConsentFields(&r, "mcp", r.GrantID), []string{string(r.ID), r.RedirectURI, r.CodeChallenge, r.State}
	}},
	{"oauth grant", func(tag string) (*audit.Fields, []string) {
		var g models.OAuthGrant
		fillStrings(reflect.ValueOf(&g), tag, "Grant")
		return audit.OAuthGrantFields(&g, "api"), nil
	}},
	{"log export", func(tag string) (*audit.Fields, []string) {
		literal := tag + "-literal"
		query := `SELECT * FROM logs WHERE session_id = '` + literal + `'`
		return audit.LogExportFields(models.QueryLanguageClickHouseSQL, tag+":format;", 100, query), []string{literal}
	}},
	{"clickhouse source", func(tag string) (*audit.Fields, []string) {
		var s models.Source
		fillStrings(reflect.ValueOf(&s), tag, "Source")
		s.SourceType = models.SourceTypeClickHouse
		return audit.SourceFields(&s), []string{s.Connection.Password, s.SecretRef}
	}},
	{"victorialogs source", func(tag string) (*audit.Fields, []string) {
		var s models.Source
		fillStrings(reflect.ValueOf(&s), tag, "Source")
		s.SourceType = models.SourceTypeVictoriaLogs
		var c models.VictoriaLogsConnectionInfo
		fillStrings(reflect.ValueOf(&c), tag, "VictoriaLogs")
		c.BaseURL = "https://" + tag + "-user:" + tag + "-pass@vl.example.com/" + tag + "-path?q=" + tag + "-query"
		c.Scope.Query = `_time:5m AND session:"` + tag + `-literal"`
		raw, err := json.Marshal(c)
		if err != nil {
			panic(err)
		}
		s.ConnectionConfig = raw
		var leaks []string
		for k, v := range c.Headers {
			leaks = append(leaks, k, v)
		}
		return audit.SourceFields(&s), append(leaks,
			c.Auth.Password, c.Auth.Token,
			tag+"-user", tag+"-pass", tag+"-path", tag+"-query", tag+"-literal")
	}},
	{"report", func(tag string) (*audit.Fields, []string) {
		var r models.Report
		fillStrings(reflect.ValueOf(&r), tag, "Report")
		r.Variables = map[string]any{"service": tag + "-variable-value"}
		return audit.ReportFields(&r), []string{tag + "-variable-value"}
	}},
	{"loki source", func(tag string) (*audit.Fields, []string) {
		var s models.Source
		fillStrings(reflect.ValueOf(&s), tag, "Source")
		s.SourceType = models.SourceTypeLoki
		var c models.LokiConnectionInfo
		fillStrings(reflect.ValueOf(&c), tag, "Loki")
		c.BaseURL = "https://" + tag + "-user:" + tag + "-pass@loki.example.com/" + tag + "-path?q=" + tag + "-query"
		c.Selector = `{app="` + tag + `-literal"}`
		raw, err := json.Marshal(c)
		if err != nil {
			panic(err)
		}
		s.ConnectionConfig = raw
		var leaks []string
		for k, v := range c.Headers {
			leaks = append(leaks, k, v)
		}
		return audit.SourceFields(&s), append(leaks,
			c.Auth.Password, c.Auth.Token,
			tag+"-user", tag+"-pass", tag+"-path", tag+"-query", tag+"-literal")
	}},
	{"user", func(tag string) (*audit.Fields, []string) {
		var u models.User
		fillStrings(reflect.ValueOf(&u), tag, "User")
		return audit.UserFields(&u), []string{u.PasswordHash}
	}},
	{"api token", func(tag string) (*audit.Fields, []string) {
		var tok models.APIToken
		fillStrings(reflect.ValueOf(&tok), tag, "APIToken")
		return audit.APITokenFields(&tok), []string{tok.TokenHash, tok.Prefix}
	}},
	{"team", func(tag string) (*audit.Fields, []string) {
		var team models.Team
		fillStrings(reflect.ValueOf(&team), tag, "Team")
		return audit.TeamFields(&team), nil
	}},
	{"team member", func(tag string) (*audit.Fields, []string) {
		var m models.TeamMember
		fillStrings(reflect.ValueOf(&m), tag, "TeamMember")
		return audit.TeamMemberFields(&m), nil
	}},
	{"sensitive setting", func(tag string) (*audit.Fields, []string) {
		var s models.SystemSetting
		fillStrings(reflect.ValueOf(&s), tag, "Setting")
		s.IsSensitive = true
		return audit.SettingFields(&s), []string{s.Value}
	}},
	{"setting whose key names a credential", func(tag string) (*audit.Fields, []string) {
		var s models.SystemSetting
		fillStrings(reflect.ValueOf(&s), tag, "Setting")
		s.Key = "auth.client_secret"
		s.IsSensitive = false
		return audit.SettingFields(&s), []string{s.Value}
	}},
	{"sensitive setting by key", func(tag string) (*audit.Fields, []string) {
		var s models.SystemSetting
		fillStrings(reflect.ValueOf(&s), tag, "Setting")
		s.Key = "ai.api_key"
		return audit.SettingFields(&s), []string{s.Value}
	}},
}

// TestViewsKeepSecretsOut records a create, an update and a delete of every
// object with a view and asserts that no secret sentinel reaches the stored
// row or the log line.
func TestViewsKeepSecretsOut(t *testing.T) {
	for _, vc := range viewCases {
		t.Run(vc.name, func(t *testing.T) {
			h := newMemHarness()
			before, leaksA := vc.fields("SENTA")
			after, leaksB := vc.fields("SENTB")
			for _, e := range []struct{ before, after *audit.Fields }{{nil, after}, {before, after}, {before, nil}} {
				ev := event(audit.SourceUpdate)
				ev.Before, ev.After, ev.Detail = e.before, e.after, after
				h.rec.Record(context.Background(), ev)
			}
			if len(h.store.events) != 3 {
				t.Fatalf("stored %d events, want 3", len(h.store.events))
			}
			var stored strings.Builder
			for _, ev := range h.store.events {
				fmt.Fprintf(&stored, "%s%s", ev.Changes, ev.Detail)
			}
			everything := stored.String() + h.logs.String()
			for _, leak := range append(leaksA, leaksB...) {
				if leak != "" && strings.Contains(everything, leak) {
					t.Errorf("sentinel %q reached the event or the log", leak)
				}
			}
			if !strings.Contains(stored.String(), "SENTB:") {
				t.Error("no allowed field was recorded, so the test proves nothing")
			}
		})
	}
}

func TestSourceFieldsRecordPasswordChange(t *testing.T) {
	h := newMemHarness()
	src := models.Source{Name: "logs.app", SourceType: models.SourceTypeClickHouse, Connection: models.ConnectionInfo{Host: "ch:9000", Password: "old-pw"}}
	before := audit.SourceFields(&src)
	src.Connection.Password = "new-pw"
	ev := event(audit.SourceUpdate)
	ev.Before, ev.After = before, audit.SourceFields(&src)
	h.rec.Record(context.Background(), ev)

	if got := string(h.store.events[0].Changes); got != `{"connection.password":{"secret":true,"changed":true}}` {
		t.Errorf("changes = %s", got)
	}
}

// Marking a setting sensitive records the change of the flag, never the value
// it held before.
func TestSettingFieldsMarkedSensitiveRecordNoValue(t *testing.T) {
	h := newMemHarness()
	setting := models.SystemSetting{Key: "custom.webhook", Value: "plain-before", ValueType: "string", Category: "alerts"}
	before := audit.SettingFields(&setting)
	setting.IsSensitive = true
	ev := event(audit.SettingUpdate)
	ev.Before, ev.After = before, audit.SettingFields(&setting)
	h.rec.Record(context.Background(), ev)

	var changes map[string]map[string]any
	if err := json.Unmarshal(h.store.events[0].Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if c := changes["is_sensitive"]; c["from"] != false || c["to"] != true {
		t.Errorf("is_sensitive change = %v", c)
	}
	if c := changes["value"]; c["secret"] != true || c["from"] != nil || c["to"] != nil {
		t.Errorf("value change = %v, want a secret marker", c)
	}
	if strings.Contains(string(h.store.events[0].Changes)+h.logs.String(), "plain-before") {
		t.Error("the earlier value reached the event or the log line")
	}
}

func TestSourceFieldsRecordLokiConnection(t *testing.T) {
	h := newMemHarness()
	loki := func(password, selector string) *audit.Fields {
		raw, err := json.Marshal(models.LokiConnectionInfo{
			BaseURL:  "https://loki.example.com",
			Auth:     models.LokiAuth{Mode: "basic", Username: "reader", Password: password},
			Selector: selector,
		})
		if err != nil {
			t.Fatal(err)
		}
		return audit.SourceFields(&models.Source{Name: "loki", SourceType: models.SourceTypeLoki, ConnectionConfig: raw})
	}
	ev := event(audit.SourceUpdate)
	ev.Before, ev.After = loki("old-pw", `{app="api"}`), loki("new-pw", `{app="web"}`)
	h.rec.Record(context.Background(), ev)

	var changes map[string]map[string]any
	if err := json.Unmarshal(h.store.events[0].Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Errorf("changes = %s, want the password and the selector", h.store.events[0].Changes)
	}
	if c := changes["connection.auth.password"]; c["secret"] != true || c["changed"] != true {
		t.Errorf("password change = %v", c)
	}
	if to, ok := changes["connection.selector"]["to"].(map[string]any); !ok || to["sha256"] == nil {
		t.Errorf("selector change = %v, want a digest", changes["connection.selector"])
	}
	for _, leak := range []string{"old-pw", "new-pw", "api", "web"} {
		if strings.Contains(string(h.store.events[0].Changes)+h.logs.String(), `"`+leak) {
			t.Errorf("%q reached the event or the log line", leak)
		}
	}
}
