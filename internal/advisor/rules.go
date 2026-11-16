package advisor

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

// rules is the registered rule set. A rule whose build gate fails in CI is
// left out of it.
var rules = []func(a *analysis) []finding{
	bloomFindings,
	materializedColumnFindings,
	lowCardinalityFindings,
	codecFindings,
}

// finding is a SchemaFinding with the share that ranks it.
type finding struct {
	models.SchemaFinding
	share float64
}

// analysis is the state one run of the rules shares.
type analysis struct {
	meta  *clickhouse.AdvisorMetadata
	usage Usage
	// ts is the source's timestamp column.
	ts string
	// q is Q, the analyzed queries with at least one filter item.
	q int64
	// usageRules is false when Q or the usage record is too small to trust.
	usageRules bool
	columns    map[string]clickhouse.AdvisorColumn
	// sample is nil until a usable sample exists; sampleSlices then holds
	// its slices, whose Distinct follows sampleOrder.
	sample       map[string]clickhouse.AdvisorCardinality
	sampleSlices []clickhouse.AdvisorSlice
	sampleOrder  map[string]int
	// sampled is false before the sample runs; rules then name the columns
	// whose cardinality they need.
	sampled    bool
	candidates map[string]float64
}

// Analyze runs the rule set on f. It is pure: the same facts give the same
// report.
func Analyze(f Facts) models.SchemaAdvisorReport {
	report := models.SchemaAdvisorReport{
		Status:            models.AdvisorStatusOK,
		ClickHouseVersion: f.Metadata.ServerVersion,
		Target:            f.Metadata.Target,
		Inputs:            slices.Clone(f.Metadata.Inputs),
		QueriesAnalyzed:   f.Usage.Queries - f.Usage.Unanalyzed - f.Usage.Overflow,
		QueriesUnanalyzed: f.Usage.Unanalyzed + f.Usage.Overflow,
		Findings:          []models.SchemaFinding{},
	}
	if f.Sample != nil {
		report.Inputs = append(report.Inputs, f.Sample.Input)
	}
	if reason := unavailable(f.Metadata); reason != "" {
		report.Status, report.Reason = models.AdvisorStatusUnavailable, reason
		return report
	}
	for _, input := range report.Inputs {
		if input.State != models.AdvisorInputOK && input.State != models.AdvisorInputSkipped {
			report.Status = models.AdvisorStatusPartial
		}
	}
	a := newAnalysis(f)
	report.Notes = a.usageNotes()

	var found []finding
	for _, rule := range rules {
		found = append(found, rule(a)...)
	}
	slices.SortFunc(found, func(x, y finding) int {
		return cmp.Or(
			cmp.Compare(impactRank(y.Impact), impactRank(x.Impact)),
			cmp.Compare(y.share, x.share),
			cmp.Compare(x.ID, y.ID),
		)
	})
	if len(found) > maxFindings {
		report.Notes = append(report.Notes, fmt.Sprintf("%d more findings not shown", len(found)-maxFindings))
		found = found[:maxFindings]
	}
	for i := range found {
		report.Findings = append(report.Findings, found[i].SchemaFinding)
	}
	return report
}

// SampleColumns returns the columns whose sampled cardinality would let a
// rule decide, most used first, at most clickhouse.AdvisorMaxSampleColumns.
// f.Sample is ignored.
func SampleColumns(f Facts) []string {
	if unavailable(f.Metadata) != "" {
		return nil
	}
	f.Sample = nil
	a := newAnalysis(f)
	for _, rule := range rules {
		rule(a)
	}
	columns := slices.SortedFunc(maps.Keys(a.candidates), func(x, y string) int {
		return cmp.Or(cmp.Compare(a.candidates[y], a.candidates[x]), cmp.Compare(x, y))
	})
	if len(columns) > clickhouse.AdvisorMaxSampleColumns {
		columns = columns[:clickhouse.AdvisorMaxSampleColumns]
	}
	return columns
}

// unavailable returns why no rule can run, or "".
func unavailable(meta *clickhouse.AdvisorMetadata) string {
	if meta.Unavailable != "" {
		return meta.Unavailable
	}
	for _, name := range []string{clickhouse.AdvisorInputTables, clickhouse.AdvisorInputColumns} {
		input, ok := meta.Input(name)
		if !ok {
			return name + " not read"
		}
		if input.State != models.AdvisorInputOK {
			return name + " " + string(input.State)
		}
	}
	return ""
}

