package advisor

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleSortKey = "sort_key"

// keyEngines are the engines whose sorting key only orders rows. In the
// replacing, collapsing, summing and aggregating engines it also decides
// which rows merge, so R6 and R7 leave them alone.
var keyEngines = []string{"MergeTree", "ReplicatedMergeTree"}

// rebuildCost is the cost of a finding that needs a new table.
const rebuildCost = "A new table and a planned cutover. Rows that arrive during a copy stay in the old table unless ingestion pauses or a catch-up step moves them."

// keyLeader is a column that R6 would put first in the sorting key.
type keyLeader struct {
	name        string
	stat        *usageStat
	cardinality clickhouse.AdvisorCardinality
}

// sortKeyFindings is R6: a sorting key led by the low-cardinality columns
// that most queries match with = or IN. It proposes a key and no DDL.
func sortKeyFindings(a *analysis) []finding {
	table := a.meta.Table
	if !a.usageRules || !slices.Contains(keyEngines, table.Engine) || table.PrimaryKey != table.SortingKey ||
		a.q < sortKeyMinQueries || a.usage.Days < sortKeyMinDays {
		return nil
	}
	keys := clickhouse.ParseSortKeys(table.SortingKey)
	leaders := a.keyLeaders(keys)
	if len(leaders) == 0 {
		return nil
	}
	share := a.share(leaders[0].stat.queries)
	slices.SortFunc(leaders, func(x, y keyLeader) int {
		return cmp.Or(cmp.Compare(x.cardinality.Distinct, y.cardinality.Distinct), cmp.Compare(x.name, y.name))
	})
	if len(leaders) > sortKeyMaxColumns {
		leaders = leaders[:sortKeyMaxColumns]
	}

	var names, proposed []string
	var evidence []models.AdvisorEvidence
	for _, l := range leaders {
		names = append(names, l.name)
		proposed = append(proposed, displayIdentifier(l.name))
		lines := a.usageEvidence("Queries with = or IN", l.stat)
		lines = append(lines, a.sliceEvidence(l.name, l.cardinality)...)
		for _, line := range lines {
			line.Label = l.name + ": " + line.Label
			evidence = append(evidence, line)
		}
	}
	for _, key := range keys {
		if slices.Contains(names, key) {
			continue
		}
		// ParseSortKeys unquotes a bare column and keeps an expression as
		// written.
		if _, ok := a.columns[key]; ok {
			key = displayIdentifier(key)
		}
		proposed = append(proposed, key)
	}
	key := "(" + strings.Join(proposed, ", ") + ")"
	evidence = append(evidence, models.AdvisorEvidence{Label: "Current sorting key", Value: "(" + table.SortingKey + ")", Source: clickhouse.AdvisorInputTables})
	evidence = append(evidence, a.tableSizeEvidence()...)
	evidence = append(evidence, models.AdvisorEvidence{Label: "Proposed sorting key", Value: key, Source: "logchef"})
	return []finding{{share: share, SchemaFinding: models.SchemaFinding{
		ID:        a.findingID(ruleSortKey, "", nil, ""),
		Rule:      ruleSortKey,
		Impact:    impactFor(share),
		Title:     fmt.Sprintf("Lead the sorting key with %s", strings.Join(names, ", ")),
		Evidence:  evidence,
		DDL:       []string{},
		Rebuild:   true,
		Candidate: true,
		Gain:      fmt.Sprintf("Queries that filter on %s read only the matching key range.", strings.Join(names, ", ")),
		Cost:      rebuildCost + " Queries that filter on time alone read more granules per time range.",
	}}}
}

// keyLeaders returns the plain columns that at least half the queries match
// with = or IN, that the sample shows to hold few values and that do not
// already come before the timestamp in keys, most used first.
func (a *analysis) keyLeaders(keys []string) []keyLeader {
	tsAt := slices.Index(keys, a.ts)
	stats := a.groupUsage(func(item models.FilterItem) (string, bool) {
		plain := item.Access == "" && len(item.Path) == 0
		return item.Column, plain && (item.Kind == models.MatchEq || item.Kind == models.MatchIn)
	})
	var leaders []keyLeader
	for _, name := range mostUsed(stats) {
		stat := stats[name]
		share := a.share(stat.queries)
		if share < sortKeyMinShare {
			continue
		}
		if _, ok := a.column(name); !ok {
			continue
		}
		// A column ahead of the timestamp already narrows the key range.
		if at := slices.Index(keys, name); at >= 0 && (tsAt < 0 || at < tsAt) {
			continue
		}
		cardinality, ok := a.cardinality(name, share)
		if !ok || !cardinality.Saturated || cardinality.Distinct >= sortKeyMaxDistinct {
			continue
		}
		leaders = append(leaders, keyLeader{name, stat, cardinality})
	}
	return leaders
}

// displayIdentifier quotes a column name only when a bare name would not
// parse.
func displayIdentifier(name string) string {
	if bareIdentifier.MatchString(name) {
		return name
	}
	return queryescape.ClickHouseIdentifier(name)
}

// tableSizeEvidence describes the target table's size, and for a
// Distributed source that it covers one replica.
func (a *analysis) tableSizeEvidence() []models.AdvisorEvidence {
	table := a.meta.Table
	if table.TotalBytes == nil {
		return nil
	}
	value := formatBytes(*table.TotalBytes)
	if table.TotalRows != nil {
		value += fmt.Sprintf(", %s rows", formatCount(*table.TotalRows))
	}
	evidence := []models.AdvisorEvidence{{Label: "Table size", Value: value, Source: clickhouse.AdvisorInputTables}}
	if a.meta.Target.Kind == models.AdvisorTargetDistributedLocal {
		evidence = append(evidence, models.AdvisorEvidence{Label: "Sizes", Value: "cover the connected replica only", Source: clickhouse.AdvisorInputTables})
	}
	return evidence
}
