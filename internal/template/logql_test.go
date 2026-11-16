package template_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/template"
)

func TestLogQLDialect(t *testing.T) {
	interval, _ := template.ParseInterval("5m")
	day, _ := template.ParseInterval("24h")
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	builtins := template.Builtins{From: &from, To: &to, Interval: interval}
	variables := []template.Variable{
		{Name: "svc", Type: template.TypeString, Value: `pay"ments`},
		{Name: "n", Type: template.TypeNumber, Value: float64(500)},
	}

	tests := []struct {
		name, query, want string
		builtins          *template.Builtins
	}{
		{"line_format passes through", `{app="api"} | line_format "{{.msg}}"`, `{app="api"} | line_format "{{.msg}}"`, nil},
		{"LogQL template names pass through", `{app="api"} | line_format "{{.msg}} {{__line__}} {{ __timestamp__ }}"`, `{app="api"} | line_format "{{.msg}} {{__line__}} {{ __timestamp__ }}"`, nil},
		{"raw string template passes through", "{app=\"api\"} | label_format x=`{{.a}} {{b}}`", "{app=\"api\"} | label_format x=`{{.a}} {{b}}`", nil},
		{"variable in an expression", `{app={{svc}}}`, `{app="pay\"ments"}`, nil},
		{"variable inside a string", `{app="{{svc}}"}`, `{app="pay\"ments"}`, nil},
		{"variable beside a LogQL template", `{app="api"} | line_format "{{svc}} {{.msg}}"`, `{app="api"} | line_format "pay\"ments {{.msg}}"`, nil},
		{"number", `{app="api"} | status >= {{n}}`, `{app="api"} | status >= 500`, nil},
		{"interval", `sum(count_over_time({app="api"}[{{__interval}}]))`, `sum(count_over_time({app="api"}[5m]))`, nil},
		{"day interval", `count_over_time({app="api"}[{{__interval}}])`, `count_over_time({app="api"}[1d])`, &template.Builtins{Interval: day}},
		{"interval seconds", `rate({app="api"}[{{__interval_s}}s])`, `rate({app="api"}[300s])`, nil},
		{"comment", "# {{ignored}}\n{app={{svc}}}", "# {{ignored}}\n{app=\"pay\\\"ments\"}", nil},
		{"single quote is not a string", `{app="api"} |= "x" # it's '{{svc}}'`, `{app="api"} |= "x" # it's '{{svc}}'`, nil},
		{"single quotes outside a comment", `{app='{{svc}}'}`, `{app='"pay\"ments"'}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builtins
			if tt.builtins != nil {
				b = *tt.builtins
			}
			got, err := template.LogQL.Substitute(tt.query, variables, b)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestLogQLDialectErrors(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	interval, _ := template.ParseInterval("5m")
	builtins := template.Builtins{From: &from, To: &to, Interval: interval, TimestampColumn: "_time"}
	variables := []template.Variable{{Name: "svc", Type: template.TypeString, Value: "payments"}}

	tests := []struct{ name, query, wantErr string }{
		{"from", `{app="api"} | ts > {{__from}}`, "not available in LogQL"},
		{"to", `{app="api"} | ts < {{__to}}`, "not available in LogQL"},
		{"time filter", `{app="api"} | {{__time_filter}}`, "not available in LogQL"},
		{"timezone", `{app="api"} | tz={{__timezone}}`, "not available in LogQL"},
		{"tz offset", `{app="api"} | tz={{__tz_offset}}`, "not available in LogQL"},
		{"undefined outside a string", `{app={{missing}}}`, "undefined variable"},
		{"macro inside a string", `{app="api"} | line_format "{{__interval}}"`, "cannot be used inside a string"},
		{"variable in a raw string", "{app=`{{svc}}`}", "raw-string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := template.LogQL.Substitute(tt.query, variables, builtins)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %q, %v; want error containing %q", got, err, tt.wantErr)
			}
		})
	}
}

// The other dialects keep failing on an undefined name inside a string.
func TestUndefinedNameInStringFailsOutsideLogQL(t *testing.T) {
	for _, dialect := range []template.Dialect{template.ClickHouseSQL, template.LogsQL} {
		if _, err := dialect.Substitute(`x = '{{__line__}}'`, nil, template.Builtins{}); err == nil {
			t.Fatalf("dialect %d: want an undefined variable error", dialect)
		}
	}
}
