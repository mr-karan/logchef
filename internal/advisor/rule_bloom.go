package advisor

import (
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleBloom = "skip_index_bloom"

// bloomFindings is R1: a bloom filter index for = and IN on a plain column
// with many distinct values.
func bloomFindings(a *analysis) []finding {
	if !a.usageRules || !a.indexesKnown() {
		return nil
	}
	sortKeys := clickhouse.ParseSortKeys(a.meta.Table.SortingKey)
	stats := a.groupUsage(func(item models.FilterItem) (string, bool) {
		plain := item.Access == "" && len(item.Path) == 0
		return item.Column, plain && (item.Kind == models.MatchEq || item.Kind == models.MatchIn)
	})
	var out []finding
	// Two columns can map to one index name; the more used one gets it.
	names := map[string]bool{}
	for _, name := range mostUsed(stats) {
		stat := stats[name]
		share := a.share(stat.queries)
		if share < bloomMinShare || stat.queries < bloomMinQueries {
			continue
		}
		column, ok := a.column(name)
		if !ok || len(sortKeys) > 0 && sortKeys[0] == name {
			continue
		}
		index := generatedName("idx", name, "bloom")
		if names[index] || a.hasIndex(name, index) {
			continue
		}
		cardinality, ok := a.cardinality(name, share)
		if !ok || cardinality.Distinct < bloomMinDistinct {
			continue
		}
		names[index] = true
		table, ident := a.qualifiedTarget(), queryescape.ClickHouseIdentifier(name)
		evidence := a.usageEvidence("Queries with = or IN", stat)
		evidence = append(evidence, models.AdvisorEvidence{Label: "Distinct values in the sample", Value: distinctValue(cardinality), Source: clickhouse.AdvisorInputSample})
		evidence = append(evidence, a.sizeEvidence(column)...)
		evidence = append(evidence, models.AdvisorEvidence{Label: "Existing skip indexes", Value: a.indexList(), Source: clickhouse.AdvisorInputIndexes})
		out = append(out, finding{share: share, SchemaFinding: models.SchemaFinding{
			ID:       a.findingID(ruleBloom, name, nil, ""),
			Rule:     ruleBloom,
			Impact:   impactFor(share),
			Title:    fmt.Sprintf("Add a bloom filter index on %s", name),
			Column:   name,
			Evidence: evidence,
			DDL: []string{
				fmt.Sprintf("%s ADD INDEX IF NOT EXISTS %s %s TYPE bloom_filter GRANULARITY 1;", a.alterTable(), queryescape.ClickHouseIdentifier(index), ident),
				fmt.Sprintf("%s MATERIALIZE INDEX %s;", a.alterTable(), queryescape.ClickHouseIdentifier(index)),
			},
			Gain:   "ClickHouse skips granules that cannot hold the value. Large for a needle such as a request id. None for != and NOT IN.",
			Cost:   "ADD INDEX changes metadata only. MATERIALIZE INDEX is a mutation over every part. Inserts and merges build the index from then on.",
			Verify: fmt.Sprintf("EXPLAIN indexes = 1 SELECT count() FROM %s WHERE %s = '<value>'", table, ident),
		}})
	}
	return out
}

// hasIndex reports whether a skip index covers exactly the column, or
// already has the name the finding would use.
func (a *analysis) hasIndex(column, name string) bool {
	for _, index := range a.meta.Indexes {
		if index.Name == name || isColumnExpr(index.Expr, column) {
			return true
		}
	}
	return false
}

// isColumnExpr reports whether a ClickHouse expression is the bare column.
func isColumnExpr(expr, column string) bool {
	expr = strings.TrimSpace(expr)
	return expr == column || expr == queryescape.ClickHouseIdentifier(column)
}

func (a *analysis) indexList() string {
	if len(a.meta.Indexes) == 0 {
		return "none"
	}
	names := make([]string, 0, len(a.meta.Indexes))
	for _, index := range a.meta.Indexes {
		names = append(names, fmt.Sprintf("%s (%s on %s)", index.Name, index.Type, index.Expr))
	}
	return strings.Join(names, ", ")
}

func distinctValue(c clickhouse.AdvisorCardinality) string {
	if c.Saturated {
		return formatCount(c.Distinct)
	}
	return "at least " + formatCount(c.Distinct)
}
