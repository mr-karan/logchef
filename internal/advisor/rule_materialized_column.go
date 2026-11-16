package advisor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleMaterializedColumn = "materialized_column"

// stringMapTypes are the Map types whose values the generated column reads
// as String, the type of m['k'].
var stringMapTypes = []string{"Map(String, String)", "Map(LowCardinality(String), String)"}

var bareIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// pathSubject is one Map key or JSON path of a column. Access is
// models.AccessMap or models.AccessJSONString: a LogchefQL nested field
// resolves to one of them by the column type.
type pathSubject struct {
	Column string   `json:"column"`
	Path   []string `json:"path"`
	Access string   `json:"access"`
}

// expression renders the subject, quoting the column when quote is true.
func (s pathSubject) expression(quote bool) string {
	column := s.Column
	if quote || !bareIdentifier.MatchString(column) {
		column = queryescape.ClickHouseIdentifier(column)
	}
	if s.Access == models.AccessMap {
		return column + "['" + queryescape.ClickHouseString(s.Path[0]) + "']"
	}
	args := []string{column}
	for _, segment := range s.Path {
		args = append(args, "'"+queryescape.ClickHouseString(segment)+"'")
	}
	return "JSONExtractString(" + strings.Join(args, ", ") + ")"
}

// materializedColumnFindings is R3: a materialized column for a Map key or a
// JSON path in a String column that many queries filter on.
func materializedColumnFindings(a *analysis) []finding {
	if !a.usageRules {
		return nil
	}
	subjects := map[string]pathSubject{}
	stats := a.groupUsage(func(item models.FilterItem) (string, bool) {
		subject, ok := a.pathSubject(item)
		if !ok {
			return "", false
		}
		key, err := json.Marshal(subject)
		if err != nil {
			panic(err) // strings and a string slice always encode
		}
		subjects[string(key)] = subject
		return string(key), true
	})
	var out []finding
	// Two subjects can map to one column name, such as the JSON paths 'a.b'
	// and 'a', 'b'; the more used one gets it.
	names := map[string]bool{}
	for _, key := range mostUsed(stats) {
		stat, subject := stats[key], subjects[key]
		share := a.share(stat.queries)
		if share < materializedMinShare || stat.queries < materializedMinQueries {
			continue
		}
		name := generatedName(append([]string{subject.Column}, subject.Path...)...)
		if names[name] || a.hasColumnFor(name, subject) {
			continue
		}
		names[name] = true
		column := a.columns[subject.Column]
		display, ident := subject.expression(false), queryescape.ClickHouseIdentifier(name)
		evidence := a.usageEvidence("Queries that filter on "+display, stat)
		evidence = append(evidence, models.AdvisorEvidence{Label: "Match kinds", Value: kindList(stat), Source: "logchef"})
		evidence = append(evidence, a.sizeEvidence(column)...)
		out = append(out, finding{share: share, SchemaFinding: models.SchemaFinding{
			ID:       a.findingID(ruleMaterializedColumn, subject.Column, subject.Path, subject.Access),
			Rule:     ruleMaterializedColumn,
			Impact:   impactFor(share),
			Title:    fmt.Sprintf("Add a materialized column for %s", display),
			Column:   subject.Column,
			Path:     subject.Path,
			Evidence: evidence,
			DDL: []string{
				fmt.Sprintf("%s ADD COLUMN IF NOT EXISTS %s String MATERIALIZED %s;", a.alterTable(), ident, subject.expression(true)),
				fmt.Sprintf("%s MATERIALIZE COLUMN %s;", a.alterTable(), ident),
			},
			Gain: fmt.Sprintf("A filter on %s reads one small column, not the whole %s. The new column can then take a bloom filter index.", name, subject.Column),
			Cost: "New parts store the value twice. The second statement is optional: MATERIALIZE COLUMN fills existing parts and is a mutation that rewrites the column in every part. " +
				fmt.Sprintf("Queries must name %s to gain; Logchef does not rewrite %s.", name, display),
		}})
	}
	return out
}

// pathSubject resolves a filter item to the Map key or JSON path it reads,
// when the column's type lets a String column hold it.
func (a *analysis) pathSubject(item models.FilterItem) (pathSubject, bool) {
	if len(item.Path) == 0 {
		return pathSubject{}, false
	}
	column, ok := a.column(item.Column)
	if !ok {
		return pathSubject{}, false
	}
	switch {
	case slices.Contains(stringMapTypes, column.Type) && (item.Access == models.AccessMap || item.Access == models.AccessNested):
		// The generator joins a nested field's segments into one key.
		return pathSubject{Column: item.Column, Path: []string{strings.Join(item.Path, ".")}, Access: models.AccessMap}, true
	case column.Type == "String" && (item.Access == models.AccessJSONString || item.Access == models.AccessNested):
		return pathSubject{Column: item.Column, Path: item.Path, Access: models.AccessJSONString}, true
	}
	return pathSubject{}, false
}

// hasColumnFor reports whether the table already has a column of the
// generated name, or one whose default expression is the subject.
func (a *analysis) hasColumnFor(name string, subject pathSubject) bool {
	if _, ok := a.columns[name]; ok {
		return true
	}
	quoted, bare := subject.expression(true), subject.expression(false)
	for _, column := range a.meta.Columns {
		if column.DefaultExpression == quoted || column.DefaultExpression == bare {
			return true
		}
	}
	return false
}

func kindList(stat *usageStat) string {
	kinds := make([]string, 0, len(stat.kinds))
	for kind := range stat.kinds {
		kinds = append(kinds, string(kind))
	}
	slices.Sort(kinds)
	return strings.Join(kinds, ", ")
}
