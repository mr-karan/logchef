package models

import "time"

// QueryHistoryPerUserCap bounds how many query-history rows are retained per
// user. On each successful record the store prunes anything beyond the newest
// cap for that user, so history stays bounded without a separate sweep.
const QueryHistoryPerUserCap = 200

// QueryHistoryDefaultLimit / QueryHistoryMaxLimit bound the list endpoint's
// page size.
const (
	QueryHistoryDefaultLimit = 50
	QueryHistoryMaxLimit     = QueryHistoryPerUserCap
)

// QueryHistory is one persisted record of a query a user executed against a
// source, captured on the preview execution paths. It survives across machines
// (unlike the old localStorage-only history) so it can back a server-side
// history panel and, later, the CLI/MCP.
type QueryHistory struct {
	ID            int64         `json:"id" db:"id"`
	UserID        UserID        `json:"user_id" db:"user_id"`
	TeamID        TeamID        `json:"team_id" db:"team_id"`
	SourceID      SourceID      `json:"source_id" db:"source_id"`
	QueryText     string        `json:"query_text" db:"query_text"`
	QueryLanguage QueryLanguage `json:"query_language" db:"query_language"`
	DurationMs    int64         `json:"duration_ms" db:"duration_ms"`
	RowCount      int64         `json:"row_count" db:"row_count"`
	CreatedAt     time.Time     `json:"created_at" db:"created_at"`
}

// QueryActivityRecord is one query_history row enriched with the executing
// user's email and the source's display name, used by the admin "recent query
// activity" view. SourceName is empty when the source row has since been
// deleted (the join is a LEFT JOIN and source_id carries no FK).
type QueryActivityRecord struct {
	ID            int64         `json:"id"`
	UserID        UserID        `json:"user_id"`
	UserEmail     string        `json:"user_email"`
	TeamID        TeamID        `json:"team_id"`
	SourceID      SourceID      `json:"source_id"`
	SourceName    string        `json:"source_name"`
	QueryText     string        `json:"query_text"`
	QueryLanguage QueryLanguage `json:"query_language"`
	DurationMs    int64         `json:"duration_ms"`
	RowCount      int64         `json:"row_count"`
	CreatedAt     time.Time     `json:"created_at"`
}
