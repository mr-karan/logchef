package server

import (
	"testing"
	"time"
)

func TestParseLogchefQLTimeRange(t *testing.T) {
	t.Run("accepts legacy picker format", func(t *testing.T) {
		start, end, err := parseLogchefQLTimeRange("2026-04-08 00:00:00", "2026-04-08 01:00:00", "UTC")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := start.Format(time.RFC3339); got != "2026-04-08T00:00:00Z" {
			t.Fatalf("unexpected start time %q", got)
		}
		if got := end.Format(time.RFC3339); got != "2026-04-08T01:00:00Z" {
			t.Fatalf("unexpected end time %q", got)
		}
	})

	t.Run("accepts iso8601 timestamps", func(t *testing.T) {
		start, end, err := parseLogchefQLTimeRange("2026-04-08T00:00:00Z", "2026-04-08T01:00:00Z", "UTC")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := start.Format(time.RFC3339); got != "2026-04-08T00:00:00Z" {
			t.Fatalf("unexpected start time %q", got)
		}
		if got := end.Format(time.RFC3339); got != "2026-04-08T01:00:00Z" {
			t.Fatalf("unexpected end time %q", got)
		}
	})
}
