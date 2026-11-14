package models

import "testing"

func TestActivityScanBounds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rows, bytes int64
		invalid     bool
	}{
		{"maximum", 10_000_000_000, 1_099_511_627_776, false},
		{"rows above maximum", 10_000_000_001, 1, true},
		{"bytes above maximum", 1, 1_099_511_627_777, true},
		{"zero rows", 0, 1, true},
		{"negative bytes", 1, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &ClickHouseActivityConfig{MaxRowsToRead: &tc.rows, MaxBytesToRead: &tc.bytes}
			if err := cfg.Validate(); (err != nil) != tc.invalid {
				t.Fatalf("Validate() = %v, invalid = %t", err, tc.invalid)
			}
		})
	}
}
