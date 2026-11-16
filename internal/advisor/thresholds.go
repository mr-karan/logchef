package advisor

// Thresholds of the rule set (RFC 0010). They are constants, not
// configuration.
const (
	// minUsageQueries is the smallest Q, the analyzed queries with a filter
	// item, at which usage rules run.
	minUsageQueries = 100
	// maxUnanalyzedShare stops usage rules when unanalyzed plus overflow
	// queries reach this share of all recorded queries.
	maxUnanalyzedShare = 0.20

	// Impact cut points for a share of queries or of table bytes.
	highShare   = 0.50
	mediumShare = 0.20

	// maxFindings bounds the findings in one report.
	maxFindings = 50

	// R1 skip_index_bloom.
	bloomMinShare    = 0.05
	bloomMinQueries  = 20
	bloomMinDistinct = 10_000

	// R3 materialized_column.
	materializedMinShare   = 0.10
	materializedMinQueries = 20
)
