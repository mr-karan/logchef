package models

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
