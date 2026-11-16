package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Scheduled reports (RFC 0008). The types mirror the reports,
// report_recipients and report_runs tables one to one.

type (
	ReportID          int64
	ReportRecipientID int64
	ReportRunID       int64
)

type ReportSourceKind string

const (
	ReportSourceDashboard  ReportSourceKind = "dashboard"
	ReportSourceSavedQuery ReportSourceKind = "saved_query"
)

type ReportState string

const (
	ReportStateActive ReportState = "active"
	ReportStatePaused ReportState = "paused"
)

// ReportPauseReason is empty exactly when the report is active.
type ReportPauseReason string

const (
	ReportPauseNone          ReportPauseReason = ""
	ReportPauseUser          ReportPauseReason = "user"
	ReportPauseOwnerInactive ReportPauseReason = "owner_inactive"
	ReportPauseFailures      ReportPauseReason = "failures"
)

// ReportRangeAnchor is the "now" a report's time range resolves against:
// the send time, or the start of that day in the report's timezone.
type ReportRangeAnchor string

const (
	ReportAnchorSendTime ReportRangeAnchor = "send_time"
	ReportAnchorDayStart ReportRangeAnchor = "day_start"
)

type ReportRecipientKind string

const (
	ReportRecipientUser  ReportRecipientKind = "user"
	ReportRecipientTeam  ReportRecipientKind = "team"
	ReportRecipientEmail ReportRecipientKind = "email"
)

type ReportRunTrigger string

const (
	ReportTriggerSchedule ReportRunTrigger = "schedule"
	ReportTriggerManual   ReportRunTrigger = "manual"
)

type ReportRunStatus string

const (
	ReportRunRunning     ReportRunStatus = "running"
	ReportRunSent        ReportRunStatus = "sent"
	ReportRunPartial     ReportRunStatus = "partial"
	ReportRunSkipped     ReportRunStatus = "skipped"
	ReportRunFailed      ReportRunStatus = "failed"
	ReportRunInterrupted ReportRunStatus = "interrupted"
)

// ReportRunReason is the fixed code that explains a run's status.
type ReportRunReason string

const (
	ReportReasonNone ReportRunReason = ""

	ReportReasonPanelFailed     ReportRunReason = "panel_failed"
	ReportReasonPanelLocked     ReportRunReason = "panel_locked"
	ReportReasonDeliveryFailed  ReportRunReason = "delivery_failed"
	ReportReasonDeliveryUnknown ReportRunReason = "delivery_unknown"
	ReportReasonDeadline        ReportRunReason = "deadline"

	ReportReasonEmpty         ReportRunReason = "empty"
	ReportReasonOwnerInactive ReportRunReason = "owner_inactive"
	ReportReasonNoOwner       ReportRunReason = "no_owner"
	ReportReasonPaused        ReportRunReason = "paused"
	ReportReasonDisabled      ReportRunReason = "disabled"
	ReportReasonBusy          ReportRunReason = "busy"

	ReportReasonNoAccess            ReportRunReason = "no_access"
	ReportReasonAllPanelsFailed     ReportRunReason = "all_panels_failed"
	ReportReasonSMTPNotConfigured   ReportRunReason = "smtp_not_configured"
	ReportReasonAllDeliveriesFailed ReportRunReason = "all_deliveries_failed"
	ReportReasonSourceInvalid       ReportRunReason = "source_invalid"
	ReportReasonTimeout             ReportRunReason = "timeout"

	ReportReasonProcessStopped ReportRunReason = "process_stopped"
)

type ReportPanelStatus string

const (
	ReportPanelOK          ReportPanelStatus = "ok"
	ReportPanelEmpty       ReportPanelStatus = "empty"
	ReportPanelLocked      ReportPanelStatus = "locked"
	ReportPanelFailed      ReportPanelStatus = "failed"
	ReportPanelUnsupported ReportPanelStatus = "unsupported"
)

type ReportDeliveryClass string

const (
	ReportDeliveryUser     ReportDeliveryClass = "user"
	ReportDeliveryExternal ReportDeliveryClass = "external"
)

type ReportDeliveryStatus string

const (
	ReportDeliverySent    ReportDeliveryStatus = "sent"
	ReportDeliveryFailed  ReportDeliveryStatus = "failed"
	ReportDeliverySkipped ReportDeliveryStatus = "skipped"
	// ReportDeliveryUnknown means the SMTP server may have accepted the message.
	ReportDeliveryUnknown ReportDeliveryStatus = "unknown"
)

// ReportCooldownKind names the per-report rate limit a request takes.
type ReportCooldownKind string

const (
	ReportCooldownManualRun ReportCooldownKind = "manual_run"
	ReportCooldownPreview   ReportCooldownKind = "preview"
)

// ReportCooldown is the minimum gap between two manual runs, or two
// previews, of one report.
const ReportCooldown = 60 * time.Second

// ReportRunHistoryLimit is how many runs a report keeps.
const ReportRunHistoryLimit = 100

