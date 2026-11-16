package advisor

import (
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleLowCardinality = "low_cardinality"

// lowCardinalityTypes maps each column type R4 changes to its new type.
var lowCardinalityTypes = map[string]string{
	"String":           "LowCardinality(String)",
	"Nullable(String)": "LowCardinality(Nullable(String))",
}

// lowCardinalityFindings is R4: LowCardinality for a large String column
// that the sample shows to hold few values.
func lowCardinalityFindings(a *analysis) []finding {
	// ClickHouse before 26.8 refuses to change the type of a column that a
	// skip index reads, so unread indexes leave every column in doubt.
	if !a.indexesKnown() || a.meta.IndexesTruncated {
		return nil
	}
	var tableUncompressed uint64
	for _, column := range a.meta.Columns {
		tableUncompressed += column.UncompressedBytes
	}
	var out []finding
	for _, column := range a.meta.Columns {
		newType, ok := lowCardinalityTypes[column.Type]
		if !ok || column.InSortingKey || column.InPrimaryKey || column.InPartitionKey {
			continue
		}
		if _, ok := a.column(column.Name); !ok {
			continue
		}
		// A truncated column list makes the table's size a lower bound, so
		// only the absolute floor holds.
		large := column.UncompressedBytes >= lowCardinalityMinBytes ||
			!a.meta.ColumnsTruncated && float64(column.UncompressedBytes) >= lowCardinalityMinShare*float64(tableUncompressed)
		if !large || a.typeChangeRefused(column.Name) {
			continue
		}
		share, _ := a.tableShare(column)
		cardinality, ok := a.cardinality(column.Name, share)
		if !ok || !cardinality.Saturated || cardinality.Distinct >= lowCardinalityMaxDistinct {
			continue
		}
		ident := queryescape.ClickHouseIdentifier(column.Name)
		evidence := a.sliceEvidence(column.Name, cardinality)
		evidence = append(evidence, a.sizeEvidence(column)...)
		out = append(out, finding{share: share, SchemaFinding: models.SchemaFinding{
			ID:        a.findingID(ruleLowCardinality, column.Name, nil, ""),
			Rule:      ruleLowCardinality,
			Impact:    impactFor(share),
			Title:     fmt.Sprintf("Store %s as %s", column.Name, newType),
			Column:    column.Name,
			Evidence:  evidence,
			DDL:       []string{fmt.Sprintf("%s MODIFY COLUMN %s %s;", a.alterTable(), ident, newType)},
			Candidate: true,
			Gain:      "Dictionary encoding. Smaller column, faster = and GROUP BY.",
			Cost:      "The type change is a mutation that rewrites the column in every part. Clients that read the column type see the new type.",
			Verify:    fmt.Sprintf("SELECT uniq(%s) FROM %s", ident, a.qualifiedTarget()),
		}})
	}
	return out
}

// typeChangeRefused reports whether ClickHouse may refuse to change the
// column's type because a skip index or a projection reads it. ClickHouse
// before 26.8 refuses it for a skip index, and 24.1 and later for a
// projection. system.tables lists projections only in the create query.
func (a *analysis) typeChangeRefused(column string) bool {
	for _, index := range a.meta.Indexes {
		if mentions(index.Expr, column) {
			return true
		}
	}
	if i := strings.Index(a.meta.Table.CreateQuery, "PROJECTION "); i >= 0 {
		// The rest of the query also holds the engine clauses; a match
		// there only withholds a finding.
		return mentions(a.meta.Table.CreateQuery[i:], column)
	}
	return false
}

// mentions reports whether a ClickHouse expression names the column, bare or
// quoted. It skips string literals. A name that matches for another reason,
// such as a function of the same name, only withholds a finding.
func mentions(expr, column string) bool {
	for i := 0; i < len(expr); {
		var name string
		switch c := expr[i]; {
		case c == '\'':
			_, i = quoted(expr, i)
			continue
		case c == '`' || c == '"':
			name, i = quoted(expr, i)
		case c == '_' || isLetter(c):
			start := i
			for i < len(expr) && (expr[i] == '_' || expr[i] == '.' || isLetter(expr[i]) || expr[i] >= '0' && expr[i] <= '9') {
				i++
			}
			// A Nested column's name holds a dot, and so does a qualified
			// name; either side may be the column.
			name = expr[start:i]
			if strings.HasPrefix(name, column+".") || strings.HasSuffix(name, "."+column) {
				return true
			}
		default:
			i++
		}
		if name == column {
			return true
		}
	}
	return false
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// quoted reads the quoted token that starts at expr[start] and returns its
// unescaped text and the index after it.
func quoted(expr string, start int) (text string, next int) {
	quote := expr[start]
	var b strings.Builder
	for i := start + 1; i < len(expr); i++ {
		switch expr[i] {
		case '\\':
			if i+1 < len(expr) {
				i++
				b.WriteByte(expr[i])
			}
		case quote:
			if i+1 < len(expr) && expr[i+1] == quote {
				i++
				b.WriteByte(quote)
				continue
			}
			return b.String(), i + 1
		default:
			b.WriteByte(expr[i])
		}
	}
	return b.String(), len(expr)
}
