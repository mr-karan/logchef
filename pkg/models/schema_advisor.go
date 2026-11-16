package models

import "time"

// MatchKind is how a query compares one column or path.
type MatchKind string

const (
	MatchEq        MatchKind = "eq"
	MatchNeq       MatchKind = "neq"
	MatchIn        MatchKind = "in"
	MatchRange     MatchKind = "range"
	MatchNull      MatchKind = "null"
	MatchPrefix    MatchKind = "prefix"
	MatchLike      MatchKind = "like"
	MatchLikeToken MatchKind = "like_token"
	MatchILike     MatchKind = "ilike"
	MatchRegex     MatchKind = "regex"
	MatchToken     MatchKind = "token"
	MatchPosition  MatchKind = "position"
	MatchOther     MatchKind = "other"
)

// How a FilterItem reads its path. A plain column has no access.
const (
	AccessMap        = "map"         // m['k']
	AccessJSONString = "json_string" // JSONExtractString(col, 'a', 'b')
	AccessJSONOther  = "json_other"  // any other JSONExtract* function
	AccessSubcolumn  = "subcolumn"   // col.a.b in SQL
	AccessNested     = "nested"      // a LogchefQL dotted field, resolved against the column type later
)

// FilterItem is one column or path that a query filters on, and how. It
// never holds a value.
type FilterItem struct {
	Column string    `json:"column"`
	Path   []string  `json:"path,omitempty"` // ordered segments, never joined
	Access string    `json:"access,omitempty"`
	Kind   MatchKind `json:"kind"`
}

// FilterShape is the set of filter items of one query, sorted by column,
// path, access and kind, with no duplicates.
type FilterShape []FilterItem

// Report statuses.
const (
	AdvisorStatusOK          = "ok"
	AdvisorStatusPartial     = "partial"     // an input failed; the rules that need it did not run
	AdvisorStatusUnavailable = "unavailable" // no target, or its table or columns could not be read
)

// Finding impacts.
const (
	ImpactHigh   = "high"
	ImpactMedium = "medium"
	ImpactLow    = "low"
	ImpactInfo   = "info"
)

// AdvisorEvidence is one line of a finding's evidence, such as "Queries with
// = or IN", "412 of 1,900 (22%)", from "logchef".
type AdvisorEvidence struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// SchemaFinding is one piece of advice. Its text is English, as the CLI and
// MCP print it unchanged.
type SchemaFinding struct {
	// ID is stable across refreshes: 16 hex characters of a hash of the
	// rule, the target table, the column, the path and the access.
	ID        string            `json:"id"`
	Rule      string            `json:"rule"`
	Impact    string            `json:"impact"`
	Title     string            `json:"title"`
	Column    string            `json:"column,omitempty"`
	Path      []string          `json:"path,omitempty"`
	Evidence  []AdvisorEvidence `json:"evidence"`
	DDL       []string          `json:"ddl"`       // statements in the order to run
	Rebuild   bool              `json:"rebuild"`   // no in-place ALTER exists, DDL is empty
	Candidate bool              `json:"candidate"` // evidence is a sample, validate first
	Gain      string            `json:"gain"`
	Cost      string            `json:"cost"`
	Verify    string            `json:"verify,omitempty"` // a query to confirm the gain
}

// SchemaAdvisorReport is the result of one analysis of a source.
type SchemaAdvisorReport struct {
	Status            string          `json:"status"`
	Reason            string          `json:"reason,omitempty"`
	ClickHouseVersion string          `json:"clickhouse_version"`
	Target            *AdvisorTarget  `json:"target,omitempty"`
	Inputs            []AdvisorInput  `json:"inputs"`
	QueriesAnalyzed   int64           `json:"queries_analyzed"`
	QueriesUnanalyzed int64           `json:"queries_unanalyzed"`
	Findings          []SchemaFinding `json:"findings"`
	Notes             []string        `json:"notes,omitempty"`
}

// Shape hashes that stand for no single shape in query_shape_stats_daily.
const (
	ShapeHashUnanalyzed = "unanalyzed" // the query could not be analyzed
	ShapeHashOverflow   = "overflow"   // the process saw too many shapes for the source that day
)

// QueryShapeUsage is one shape's usage in query_shape_stats_daily: one day's
// row, or several days summed.
type QueryShapeUsage struct {
	QueryLanguage   QueryLanguage `json:"query_language"`
	ShapeHash       string        `json:"shape_hash"`
	Shape           FilterShape   `json:"shape"`
	QueryCount      int64         `json:"query_count"`
	TotalDurationMs int64         `json:"total_duration_ms"`
	TotalRows       int64         `json:"total_rows"`
	// MaxLookbackSeconds is the largest now - start_time of the counted
	// queries, nil when none had a start time.
	MaxLookbackSeconds *int64 `json:"max_lookback_s,omitempty"`
}

// QueryShapeIncrement adds usage to one source's shape on one UTC day.
type QueryShapeIncrement struct {
	BucketDate string // YYYY-MM-DD, UTC
	SourceID   SourceID
	Usage      QueryShapeUsage
}

// QueryShapeCoverage summarizes every rollup row of a source in a window, so
// the advisor can tell how complete the usage record is.
type QueryShapeCoverage struct {
	Days            int64 `json:"days"`
	QueryCount      int64 `json:"query_count"`
	UnanalyzedCount int64 `json:"unanalyzed_count"`
	OverflowCount   int64 `json:"overflow_count"`
	// NullLookbackCount counts queries in rows where no query had a start
	// time. A row mixing both kinds keeps its largest lookback, so this is a
	// lower bound.
	NullLookbackCount  int64  `json:"null_lookback_count"`
	MaxLookbackSeconds *int64 `json:"max_lookback_s,omitempty"`
}

// StoredSchemaAdvisorReport is the last report of one source. A refresh
// replaces it.
type StoredSchemaAdvisorReport struct {
	SourceID    SourceID
	GeneratedAt time.Time
	// GeneratedBy is nil for a scheduled run, or once the user is deleted.
	GeneratedBy *UserID
	WindowDays  int
	Report      SchemaAdvisorReport
}