var (
	// ErrReportTickTaken means a scheduled run row for this report and tick
	// already exists: another replica or an earlier attempt holds the tick.
	ErrReportTickTaken = fmt.Errorf("%w: report tick already claimed", ErrConflict)
	// ErrReportRunInFlight means another run of this report is running.
	ErrReportRunInFlight = fmt.Errorf("%w: a run of this report is in flight", ErrConflict)
)

type ReportFrequency string

const (
	ReportHourly  ReportFrequency = "hourly"
	ReportDaily   ReportFrequency = "daily"
	ReportWeekly  ReportFrequency = "weekly"
	ReportMonthly ReportFrequency = "monthly"
)

// ReportSchedule is the whole schedule. A field that the frequency does not
// use must be absent, and Validate rejects it. No value expresses an
// interval below one hour.
type ReportSchedule struct {
	Frequency  ReportFrequency `json:"frequency"`
	Minute     int             `json:"minute"`                 // 0-59
	Hour       *int            `json:"hour,omitempty"`         // 0-23, not hourly
	Weekdays   []int           `json:"weekdays,omitempty"`     // 0-6, Sunday is 0, weekly only
	DayOfMonth *int            `json:"day_of_month,omitempty"` // 1-28, monthly only
}

// Validate checks the field rules of each frequency. Errors name the field.
func (s ReportSchedule) Validate() error {
	switch s.Frequency {
	case ReportHourly, ReportDaily, ReportWeekly, ReportMonthly:
	default:
		return fmt.Errorf("schedule.frequency must be hourly, daily, weekly or monthly, got %q", s.Frequency)
	}
	if s.Minute < 0 || s.Minute > 59 {
		return fmt.Errorf("schedule.minute must be 0 to 59, got %d", s.Minute)
	}
	if err := s.validateHour(); err != nil {
		return err
	}
	if err := s.validateWeekdays(); err != nil {
		return err
	}
	return s.validateDayOfMonth()
}

func (s ReportSchedule) validateHour() error {
	switch {
	case s.Frequency == ReportHourly && s.Hour != nil:
		return fmt.Errorf("schedule.hour is not allowed for an hourly schedule")
	case s.Frequency == ReportHourly:
		return nil
	case s.Hour == nil:
		return fmt.Errorf("schedule.hour is required for a %s schedule", s.Frequency)
	case *s.Hour < 0 || *s.Hour > 23:
		return fmt.Errorf("schedule.hour must be 0 to 23, got %d", *s.Hour)
	}
	return nil
}

func (s ReportSchedule) validateWeekdays() error {
	if s.Frequency != ReportWeekly {
		if s.Weekdays != nil {
			return fmt.Errorf("schedule.weekdays is only allowed for a weekly schedule")
		}
		return nil
	}
	if len(s.Weekdays) == 0 {
		return fmt.Errorf("schedule.weekdays is required for a weekly schedule")
	}
	var seen [7]bool
	for _, d := range s.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("schedule.weekdays must hold 0 to 6, Sunday is 0, got %d", d)
		}
		if seen[d] {
			return fmt.Errorf("schedule.weekdays repeats %d", d)
		}
		seen[d] = true
	}
	return nil
}

func (s ReportSchedule) validateDayOfMonth() error {
	if s.Frequency != ReportMonthly {
		if s.DayOfMonth != nil {
			return fmt.Errorf("schedule.day_of_month is only allowed for a monthly schedule")
		}
		return nil
	}
	if s.DayOfMonth == nil {
		return fmt.Errorf("schedule.day_of_month is required for a monthly schedule")
	}
	if *s.DayOfMonth < 1 || *s.DayOfMonth > 28 {
		return fmt.Errorf("schedule.day_of_month must be 1 to 28, got %d", *s.DayOfMonth)
	}
	return nil
}

// ParseReportSchedule decodes a schedule, refusing unknown fields, and
// validates it.
func ParseReportSchedule(raw []byte) (ReportSchedule, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s ReportSchedule
	if err := dec.Decode(&s); err != nil {
		return ReportSchedule{}, fmt.Errorf("schedule: %w", err)
	}
	if dec.More() {
		return ReportSchedule{}, fmt.Errorf("schedule: trailing data after the object")
	}
	if err := s.Validate(); err != nil {
		return ReportSchedule{}, err
	}
	return s, nil
}

