package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompilationJSON(t *testing.T) {
	for _, tc := range []struct {
		query  string
		sql    string
		logsql string
		logql  string
	}{
		{`level="error"`, "`level` = 'error'", `level:="error"`, `{service_name=~".+", level="error"}`},
		{`level="error" | count() by service`, "GROUP BY `service`", "stats by (service) count()", "AGGREGATION_NOT_SUPPORTED"},
		{`| count() by time(5m)`, "INTERVAL", "5m", "AGGREGATION_NOT_SUPPORTED"},
		{`level=`, "UNEXPECTED_END", "UNEXPECTED_END", "UNEXPECTED_END"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			b, err := json.Marshal(compile(tc.query, "5m"))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]output
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			for engine, want := range map[string]string{"clickhouse": tc.sql, "victorialogs": tc.logsql, "loki": tc.logql} {
				out := got[engine]
				if out.Error != nil {
					if out.Error.Code != want || out.Error.Message == "" || out.Error.Position == nil || out.Error.Position.Column < 1 || out.Error.Position.Line != 1 || out.Query != "" {
						t.Errorf("%s: %s", engine, b)
					}
				} else if !strings.Contains(out.Query, want) {
					t.Errorf("%s: expected %q in %q", engine, want, out.Query)
				}
			}
		})
	}
}
