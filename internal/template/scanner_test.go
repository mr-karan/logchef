package template_test

import (
	"testing"

	"github.com/mr-karan/logchef/internal/template"
)

func TestDialectScannerRegressions(t *testing.T) {
	variables := []template.Variable{{Name: "v", Type: template.TypeString, Value: "safe"}}
	tests := []struct {
		name, query, want string
		substitute        func(string, []template.Variable) (string, error)
	}{
		{"sql array function", `SELECT [[and(1,1)]]`, `SELECT [[and(1,1)]]`, template.SubstituteVariables},
		{"sql array or", `SELECT [[or(0,1)]]`, `SELECT [[or(0,1)]]`, template.SubstituteVariables},
		{"sql array left", `SELECT [[left('abc',2)]]`, `SELECT [[left('abc',2)]]`, template.SubstituteVariables},
		{"sql nested array", `SELECT [[1,2],[3]]`, `SELECT [[1,2],[3]]`, template.SubstituteVariables},
		{"sql array with variable", `SELECT [[{{v}}],[3]]`, `SELECT [['safe'],[3]]`, template.SubstituteVariables},
		{"sql where operand", `WHERE [[ host = {{v}} ]]`, `WHERE  host = 'safe' `, template.SubstituteVariables},
		{"sql select item", `SELECT 1 [[, {{v}} ]]`, `SELECT 1 , 'safe' `, template.SubstituteVariables},
		{"sql not in", `x [[ NOT IN ({{v}}) ]]`, `x  NOT IN ('safe') `, template.SubstituteVariables},
		{"sql block comment", `SELECT 1 [[ WHERE 0 /* {{ignored}} */ AND {{v}} = 'safe' ]]`, `SELECT 1  WHERE 0 /* {{ignored}} */ AND 'safe' = 'safe' `, template.SubstituteVariables},
		{"sql line comment", "SELECT 1 [[ WHERE 0 AND {{v}} = 'safe' -- {{ignored}}\n]]", "SELECT 1  WHERE 0 AND 'safe' = 'safe' -- {{ignored}}\n", template.SubstituteVariables},
		{"sql quoted brackets", `SELECT '[[{{v}}]]'`, `SELECT '[[safe]]'`, template.SubstituteVariables},
		{"sql quoted dependency only", `SELECT [[ '{{v}}' ]]`, `SELECT [[ 'safe' ]]`, template.SubstituteVariables},
		{"sql comment dependency only", "SELECT [[ 1 -- {{ignored}}\n]]", "SELECT [[ 1 -- {{ignored}}\n]]", template.SubstituteVariables},
		{"logs path", `path:/api/* level:={{v}}`, `path:/api/* level:="safe"`, template.SubstituteLogsQLVariables},
		{"logs dash", `service:my--svc level:={{v}}`, `service:my--svc level:="safe"`, template.SubstituteLogsQLVariables},
		{"logs dollar", `tag:$tag$ level:={{v}}`, `tag:$tag$ level:="safe"`, template.SubstituteLogsQLVariables},
		{"logs raw backslash", "* | format `raw\\` as a | format {{v}} as b", "* | format `raw\\` as a | format \"safe\" as b", template.SubstituteLogsQLVariables},
		{"logs raw optional", "* | format `raw\\` as a [[ | format {{v}} as b ]]", "* | format `raw\\` as a  | format \"safe\" as b ", template.SubstituteLogsQLVariables},
		{"logs filter block", `_time:5m [[ level:={{v}} ]]`, `_time:5m  level:="safe" `, template.SubstituteLogsQLVariables},
		{"logs comment", "* [[ level:={{v}} # {{ignored}}\n]]", "*  level:=\"safe\" # {{ignored}}\n", template.SubstituteLogsQLVariables},
		{"logchef dash", `service=my--svc [[ AND level={{v}} ]]`, `service=my--svc  AND level="safe" `, template.SubstituteLogchefQLVariables},
		{"logchef hash", `service=my#svc [[ AND level={{v}} ]]`, `service=my#svc  AND level="safe" `, template.SubstituteLogchefQLVariables},
		{"logchef slash", `path=/api/* [[ AND level={{v}} ]]`, `path=/api/*  AND level="safe" `, template.SubstituteLogchefQLVariables},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.substitute(tt.query, variables)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestOptionalBlocksWithNoValues(t *testing.T) {
	for _, substitute := range []func(string, []template.Variable) (string, error){template.SubstituteVariables, template.SubstituteLogsQLVariables, template.SubstituteLogchefQLVariables} {
		got, err := substitute(`* [[ level={{missing}} ]]`, nil)
		if err != nil || got != "* " {
			t.Fatalf("got %q, %v", got, err)
		}
	}
}
