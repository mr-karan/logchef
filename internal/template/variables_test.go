package template

import (
	"strings"
	"testing"
)

func TestSubstituteVariables(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		variables   []Variable
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "string variable",
			sql:  "SELECT * FROM logs WHERE host = {{hostname}}",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE host = 'server-1'",
		},
		{
			name: "text type (alias for string)",
			sql:  "SELECT * FROM logs WHERE host = {{hostname}}",
			variables: []Variable{
				{Name: "hostname", Type: TypeText, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE host = 'server-1'",
		},
		{
			name: "string with SQL injection attempt",
			sql:  "SELECT * FROM logs WHERE host = {{hostname}}",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "'; DROP TABLE logs; --"},
			},
			want: "SELECT * FROM logs WHERE host = '''; DROP TABLE logs; --'",
		},
		{
			name: "number variable - integer",
			sql:  "SELECT * FROM logs WHERE status = {{code}}",
			variables: []Variable{
				{Name: "code", Type: TypeNumber, Value: float64(500)},
			},
			want: "SELECT * FROM logs WHERE status = 500",
		},
		{
			name: "number variable - float",
			sql:  "SELECT * FROM logs WHERE latency > {{threshold}}",
			variables: []Variable{
				{Name: "threshold", Type: TypeNumber, Value: 0.5},
			},
			want: "SELECT * FROM logs WHERE latency > 0.5",
		},
		{
			name: "number variable - string input",
			sql:  "SELECT * FROM logs WHERE status = {{code}}",
			variables: []Variable{
				{Name: "code", Type: TypeNumber, Value: "500"},
			},
			want: "SELECT * FROM logs WHERE status = 500",
		},
		{
			name: "date variable - ISO format",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}}",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01T00:00:00Z"},
			},
			want: "SELECT * FROM logs WHERE timestamp > '2026-01-01 00:00:00'",
		},
		{
			name: "date variable - simple format",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}}",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01 12:30:45"},
			},
			want: "SELECT * FROM logs WHERE timestamp > '2026-01-01 12:30:45'",
		},
		{
			name: "date variable - date only",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}}",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01"},
			},
			want: "SELECT * FROM logs WHERE timestamp > '2026-01-01 00:00:00'",
		},
		{
			name: "multiple variables",
			sql:  "SELECT * FROM logs WHERE host = {{host}} AND status = {{status}}",
			variables: []Variable{
				{Name: "host", Type: TypeString, Value: "prod-1"},
				{Name: "status", Type: TypeNumber, Value: float64(200)},
			},
			want: "SELECT * FROM logs WHERE host = 'prod-1' AND status = 200",
		},
		{
			name: "variable used multiple times",
			sql:  "SELECT * FROM logs WHERE host = {{host}} OR origin = {{host}}",
			variables: []Variable{
				{Name: "host", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE host = 'server-1' OR origin = 'server-1'",
		},
		{
			name: "variable with whitespace in template",
			sql:  "SELECT * FROM logs WHERE host = {{ hostname }}",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE host = 'server-1'",
		},
		{
			name: "no variables in SQL",
			sql:  "SELECT * FROM logs WHERE host = 'server-1'",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE host = 'server-1'",
		},
		{
			name:      "empty variables",
			sql:       "SELECT * FROM logs WHERE host = 'server-1'",
			variables: []Variable{},
			want:      "SELECT * FROM logs WHERE host = 'server-1'",
		},
		{
			name: "CTE with variable",
			sql: `WITH filtered AS (
				SELECT * FROM logs WHERE timestamp > {{from_date}}
			)
			SELECT * FROM filtered`,
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01"},
			},
			want: `WITH filtered AS (
				SELECT * FROM logs WHERE timestamp > '2026-01-01 00:00:00'
			)
			SELECT * FROM filtered`,
		},
		// Error cases
		{
			name: "undefined variable",
			sql:  "SELECT * FROM logs WHERE host = {{undefined}}",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			wantErr:     true,
			errContains: "undefined variable",
		},
		{
			name: "invalid variable name - starts with number",
			sql:  "SELECT * FROM logs WHERE x = {{valid}}",
			variables: []Variable{
				{Name: "123invalid", Type: TypeString, Value: "test"},
			},
			wantErr:     true,
			errContains: "invalid variable name",
		},
		{
			name: "invalid variable name - contains special chars",
			sql:  "SELECT * FROM logs WHERE x = {{valid}}",
			variables: []Variable{
				{Name: "var-name", Type: TypeString, Value: "test"},
			},
			wantErr:     true,
			errContains: "invalid variable name",
		},
		{
			name: "invalid number value",
			sql:  "SELECT * FROM logs WHERE status = {{code}}",
			variables: []Variable{
				{Name: "code", Type: TypeNumber, Value: "not-a-number"},
			},
			wantErr:     true,
			errContains: "invalid number",
		},
		{
			name: "invalid date format",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}}",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "invalid-date"},
			},
			wantErr:     true,
			errContains: "invalid date format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SubstituteVariables(tt.sql, tt.variables)
			if (err != nil) != tt.wantErr {
				t.Errorf("SubstituteVariables() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("SubstituteVariables() error = %v, should contain %q", err, tt.errContains)
				}
				return
			}
			if got != tt.want {
				t.Errorf("SubstituteVariables() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractVariableNames(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "single variable",
			sql:  "SELECT * FROM logs WHERE host = {{hostname}}",
			want: []string{"hostname"},
		},
		{
			name: "multiple unique variables",
			sql:  "SELECT * FROM logs WHERE host = {{host}} AND status = {{status}}",
			want: []string{"host", "status"},
		},
		{
			name: "duplicate variables",
			sql:  "SELECT * FROM logs WHERE host = {{host}} OR origin = {{host}}",
			want: []string{"host"},
		},
		{
			name: "no variables",
			sql:  "SELECT * FROM logs",
			want: []string{},
		},
		{
			name: "variable with whitespace",
			sql:  "SELECT * FROM logs WHERE host = {{ hostname }}",
			want: []string{"hostname"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractVariableNames(tt.sql)
			if len(got) != len(tt.want) {
				t.Errorf("ExtractVariableNames() = %v, want %v", got, tt.want)
				return
			}
			for i, name := range got {
				if name != tt.want[i] {
					t.Errorf("ExtractVariableNames()[%d] = %q, want %q", i, name, tt.want[i])
				}
			}
		})
	}
}