func newAnalysis(f Facts) *analysis {
	a := &analysis{
		meta:       f.Metadata,
		usage:      f.Usage,
		ts:         f.TimestampColumn,
		columns:    map[string]clickhouse.AdvisorColumn{},
		sampled:    f.Sample != nil,
		candidates: map[string]float64{},
	}
	for _, column := range f.Metadata.Columns {
		a.columns[column.Name] = column
	}
	if f.Sample != nil && f.Sample.Input.State == models.AdvisorInputOK {
		a.sample = map[string]clickhouse.AdvisorCardinality{}
		a.sampleSlices = f.Sample.Slices
		a.sampleOrder = map[string]int{}
		for i, c := range f.Sample.Columns {
			a.sample[c.Column] = c
			a.sampleOrder[c.Column] = i
		}
	}
	// Q comes from the window's totals, not the listed shapes, so a
	// truncated list cannot shrink it. The queries with no filter are one
	// shape; when the truncation dropped it, Q is too large by its count,
	// which only lowers shares.
	unrecorded := f.Usage.Unanalyzed + f.Usage.Overflow
	a.q = f.Usage.Queries - unrecorded
	for _, shape := range f.Usage.Shapes {
		if len(shape.Shape) == 0 {
			a.q -= shape.Queries
		}
	}
	a.usageRules = a.q >= minUsageQueries &&
		(f.Usage.Queries == 0 || float64(unrecorded) < maxUnanalyzedShare*float64(f.Usage.Queries))
	return a
}

func (a *analysis) usageNotes() []string {
	var notes []string
	if a.usage.Truncated {
		notes = append(notes, fmt.Sprintf("usage truncated to the %s most common shapes", formatCount(int64(len(a.usage.Shapes)))))
	}
	if a.usage.Overflow > 0 {
		notes = append(notes, fmt.Sprintf("%s queries were not analyzed because their source passed its daily limit of distinct filter shapes", formatCount(a.usage.Overflow)))
	}
	unrecorded := a.usage.Unanalyzed + a.usage.Overflow
	if a.usage.Queries > 0 && float64(unrecorded) >= maxUnanalyzedShare*float64(a.usage.Queries) {
		notes = append(notes, "usage record is incomplete")
	}
	if a.q < minUsageQueries {
		notes = append(notes, "too few recorded queries for usage rules")
	}
	return notes
}

// column returns a target column that the source exposes under its name.
func (a *analysis) column(name string) (clickhouse.AdvisorColumn, bool) {
	column, ok := a.columns[name]
	return column, ok && a.meta.PassThrough.Has(name)
}

// cardinality returns the sampled cardinality of a column. Before the sample
// runs it records the column as a candidate, ranked by share.
func (a *analysis) cardinality(column string, share float64) (clickhouse.AdvisorCardinality, bool) {
	if !a.sampled {
		a.candidates[column] = max(a.candidates[column], share)
		return clickhouse.AdvisorCardinality{}, false
	}
	c, ok := a.sample[column]
	return c, ok
}

// indexesKnown reports whether the table's skip indexes were read.
func (a *analysis) indexesKnown() bool {
	input, ok := a.meta.Input(clickhouse.AdvisorInputIndexes)
	return ok && input.State == models.AdvisorInputOK
}

// usageStat is the usage of one subject: a column, or a path in it.
type usageStat struct {
	queries    int64
	durationMs int64
	kinds      map[models.MatchKind]struct{}
}

func (s *usageStat) meanDurationMs() int64 {
	if s.queries == 0 {
		return 0
	}
	return s.durationMs / s.queries
}

// groupUsage sums the queries whose shape holds an item that key maps to a
// subject. A query counts once per subject, whatever its items.
func (a *analysis) groupUsage(key func(models.FilterItem) (string, bool)) map[string]*usageStat {
	stats := map[string]*usageStat{}
	for _, shape := range a.usage.Shapes {
		seen := map[string]bool{}
		for _, item := range shape.Shape {
			subject, ok := key(item)
			if !ok {
				continue
			}
			stat := stats[subject]
			if stat == nil {
				stat = &usageStat{kinds: map[models.MatchKind]struct{}{}}
				stats[subject] = stat
			}
			stat.kinds[item.Kind] = struct{}{}
			if !seen[subject] {
				seen[subject] = true
				stat.queries += shape.Queries
				stat.durationMs += shape.DurationMs
			}
		}
	}
	return stats
}

// mostUsed returns the subjects of stats by query count desc, then by key.
func mostUsed(stats map[string]*usageStat) []string {
	return slices.SortedFunc(maps.Keys(stats), func(x, y string) int {
		return cmp.Or(cmp.Compare(stats[y].queries, stats[x].queries), cmp.Compare(x, y))
	})
}

// share returns queries as a share of Q.
func (a *analysis) share(queries int64) float64 {
	if a.q == 0 {
		return 0
	}
	return float64(queries) / float64(a.q)
}

