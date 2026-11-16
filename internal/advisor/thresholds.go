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

	// R4 low_cardinality. The docs give 10,000 distinct values as the bound
	// for a gain; the rule stays well under it.
	lowCardinalityMinBytes    = 1 << 30
	lowCardinalityMinShare    = 0.01
	lowCardinalityMaxDistinct = 1_000

	// R5 codec, trigger A on the timestamp column and B on a String column.
	codecTimestampMinBytes = 1 << 30
	codecStringMinBytes    = 10 << 30
	codecStringMinShare    = 0.20
	codecStringMaxRatio    = 3

	// R6 sort_key.
	sortKeyMinQueries  = 200
	sortKeyMinDays     = 14
	sortKeyMinShare    = 0.50
	sortKeyMaxDistinct = 10_000
	sortKeyMaxColumns  = 3

	// R7 partitioning. The docs give about a thousand partitions as the
	// most a table should hold.
	partitionMinBytes     = 100 << 30
	partitionMaxCount     = 1_000
	partitionDailyTTLDays = 90

	// R8 retention_review.
	retentionMinAgeDays   = 90
	retentionMinDays      = 60
	retentionMinQueries   = 200
	retentionMaxNullShare = 0.05
)
