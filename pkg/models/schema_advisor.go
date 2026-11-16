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