// usageEvidence describes a subject's usage: its query count and share of Q,
// and the mean duration of those queries.
func (a *analysis) usageEvidence(label string, stat *usageStat) []models.AdvisorEvidence {
	return []models.AdvisorEvidence{
		{Label: label, Value: fmt.Sprintf("%s of %s (%s)", formatCount(stat.queries), formatCount(a.q), formatPercent(a.share(stat.queries))), Source: "logchef"},
		{Label: "Mean duration of those queries", Value: fmt.Sprintf("%s ms", formatCount(stat.meanDurationMs())), Source: "logchef"},
	}
}

// tableShare returns a column's compressed bytes as a share of the table's,
// and false when the table's size is unknown.
func (a *analysis) tableShare(column clickhouse.AdvisorColumn) (float64, bool) {
	total := a.meta.Table.TotalBytes
	if total == nil || *total == 0 {
		return 0, false
	}
	return float64(column.CompressedBytes) / float64(*total), true
}

// sizeEvidence describes a column's size, and for a Distributed source that
// the sizes cover one replica.
func (a *analysis) sizeEvidence(column clickhouse.AdvisorColumn) []models.AdvisorEvidence {
	value := fmt.Sprintf("%s compressed, %s uncompressed", formatBytes(column.CompressedBytes), formatBytes(column.UncompressedBytes))
	if share, ok := a.tableShare(column); ok {
		value += fmt.Sprintf(", %s of the table", formatPercent(share))
	}
	evidence := []models.AdvisorEvidence{{Label: "Size of " + column.Name, Value: value, Source: clickhouse.AdvisorInputColumns}}
	if a.meta.Target.Kind == models.AdvisorTargetDistributedLocal {
		evidence = append(evidence, models.AdvisorEvidence{Label: "Sizes", Value: "cover the connected replica only", Source: clickhouse.AdvisorInputColumns})
	}
	return evidence
}

// alterTable starts an ALTER of the target table, on its cluster when the
// source is a Distributed table.
func (a *analysis) alterTable() string {
	target := a.meta.Target
	statement := "ALTER TABLE " + queryescape.ClickHouseIdentifier(target.Database) + "." + queryescape.ClickHouseIdentifier(target.Table)
	if target.Kind == models.AdvisorTargetDistributedLocal && target.Cluster != "" {
		statement += " ON CLUSTER '" + queryescape.ClickHouseString(target.Cluster) + "'"
	}
	return statement
}

// qualifiedTarget is the target table as a query names it.
func (a *analysis) qualifiedTarget() string {
	return queryescape.ClickHouseIdentifier(a.meta.Target.Database) + "." + queryescape.ClickHouseIdentifier(a.meta.Target.Table)
}

// findingID is stable across refreshes: the hash of the rule, the target
// table, the column, the path and the access.
func (a *analysis) findingID(rule, column string, path []string, access string) string {
	key, err := json.Marshal([]any{rule, a.meta.Target.Database, a.meta.Target.Table, column, path, access})
	if err != nil {
		panic(err) // strings and a string slice always encode
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:8])
}

func impactFor(share float64) string {
	switch {
	case share >= highShare:
		return models.ImpactHigh
	case share >= mediumShare:
		return models.ImpactMedium
	}
	return models.ImpactLow
}

func impactRank(impact string) int {
	switch impact {
	case models.ImpactHigh:
		return 3
	case models.ImpactMedium:
		return 2
	case models.ImpactLow:
		return 1
	}
	return 0
}

// generatedName builds an index or column name from parts, with every
// character outside [A-Za-z0-9_] replaced by _.
func generatedName(parts ...string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.Join(parts, "_"))
}

func formatCount[T int64 | uint64](n T) string {
	digits := fmt.Sprint(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	b.WriteString(sign)
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

func formatPercent(share float64) string {
	return fmt.Sprintf("%.0f%%", share*100)
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), ""
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		value /= unit
		suffix = s
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}

// sliceEvidence describes a column's sample: the distinct count over all
// slices, and the rows and distinct count of each slice.
func (a *analysis) sliceEvidence(column string, cardinality clickhouse.AdvisorCardinality) []models.AdvisorEvidence {
	order := a.sampleOrder[column]
	var rows uint64
	lines := make([]string, len(a.sampleSlices))
	for i, slice := range a.sampleSlices {
		rows += slice.Rows
		lines[i] = fmt.Sprintf("%s: %s rows, %s distinct", slice.Start.UTC().Format("2006-01-02 15:04"), formatCount(slice.Rows), formatCount(slice.Distinct[order]))
	}
	return []models.AdvisorEvidence{
		{Label: "Distinct values in the sample", Value: fmt.Sprintf("%s in %s rows, from %d slices of the last 7 days", formatCount(cardinality.Distinct), formatCount(rows), len(a.sampleSlices)), Source: clickhouse.AdvisorInputSample},
		{Label: "Rows and distinct values per slice (UTC)", Value: strings.Join(lines, "; "), Source: clickhouse.AdvisorInputSample},
	}
}
