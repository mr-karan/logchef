package server

import "testing"

func TestNormalizeExplicitExportFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "csv", input: "csv", want: "csv", wantOK: true},
		{name: "ndjson", input: "ndjson", want: "ndjson", wantOK: true},
		{name: "jsonl alias", input: "jsonl", want: "ndjson", wantOK: true},
		{name: "mixed case", input: "NdJsOn", want: "ndjson", wantOK: true},
		{name: "blank", input: "", want: "", wantOK: false},
		{name: "invalid", input: "xlsx", want: "", wantOK: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := normalizeExplicitExportFormat(tc.input)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("normalizeExplicitExportFormat(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestInferExportFormatFromAccept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		accept string
		want   string
	}{
		{name: "csv accept", accept: "text/csv", want: "csv"},
		{name: "ndjson accept", accept: "application/x-ndjson", want: "ndjson"},
		{name: "jsonl accept", accept: "application/jsonl", want: "ndjson"},
		{name: "wildcard defaults ndjson", accept: "*/*", want: "ndjson"},
		{name: "blank defaults ndjson", accept: "", want: "ndjson"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := inferExportFormatFromAccept(tc.accept); got != tc.want {
				t.Fatalf("inferExportFormatFromAccept(%q) = %q, want %q", tc.accept, got, tc.want)
			}
		})
	}
}
