package advisor

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

const rulePartitioning = "partitioning"

// dateType matches the types that toYYYYMM and toDate take.
var dateType = regexp.MustCompile(`^(Date|Date32|DateTime|DateTime64)(\(.*\))?$`)

// partitioningFindings is R7: a partition key for a large unpartitioned
// table (trigger A), or a coarser one for a table with too many partitions
// (trigger B). It proposes a key and no DDL.
func partitioningFindings(a *analysis) []finding {
	table := a.meta.Table
	if !slices.Contains(keyEngines, table.Engine) {
		return nil
	}
	ts, ok := a.column(a.ts)
	if !ok || !dateType.MatchString(ts.Type) {
		return nil
	}
	parts, _ := a.meta.Input(clickhouse.AdvisorInputParts)
	partsKnown := parts.State == models.AdvisorInputOK
	unpartitioned := table.PartitionKey == "" && table.TotalBytes != nil && *table.TotalBytes >= partitionMinBytes
	tooMany := partsKnown && (len(a.meta.Partitions) > partitionMaxCount || a.meta.PartitionsTruncated)
	if !unpartitioned && !tooMany {
		return nil
	}

	key := "toYYYYMM(" + displayIdentifier(ts.Name) + ")"
	var ttlEvidence []models.AdvisorEvidence
	if clause, ok := tableTTL(table.CreateQuery); ok {
		days, known := ttlDays(clause)
		if known && days < partitionDailyTTLDays {
			key = "toDate(" + displayIdentifier(ts.Name) + ")"
		}
		ttlEvidence = append(ttlEvidence, models.AdvisorEvidence{Label: "Table TTL", Value: clause, Source: clickhouse.AdvisorInputTables})
	}
	if key == table.PartitionKey {
		return nil
	}

	current, title, gain := table.PartitionKey, fmt.Sprintf("Use a coarser partition key than %s", table.PartitionKey), "Fewer files per query."
	if unpartitioned {
		current, title, gain = "none", fmt.Sprintf("Partition the table by %s", key), "TTL and manual cleanup drop whole parts."
	}
	evidence := []models.AdvisorEvidence{{Label: "Partition key", Value: current, Source: clickhouse.AdvisorInputTables}}
	if partsKnown {
		count := formatCount(int64(len(a.meta.Partitions)))
		if a.meta.PartitionsTruncated {
			count = "more than " + count
		}
		evidence = append(evidence, models.AdvisorEvidence{Label: "Active partitions", Value: count, Source: clickhouse.AdvisorInputParts})
	}
	evidence = append(evidence, a.tableSizeEvidence()...)
	evidence = append(evidence, ttlEvidence...)
	evidence = append(evidence, models.AdvisorEvidence{Label: "Proposed partition key", Value: key, Source: "logchef"})
	return []finding{{SchemaFinding: models.SchemaFinding{
		ID:       a.findingID(rulePartitioning, "", nil, ""),
		Rule:     rulePartitioning,
		Impact:   models.ImpactMedium,
		Title:    title,
		Evidence: evidence,
		DDL:      []string{},
		Rebuild:  true,
		Gain:     gain,
		Cost:     rebuildCost + " A new partition key also changes which rows a merge can combine.",
	}}}
}

// tableTTL returns the table's TTL clause from its create query. Column
// TTLs sit inside the column list, before the engine, and do not count.
func tableTTL(createQuery string) (string, bool) {
	engine := strings.Index(createQuery, ") ENGINE = ")
	if engine < 0 {
		return "", false
	}
	_, clause, ok := strings.Cut(createQuery[engine:], " TTL ")
	if !ok {
		return "", false
	}
	clause, _, _ = strings.Cut(clause, " SETTINGS ")
	return strings.TrimSpace(clause), true
}

var ttlInterval = regexp.MustCompile(`toInterval(Second|Minute|Hour|Day|Week|Month|Quarter|Year)\((\d+)\)`)

// ttlDays returns the longest interval in a TTL clause, in days. A clause can
// move data before it deletes it; the longest interval is the one that
// bounds the table's age. It is false when the clause names no interval.
func ttlDays(clause string) (float64, bool) {
	perDay := map[string]float64{
		"Second": 1.0 / 86400, "Minute": 1.0 / 1440, "Hour": 1.0 / 24, "Day": 1,
		"Week": 7, "Month": 30, "Quarter": 91, "Year": 365,
	}
	longest, found := 0.0, false
	for _, m := range ttlInterval.FindAllStringSubmatch(clause, -1) {
		n, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		longest, found = max(longest, n*perDay[m[1]]), true
	}
	return longest, found
}
