package models

import "time"

// JobDefinition is the part of a background job that its owner stores: what
// runs, on which schedule, and the missed-tick policy. internal/jobs gives the
// strings their meaning.
type JobDefinition struct {
	Kind         string
	Key          string
	ScheduleKind string // "interval" or "cron"
	ScheduleExpr string // seconds for "interval"
	Timezone     string // IANA name, used by "cron"
	Misfire      string // "run_once" or "skip"
	GraceSeconds int64
	Enabled      bool
}

// JobRunResult is what a finished run records. NextRunAt applies only while
// the job still has the schedule the run used, so a schedule change made
// during the run keeps the due time it set.
type JobRunResult struct {
	ScheduleKind string
	ScheduleExpr string
	Timezone     string
	NextRunAt    time.Time
	Status       string // "ok", "error" or "skipped"
	Error        string // "" stores NULL
}

// ScheduledJob is a stored background job with its schedule state and lease.
type ScheduledJob struct {
	JobDefinition
	ID             int64
	NextRunAt      time.Time
	LeaseOwner     string // "" when no process holds the claim
	LeaseToken     int64
	LeaseExpiresAt *time.Time
	// LastStartedAt is set by every claim to the database clock at the claim.
	LastStartedAt  *time.Time
	LastFinishedAt *time.Time
	LastStatus     string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
