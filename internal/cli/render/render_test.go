package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderer_RenderJSON(t *testing.T) {
	result := &QueryResult{
		Logs: []map[string]any{
			{"timestamp": "2024-01-15T10:00:00Z", "level": "error", "message": "test error"},
			{"timestamp": "2024-01-15T10:01:00Z", "level": "info", "message": "test info"},
		},
		Columns: []Column{
			{Name: "timestamp", Type: "DateTime"},
			{Name: "level", Type: "String"},
			{Name: "message", Type: "String"},
		},
		Stats: QueryStats{
			ExecutionTimeMs: 150,
			RowsRead:        1000,
			BytesRead:       50000,
		},
	}

	renderer, err := New(Options{Format: "json", UsePager: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var buf bytes.Buffer
	err = renderer.renderJSON(&buf, result, true)
	if err != nil {
		t.Fatalf("renderJSON() error = %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("renderJSON() produced invalid JSON: %v", err)
	}

	// Verify structure
	if logs, ok := parsed["logs"].([]any); !ok || len(logs) != 2 {
		t.Errorf("renderJSON() logs count = %v, want 2", len(logs))
	}

	if stats, ok := parsed["stats"].(map[string]any); !ok {
		t.Error("renderJSON() missing stats")
	} else if stats["execution_time_ms"] != float64(150) {
		t.Errorf("renderJSON() stats.execution_time_ms = %v, want 150", stats["execution_time_ms"])
	}

	if count, ok := parsed["count"].(float64); !ok || count != 2 {
		t.Errorf("renderJSON() count = %v, want 2", count)
	}
}

func TestRenderer_RenderJSONL(t *testing.T) {
	result := &QueryResult{
		Logs: []map[string]any{
			{"level": "error", "message": "first"},
			{"level": "info", "message": "second"},
			{"level": "debug", "message": "third"},
		},
	}

	renderer, err := New(Options{Format: "jsonl", UsePager: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var buf bytes.Buffer
	err = renderer.renderJSONL(&buf, result)
	if err != nil {
		t.Fatalf("renderJSONL() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("renderJSONL() line count = %d, want 3", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("renderJSONL() line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestRenderer_RenderCSV(t *testing.T) {
	result := &QueryResult{
		Logs: []map[string]any{
			{"name": "Alice", "age": float64(30), "city": "NYC"},
			{"name": "Bob", "age": float64(25), "city": "LA"},
		},
		Columns: []Column{
			{Name: "name", Type: "String"},
			{Name: "age", Type: "Int32"},
			{Name: "city", Type: "String"},
		},
	}

	renderer, err := New(Options{Format: "csv", UsePager: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var buf bytes.Buffer
	err = renderer.renderCSV(&buf, result)
	if err != nil {
		t.Fatalf("renderCSV() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 { // header + 2 data rows
		t.Errorf("renderCSV() line count = %d, want 3", len(lines))
	}

	// First line should be headers
	headers := lines[0]
	if !strings.Contains(headers, "name") || !strings.Contains(headers, "age") || !strings.Contains(headers, "city") {
		t.Errorf("renderCSV() headers = %q, missing expected columns", headers)
	}
}

func TestRenderer_FilterFields(t *testing.T) {
	result := &QueryResult{
		Logs: []map[string]any{
			{"a": 1, "b": 2, "c": 3},
			{"a": 4, "b": 5, "c": 6},
		},
	}

	tests := []struct {
		name           string
		fields         []string
		expectedFields []string
	}{
		{
			name:           "no filter",
			fields:         nil,
			expectedFields: []string{"a", "b", "c"},
		},
		{
			name:           "single field",
			fields:         []string{"a"},
			expectedFields: []string{"a"},
		},
		{
			name:           "multiple fields",
			fields:         []string{"a", "c"},
			expectedFields: []string{"a", "c"},
		},
		{
			name:           "non-existent field",
			fields:         []string{"x"},
			expectedFields: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer, _ := New(Options{Fields: tt.fields, UsePager: false})
			filtered := renderer.filterFields(result.Logs)

			if len(filtered) != 2 {
				t.Errorf("filterFields() row count = %d, want 2", len(filtered))
				return
			}

			if tt.fields == nil {
				// No filter, should have all original fields
				if len(filtered[0]) != 3 {
					t.Errorf("filterFields() field count = %d, want 3", len(filtered[0]))
				}
			} else {
				// Should only have specified fields
				for _, row := range filtered {
					for k := range row {
						found := false
						for _, f := range tt.fields {
							if k == f {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("filterFields() unexpected field %q", k)
						}
					}
				}
			}
		})
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{name: "string", input: "hello", expected: "hello"},
		{name: "integer float", input: float64(42), expected: "42"},
		{name: "decimal float", input: 3.14159, expected: "3.14"},
		{name: "nil", input: nil, expected: ""},
		{name: "bool true", input: true, expected: "true"},
		{name: "bool false", input: false, expected: "false"},
		{name: "long string truncated", input: strings.Repeat("a", 100), expected: strings.Repeat("a", 77) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.input)
			if got != tt.expected {
				t.Errorf("formatValue(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{1234567890, "1,234,567,890"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatNumber(tt.input)
			if got != tt.expected {
				t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.expected {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRenderer_RenderTable_EmptyResult(t *testing.T) {
	result := &QueryResult{
		Logs:    []map[string]any{},
		Columns: []Column{},
		Stats: QueryStats{
			ExecutionTimeMs: 50,
			RowsRead:        0,
			BytesRead:       0,
		},
	}

	renderer, err := New(Options{Format: "table", UsePager: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var buf bytes.Buffer
	err = renderer.renderTable(&buf, result)
	if err != nil {
		t.Fatalf("renderTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No results found") {
		t.Errorf("renderTable() empty result should contain 'No results found', got %q", output)
	}
}

func TestRenderer_Template(t *testing.T) {
	result := &QueryResult{
		Logs: []map[string]any{
			{"name": "alice", "count": float64(10)},
			{"name": "bob", "count": float64(20)},
		},
	}

	tests := []struct {
		name     string
		template string
		contains []string
	}{
		{
			name:     "simple field",
			template: "{{.name}}",
			contains: []string{"alice", "bob"},
		},
		{
			name:     "upper function",
			template: "{{.name | upper}}",
			contains: []string{"ALICE", "BOB"},
		},
		{
			name:     "multiple fields",
			template: "{{.name}}: {{.count}}",
			contains: []string{"alice: 10", "bob: 20"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer, err := New(Options{Format: "template", Template: tt.template, UsePager: false})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			var buf bytes.Buffer
			err = renderer.renderTemplate(&buf, result)
			if err != nil {
				t.Fatalf("renderTemplate() error = %v", err)
			}

			output := buf.String()
			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("renderTemplate() output should contain %q, got %q", expected, output)
				}
			}
		})
	}
}