// Report is what to send, when, and as whom. Exactly one of DashboardID and
// SavedQueryID is set, as SourceKind says. TabID and TimeRange are "" when
// the column is NULL; an empty TimeRange means the dashboard default.
type Report struct {
	ID                  ReportID          `json:"id"`
	Name                string            `json:"name"`
	OwnerID             *UserID           `json:"owner_id"`
	SourceKind          ReportSourceKind  `json:"source_kind"`
	DashboardID         *int              `json:"dashboard_id,omitempty"`
	TabID               string            `json:"tab_id,omitempty"`
	SavedQueryID        *int              `json:"saved_query_id,omitempty"`
	Schedule            ReportSchedule    `json:"schedule"`
	Timezone            string            `json:"timezone"`
	TimeRange           string            `json:"time_range,omitempty"`
	RangeAnchor         ReportRangeAnchor `json:"range_anchor"`
	Variables           map[string]any    `json:"variables"`
	MaxRows             int               `json:"max_rows"`
	SkipIfEmpty         bool              `json:"skip_if_empty"`
	AttachCSV           bool              `json:"attach_csv"`
	State               ReportState       `json:"state"`
	PauseReason         ReportPauseReason `json:"pause_reason"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	LastManualRunAt     *time.Time        `json:"last_manual_run_at,omitempty"`
	LastPreviewAt       *time.Time        `json:"last_preview_at,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// ReportRecipient is one user, team or address. The field that Kind names
// is set and the others are empty.
type ReportRecipient struct {
	ID        ReportRecipientID   `json:"id"`
	ReportID  ReportID            `json:"report_id"`
	Kind      ReportRecipientKind `json:"kind"`
	UserID    *UserID             `json:"user_id,omitempty"`
	TeamID    *TeamID             `json:"team_id,omitempty"`
	Email     string              `json:"email,omitempty"`
	AddedBy   *UserID             `json:"added_by,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
}

// ReportPanelOutcome is one panel of a run, stored in panels_json. Error
// holds query error text, which only the owner and admins read.
type ReportPanelOutcome struct {
	PanelID    string            `json:"panel_id"`
	Title      string            `json:"title"`
	Status     ReportPanelStatus `json:"status"`
	Rows       int               `json:"rows"`
	Truncation Truncation        `json:"truncation,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// ReportDelivery is one recipient of a run, stored in deliveries_json.
// Target is a user id or an address; Via is "direct", "team:<id>" or
// "email".
type ReportDelivery struct {
	Class        ReportDeliveryClass  `json:"class"`
	Target       string               `json:"target"`
	Via          string               `json:"via"`
	Status       ReportDeliveryStatus `json:"status"`
	Attempts     int                  `json:"attempts"`
	Reason       string               `json:"reason,omitempty"`
	LockedPanels []string             `json:"locked_panels,omitempty"`
}

// ReportRun is one attempt. A run row is the claim: only a running run has
// DeadlineAt, and only a finished one has FinishedAt.
type ReportRun struct {
	ID           ReportRunID          `json:"id"`
	ReportID     ReportID             `json:"report_id"`
	Trigger      ReportRunTrigger     `json:"trigger"`
	ScheduledFor time.Time            `json:"scheduled_for"`
	ActorID      *UserID              `json:"actor_id,omitempty"`
	Status       ReportRunStatus      `json:"status"`
	Reason       ReportRunReason      `json:"reason"`
	WindowStart  *time.Time           `json:"window_start,omitempty"`
	WindowEnd    *time.Time           `json:"window_end,omitempty"`
	Panels       []ReportPanelOutcome `json:"panels"`
	Deliveries   []ReportDelivery     `json:"deliveries"`
	StartedAt    time.Time            `json:"started_at"`
	DeadlineAt   *time.Time           `json:"deadline_at,omitempty"`
	FinishedAt   *time.Time           `json:"finished_at,omitempty"`
}

// ReportRunClaim starts a run. ScheduledFor is the tick for a scheduled run
// and the request time for a manual one. HandlerBudget sets deadline_at from
// the database clock.
type ReportRunClaim struct {
	ReportID      ReportID
	Trigger       ReportRunTrigger
	ScheduledFor  time.Time
	ActorID       *UserID
	HandlerBudget time.Duration
}

// ReportRunResult is what a finished run writes in one update.
type ReportRunResult struct {
	Status      ReportRunStatus
	Reason      ReportRunReason
	WindowStart *time.Time
	WindowEnd   *time.Time
	Panels      []ReportPanelOutcome
	Deliveries  []ReportDelivery
}

// Validate refuses a result that would leave the run in a state a finish
// cannot produce: running, or interrupted, which only cleanup writes.
func (r ReportRunResult) Validate() error {
	switch r.Status {
	case ReportRunSent, ReportRunPartial, ReportRunSkipped, ReportRunFailed:
		return nil
	default:
		return fmt.Errorf("report run result status %q is not a finished status", r.Status)
	}
}

// ColumnsJSON encodes Panels and Deliveries for panels_json and
// deliveries_json, writing [] for none.
func (r ReportRunResult) ColumnsJSON() (panels, deliveries string, err error) {
	p := r.Panels
	if p == nil {
		p = []ReportPanelOutcome{}
	}
	d := r.Deliveries
	if d == nil {
		d = []ReportDelivery{}
	}
	pb, err := json.Marshal(p)
	if err != nil {
		return "", "", fmt.Errorf("encoding report run panels: %w", err)
	}
	db, err := json.Marshal(d)
	if err != nil {
		return "", "", fmt.Errorf("encoding report run deliveries: %w", err)
	}
	return string(pb), string(db), nil
}
