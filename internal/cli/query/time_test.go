package query

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Standard Go durations
		{name: "seconds", input: "30s", expected: 30 * time.Second},
		{name: "minutes", input: "5m", expected: 5 * time.Minute},
		{name: "hours", input: "2h", expected: 2 * time.Hour},
		{name: "mixed", input: "1h30m", expected: 90 * time.Minute},

		// Extended format (days, weeks)
		{name: "days", input: "7d", expected: 7 * 24 * time.Hour},
		{name: "weeks", input: "2w", expected: 14 * 24 * time.Hour},
		{name: "one day", input: "1d", expected: 24 * time.Hour},

		// Case insensitive
		{name: "uppercase", input: "15M", expected: 15 * time.Minute},
		{name: "mixed case", input: "1H", expected: time.Hour},

		// Edge cases
		{name: "zero", input: "0s", expected: 0},
		{name: "large number", input: "365d", expected: 365 * 24 * time.Hour},

		// Invalid inputs
		{name: "invalid unit", input: "5x", wantErr: true},
		{name: "no number", input: "m", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "negative", input: "-5m", wantErr: true},
		{name: "decimal", input: "1.5h", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseDuration(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseDuration(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.expected {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseTimeRange(t *testing.T) {
	// Fixed time for consistent testing
	now := time.Now()

	tests := []struct {
		name        string
		opts        TimeRangeOptions
		wantErr     bool
		validateFn  func(start, end time.Time) bool
		errContains string
	}{
		{
			name: "since only",
			opts: TimeRangeOptions{Since: "1h"},
			validateFn: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 59*time.Minute && diff <= 61*time.Minute
			},
		},
		{
			name: "since 15m",
			opts: TimeRangeOptions{Since: "15m"},
			validateFn: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 14*time.Minute && diff <= 16*time.Minute
			},
		},
		{
			name: "since 7d",
			opts: TimeRangeOptions{Since: "7d"},
			validateFn: func(start, end time.Time) bool {
				diff := end.Sub(start)
				expected := 7 * 24 * time.Hour
				return diff >= expected-time.Minute && diff <= expected+time.Minute
			},
		},
		{
			name: "from and to absolute",
			opts: TimeRangeOptions{
				From: "2024-01-01T00:00:00Z",
				To:   "2024-01-01T12:00:00Z",
			},
			validateFn: func(start, end time.Time) bool {
				return start.Year() == 2024 && start.Month() == 1 && start.Day() == 1 &&
					start.Hour() == 0 && end.Hour() == 12
			},
		},
		{
			name: "from only (to defaults to now)",
			opts: TimeRangeOptions{
				From: now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
			validateFn: func(start, end time.Time) bool {
				return end.Sub(start) >= 119*time.Minute && end.Sub(start) <= 121*time.Minute
			},
		},
		{
			name:        "invalid since format",
			opts:        TimeRangeOptions{Since: "invalid"},
			wantErr:     true,
			errContains: "invalid 'since' duration",
		},
		{
			name:        "invalid from format",
			opts:        TimeRangeOptions{From: "not-a-date"},
			wantErr:     true,
			errContains: "invalid 'from' time",
		},
		{
			name: "start after end",
			opts: TimeRangeOptions{
				From: "2024-01-02T00:00:00Z",
				To:   "2024-01-01T00:00:00Z",
			},
			wantErr:     true,
			errContains: "start time must be before end time",
		},
		{
			name:        "missing time range",
			opts:        TimeRangeOptions{},
			wantErr:     true,
			errContains: "start time is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := ParseTimeRange(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTimeRange() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("ParseTimeRange() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseTimeRange() unexpected error: %v", err)
				return
			}
			if tt.validateFn != nil && !tt.validateFn(start, end) {
				t.Errorf("ParseTimeRange() validation failed: start=%v, end=%v", start, end)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "90m"},
		{2 * time.Hour, "2h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
		{7 * 24 * time.Hour, "1w"},
		{14 * 24 * time.Hour, "2w"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := FormatDuration(tt.input)
			if got != tt.expected {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatTimeRange(t *testing.T) {
	// Test same day formatting
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	result := FormatTimeRange(start, end)
	if !contains(result, "10:00:00") || !contains(result, "12:00:00") || !contains(result, "2h") {
		t.Errorf("FormatTimeRange() same day = %q, expected time range with 2h duration", result)
	}

	// Test different day formatting
	start2 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	end2 := time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC)

	result2 := FormatTimeRange(start2, end2)
	if !contains(result2, "2024-01-15") || !contains(result2, "2024-01-16") || !contains(result2, "1d") {
		t.Errorf("FormatTimeRange() different day = %q, expected date range with 1d duration", result2)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
