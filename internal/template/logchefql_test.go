package template

import (
	"testing"

	"github.com/mr-karan/logchef/internal/logchefql"
)

func TestSubstituteLogchefQLVariables(t *testing.T) {
	for _, query := range []string{`p.tcode={{ code }} | p.order_number`, `p.tcode="{{code}}" | p.order_number`, `p.tcode='{{code}}' | p.order_number`} {
		t.Run(query, func(t *testing.T) {
			value := "500'\"\\\n or level=\"ERROR\""
			got, err := SubstituteLogchefQLVariables(query, []Variable{{Name: "code", Type: TypeText, Value: value}})
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
		variables []Variable
		want      string
		wantErr   bool
	}{
		{"number", `p.tcode={{code}}`, []Variable{{Name: "code", Type: TypeNumber, Value: "10023"}}, `p.tcode=10023`, false},
		{"date", `ts>{{date}}`, []Variable{{Name: "date", Type: TypeDate, Value: "2026-09-15"}}, `ts>"2026-09-15 00:00:00"`, false},
		{"embedded", `msg~"prefix {{code}} suffix"`, []Variable{{Name: "code", Type: TypeText, Value: "500"}}, `msg~"prefix 500 suffix"`, false},
		{"missing", `p.tcode={{code}}`, nil, "", true},
		{"empty", `p.tcode={{code}}`, []Variable{{Name: "code", Type: TypeText, Value: ""}}, "", true},
		{"invalid number", `p.tcode={{code}}`, []Variable{{Name: "code", Type: TypeNumber, Value: "1 or x=1"}}, "", true},
		{"array", `p.tcode={{code}}`, []Variable{{Name: "code", Type: TypeText, Value: []string{"a", "b"}}}, "", true},
		{"optional", `level="ERROR" [[and p.tcode={{code}}]]`, nil, `level="ERROR" `, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SubstituteLogchefQLVariables(tt.query, tt.variables)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %q, %v; want %q, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
