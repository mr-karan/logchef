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

func TestSubstituteVariablesMultiSelect(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		variables   []Variable
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "multi-select string array with IN clause",
			sql:  "SELECT * FROM logs WHERE host IN ({{hosts}})",
			variables: []Variable{
				{Name: "hosts", Type: TypeString, Value: []interface{}{"server-1", "server-2", "server-3"}},
			},
			want: "SELECT * FROM logs WHERE host IN ('server-1', 'server-2', 'server-3')",
		},
		{
			name: "multi-select number array",
			sql:  "SELECT * FROM logs WHERE status IN ({{codes}})",
			variables: []Variable{
				{Name: "codes", Type: TypeNumber, Value: []interface{}{float64(200), float64(201), float64(204)}},
			},
			want: "SELECT * FROM logs WHERE status IN (200, 201, 204)",
		},
		{
			name: "multi-select string array with escaping",
			sql:  "SELECT * FROM logs WHERE msg IN ({{messages}})",
			variables: []Variable{
				{Name: "messages", Type: TypeString, Value: []interface{}{"hello", "it's working", "test"}},
			},
			want: "SELECT * FROM logs WHERE msg IN ('hello', 'it''s working', 'test')",
		},
		{
			name: "multi-select with single value",
			sql:  "SELECT * FROM logs WHERE host IN ({{hosts}})",
			variables: []Variable{
				{Name: "hosts", Type: TypeString, Value: []interface{}{"server-1"}},
			},
			want: "SELECT * FROM logs WHERE host IN ('server-1')",
		},
		{
			name: "multi-select []string type",
			sql:  "SELECT * FROM logs WHERE env IN ({{envs}})",
			variables: []Variable{
				{Name: "envs", Type: TypeString, Value: []string{"prod", "staging"}},
			},
			want: "SELECT * FROM logs WHERE env IN ('prod', 'staging')",
		},
		{
			name: "empty array fails",
			sql:  "SELECT * FROM logs WHERE host IN ({{hosts}})",
			variables: []Variable{
				{Name: "hosts", Type: TypeString, Value: []interface{}{}},
			},
			wantErr:     true,
			errContains: "requires a value",
		},
		{
			name: "multi-select in optional clause - provided",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host IN ({{hosts}}) ]]",
			variables: []Variable{
				{Name: "hosts", Type: TypeString, Value: []interface{}{"server-1", "server-2"}},
			},
			want: "SELECT * FROM logs WHERE 1=1  AND host IN ('server-1', 'server-2') ",
		},
		{
			name:      "multi-select in optional clause - empty removes block",
			sql:       "SELECT * FROM logs WHERE 1=1 [[ AND host IN ({{hosts}}) ]]",
			variables: []Variable{},
			want:      "SELECT * FROM logs WHERE 1=1 ",
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

func TestProcessOptionalClauses(t *testing.T) {
	tests := []struct {
		name   string
		sql    string
		varMap map[string]Variable
		want   string
	}{
		{
			name:   "optional clause removed - variable missing",
			sql:    "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: map[string]Variable{},
			want:   "SELECT * FROM logs WHERE 1=1 ",
		},
		{
			name: "optional clause kept - variable has value",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: map[string]Variable{
				"hostname": {Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE 1=1  AND host = {{hostname}} ",
		},
		{
			name: "optional clause removed - variable is empty string",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: map[string]Variable{
				"hostname": {Name: "hostname", Type: TypeString, Value: ""},
			},
			want: "SELECT * FROM logs WHERE 1=1 ",
		},
		{
			name: "optional clause removed - variable is whitespace",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: map[string]Variable{
				"hostname": {Name: "hostname", Type: TypeString, Value: "   "},
			},
			want: "SELECT * FROM logs WHERE 1=1 ",
		},
		{
			name: "optional clause removed - variable is nil",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: map[string]Variable{
				"hostname": {Name: "hostname", Type: TypeString, Value: nil},
			},
			want: "SELECT * FROM logs WHERE 1=1 ",
		},
		{
			name: "optional clause kept - zero is valid value",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND status = {{code}} ]]",
			varMap: map[string]Variable{
				"code": {Name: "code", Type: TypeNumber, Value: float64(0)},
			},
			want: "SELECT * FROM logs WHERE 1=1  AND status = {{code}} ",
		},
		{
			name: "multiple optional clauses - mixed",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{host}} ]] [[ AND status = {{status}} ]]",
			varMap: map[string]Variable{
				"host": {Name: "host", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE 1=1  AND host = {{host}}  ",
		},
		{
			name: "multiple variables in one block - all provided",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{host}} AND status = {{status}} ]]",
			varMap: map[string]Variable{
				"host":   {Name: "host", Type: TypeString, Value: "server-1"},
				"status": {Name: "status", Type: TypeNumber, Value: float64(200)},
			},
			want: "SELECT * FROM logs WHERE 1=1  AND host = {{host}} AND status = {{status}} ",
		},
		{
			name: "multiple variables in one block - one missing removes block",
			sql:  "SELECT * FROM logs WHERE 1=1 [[ AND host = {{host}} AND status = {{status}} ]]",
			varMap: map[string]Variable{
				"host": {Name: "host", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE 1=1 ",
		},
		{
			name:   "no optional blocks",
			sql:    "SELECT * FROM logs WHERE host = {{hostname}}",
			varMap: map[string]Variable{},
			want:   "SELECT * FROM logs WHERE host = {{hostname}}",
		},
		{
			name:   "optional block with no variables - kept as-is",
			sql:    "SELECT * FROM logs WHERE 1=1 [[ AND active = true ]]",
			varMap: map[string]Variable{},
			want:   "SELECT * FROM logs WHERE 1=1  AND active = true ",
		},
		{
			name:   "nil varMap",
			sql:    "SELECT * FROM logs WHERE 1=1 [[ AND host = {{hostname}} ]]",
			varMap: nil,
			want:   "SELECT * FROM logs WHERE 1=1 ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProcessOptionalClauses(tt.sql, tt.varMap)
			if got != tt.want {
				t.Errorf("ProcessOptionalClauses() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstituteVariablesWithOptionalClauses(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		variables   []Variable
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "optional clause removed and required variable substituted",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}} [[ AND host = {{hostname}} ]]",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01"},
			},
			want: "SELECT * FROM logs WHERE timestamp > '2026-01-01 00:00:00' ",
		},
		{
			name: "optional clause kept and both substituted",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}} [[ AND host = {{hostname}} ]]",
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01"},
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			want: "SELECT * FROM logs WHERE timestamp > '2026-01-01 00:00:00'  AND host = 'server-1' ",
		},
		{
			name: "required variable outside optional block - error if missing",
			sql:  "SELECT * FROM logs WHERE timestamp > {{from_date}} [[ AND host = {{hostname}} ]]",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: "server-1"},
			},
			wantErr:     true,
			errContains: "undefined variable: {{from_date}}",
		},
		{
			name: "multiple optional clauses with complex query",
			sql: `SELECT * FROM logs 
				WHERE timestamp > {{from_date}}
				[[ AND host = {{host}} ]]
				[[ AND severity = {{severity}} ]]
				[[ AND service = {{service}} ]]
				ORDER BY timestamp DESC`,
			variables: []Variable{
				{Name: "from_date", Type: TypeDate, Value: "2026-01-01"},
				{Name: "host", Type: TypeString, Value: "prod-1"},
				{Name: "service", Type: TypeString, Value: "api"},
			},
			want: `SELECT * FROM logs 
				WHERE timestamp > '2026-01-01 00:00:00'
				 AND host = 'prod-1' 
				
				 AND service = 'api' 
				ORDER BY timestamp DESC`,
		},
		{
			name: "empty value for required variable outside optional - error",
			sql:  "SELECT * FROM logs WHERE host = {{hostname}} [[ AND status = {{status}} ]]",
			variables: []Variable{
				{Name: "hostname", Type: TypeString, Value: ""},
			},
			wantErr:     true,
			errContains: "requires a value",
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

func TestIsValueProvided(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{name: "nil", value: nil, want: false},
		{name: "empty string", value: "", want: false},
		{name: "whitespace string", value: "   ", want: false},
		{name: "non-empty string", value: "hello", want: true},
		{name: "zero int", value: 0, want: true},
		{name: "zero float64", value: float64(0), want: true},
		{name: "non-zero int", value: 42, want: true},
		{name: "non-zero float64", value: 3.14, want: true},
		{name: "negative number", value: -1, want: true},
		{name: "int64 zero", value: int64(0), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValueProvided(tt.value)
			if got != tt.want {
				t.Errorf("isValueProvided(%v) = %v, want %v", tt.value, got, tt.want)
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
