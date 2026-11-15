package template_test

import (
	"testing"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/template"
)

func TestSubstituteLogchefQLVariables(t *testing.T) {
	for _, query := range []string{`p.tcode={{ code }} | p.order_number`, `p.tcode="{{code}}" | p.order_number`, `p.tcode='{{code}}' | p.order_number`} {
		t.Run(query, func(t *testing.T) {
			value := "500'\"\\\n or level=\"ERROR\""
			got, err := template.SubstituteLogchefQLVariables(query, []template.Variable{{Name: "code", Type: template.TypeText, Value: value}})
			if err != nil {
				t.Fatal(err)
			}
			result := logchefql.Translate(got, nil)
			if !result.Valid || len(result.Conditions) != 1 || result.Conditions[0].Value != value || result.SelectClause == "" {
				t.Fatalf("substitution changed query semantics: %q, %+v", got, result)
			}
		})
	}
	for _, tt := range []struct {
		name      string
		query     string
		variables []template.Variable
		want      string
		wantErr   bool
	}{
		{"number", `p.tcode={{code}}`, []template.Variable{{Name: "code", Type: template.TypeNumber, Value: "10023"}}, `p.tcode=10023`, false},
		{"date", `ts>{{date}}`, []template.Variable{{Name: "date", Type: template.TypeDate, Value: "2026-09-15"}}, `ts>"2026-09-15 00:00:00"`, false},
		{"embedded", `msg~"prefix {{code}} suffix"`, []template.Variable{{Name: "code", Type: template.TypeText, Value: "500"}}, `msg~"prefix 500 suffix"`, false},
		{"missing", `p.tcode={{code}}`, nil, "", true},
		{"empty", `p.tcode={{code}}`, []template.Variable{{Name: "code", Type: template.TypeText, Value: ""}}, "", true},
		{"invalid number", `p.tcode={{code}}`, []template.Variable{{Name: "code", Type: template.TypeNumber, Value: "1 or x=1"}}, "", true},
		{"array", `p.tcode={{code}}`, []template.Variable{{Name: "code", Type: template.TypeText, Value: []string{"a", "b"}}}, "", true},
		{"optional expression", `level="ERROR" [[p.tcode={{code}}]]`, nil, `level="ERROR" `, false},
		{"literal brackets", `msg="[[info]]"`, nil, `msg="[[info]]"`, false},
		{"optional", `level="ERROR" [[and p.tcode={{code}}]]`, nil, `level="ERROR" `, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := template.SubstituteLogchefQLVariables(tt.query, tt.variables)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %q, %v; want %q, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
