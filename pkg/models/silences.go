package models

import "time"

// AlertSilenceID identifies a silence.
type AlertSilenceID int64

// SilenceScheduleKind says when a silence applies.
type SilenceScheduleKind string

const (
	// SilenceOnce applies from StartsAt until EndsAt.
	SilenceOnce SilenceScheduleKind = "once"
	// SilenceWeekly applies in the windows of Weekly, until EndsAt when set.
	// A maintenance window is a weekly silence.
	SilenceWeekly SilenceScheduleKind = "weekly"
)

// Silence limits (RFC 0006, "Limits and failure modes").
const (
	MaxSilencesPerSource    = 100
	MaxSilenceMatchers      = 5
	MaxSilenceOnceDuration  = 30 * 24 * time.Hour
	MaxSilenceWindowMinutes = 24 * 60
	MaxSilenceCommentChars  = 1000
	MaxSilenceMatcherChars  = 256
)

// SilenceWeeklyWindow is the weekly window of a silence: it starts at Start, local
// time in Timezone, on each of Weekdays (0 is Sunday), and lasts
// DurationMinutes, so a window can cross midnight.
type SilenceWeeklyWindow struct {
	Weekdays        []int  `json:"weekdays"`
	Start           string `json:"start"`
	DurationMinutes int    `json:"duration_minutes"`
	Timezone        string `json:"timezone"`
}

// AlertSilence mutes the notifications of a source's rules, or of one rule,
// for the instances whose labels equal every matcher. A silenced instance
// still changes state and writes history.
type AlertSilence struct {
	ID       AlertSilenceID `json:"id"`
	SourceID SourceID       `json:"source_id"`
	// AlertID limits the silence to one rule. Nil covers every rule of the source.
	AlertID      *AlertID             `json:"alert_id,omitempty"`
	Matchers     map[string]string    `json:"matchers"`
	Comment      string               `json:"comment"`
	ScheduleKind SilenceScheduleKind  `json:"schedule_kind"`
	StartsAt     *time.Time           `json:"starts_at,omitempty"`
	EndsAt       *time.Time           `json:"ends_at,omitempty"`
	Weekly       *SilenceWeeklyWindow `json:"weekly,omitempty"`
	CreatedBy    *UserID              `json:"created_by,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	// Active is computed by the list route.
	Active bool `json:"active"`
}

// AlertSilenceRequest creates or replaces a silence. An update keeps the
// silence's source.
type AlertSilenceRequest struct {
	SourceID     SourceID             `json:"source_id"`
	AlertID      *AlertID             `json:"alert_id"`
	Matchers     map[string]string    `json:"matchers"`
	Comment      string               `json:"comment"`
	ScheduleKind SilenceScheduleKind  `json:"schedule_kind"`
	StartsAt     *time.Time           `json:"starts_at"`
	EndsAt       *time.Time           `json:"ends_at"`
	Weekly       *SilenceWeeklyWindow `json:"weekly"`
}
