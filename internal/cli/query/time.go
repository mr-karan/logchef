// Package query provides query-related utilities for the LogChef CLI.
package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TimeRangeOptions specifies time range parsing options
type TimeRangeOptions struct {
	Since string // Relative time (e.g., "15m", "1h", "24h")
	From  string // Absolute start time (ISO8601)
	To    string // Absolute end time (ISO8601)
}

// ParseTimeRange parses time range options into start and end times
func ParseTimeRange(opts TimeRangeOptions) (start, end time.Time, err error) {
	now := time.Now()

	// Handle absolute times
	if opts.From != "" {
		start, err = parseTime(opts.From)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'from' time: %w", err)
		}
	}

	if opts.To != "" {
		end, err = parseTime(opts.To)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'to' time: %w", err)
		}
	} else {
		end = now
	}

	// Handle relative time (--since)
	if opts.From == "" && opts.Since != "" {
		duration, err := ParseDuration(opts.Since)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'since' duration: %w", err)
		}
		start = end.Add(-duration)
	}

	// Validate
	if start.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("start time is required (use --since or --from)")
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time must be before end time")
	}

	return start, end, nil
}

// parseTime parses various time formats
func parseTime(s string) (time.Time, error) {
	// Handle special values
	switch strings.ToLower(s) {
	case "now":
		return time.Now(), nil
	}

	// Try various formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	// Try parsing as local time
	for _, format := range formats {
		if t, err := time.ParseInLocation(format, s, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}

// durationRegex matches durations like "15m", "1h", "24h", "7d"
var durationRegex = regexp.MustCompile(`^(\d+)(s|m|h|d|w)$`)

// ParseDuration parses a duration string with support for days and weeks
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	// Try standard Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Try our extended format
	matches := durationRegex.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s (examples: 15m, 1h, 24h, 7d)", s)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := matches[2]

	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
}

// FormatDuration formats a duration in a human-readable way
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	weeks := days / 7
	return fmt.Sprintf("%dw", weeks)
}

// FormatTimeRange formats a time range for display
func FormatTimeRange(start, end time.Time) string {
	duration := end.Sub(start)
	if start.YearDay() == end.YearDay() && start.Year() == end.Year() {
		// Same day
		return fmt.Sprintf("%s - %s (%s)",
			start.Format("15:04:05"),
			end.Format("15:04:05"),
			FormatDuration(duration),
		)
	}
	return fmt.Sprintf("%s - %s (%s)",
		start.Format("2006-01-02 15:04"),
		end.Format("2006-01-02 15:04"),
		FormatDuration(duration),
	)
}
