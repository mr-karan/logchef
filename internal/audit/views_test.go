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
