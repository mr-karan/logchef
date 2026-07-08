package victorialogs

import (
	"testing"
	"time"
)

// TestFormatTimezoneOffset asserts formatTimezoneOffset returns a
// VictoriaLogs-parseable duration string (not a clock-offset string like
// "+05:30") for the `offset` param on /select/logsql/hits, including
// half-hour and negative zones. Empirically verified against a running
// VictoriaLogs instance: offset=19800s (Asia/Kolkata, UTC+5:30) aligns
// buckets to IST midnight (00:00 IST == 18:30 UTC), and offset=-9000s
// (America/St_Johns, UTC-2:30 in July/NDT) aligns buckets to NDT midnight
// (00:00 NDT == 02:30 UTC).
func TestFormatTimezoneOffset(t *testing.T) {
	t.Parallel()

	reference := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		timezone string
		want     string
	}{
		{name: "UTC returns empty", timezone: "UTC", want: ""},
		{name: "empty timezone returns empty", timezone: "", want: ""},
		{name: "unknown timezone returns empty", timezone: "Not/AZone", want: ""},
		{name: "positive offset Asia/Kolkata (+05:30)", timezone: "Asia/Kolkata", want: "19800s"},
		{name: "half-hour negative offset America/St_Johns (-02:30 NDT in July)", timezone: "America/St_Johns", want: "-9000s"},
		{name: "quarter-hour offset Asia/Kathmandu (+05:45)", timezone: "Asia/Kathmandu", want: "20700s"},
		{name: "negative whole-hour offset America/New_York (-04:00 EDT in July)", timezone: "America/New_York", want: "-14400s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatTimezoneOffset(tt.timezone, &reference, nil)
			if got != tt.want {
				t.Fatalf("formatTimezoneOffset(%q) = %q, want %q", tt.timezone, got, tt.want)
			}
			if got != "" {
				if _, err := time.ParseDuration(got); err != nil {
					t.Fatalf("formatTimezoneOffset(%q) = %q is not a valid Go/VL duration: %v", tt.timezone, got, err)
				}
			}
		})
	}
}
