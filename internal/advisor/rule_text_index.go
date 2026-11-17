package advisor

import (
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleTextIndex = "text_index"

// textIndexMinVersion is the oldest server that CI tests with a text index.
var textIndexMinVersion = proto.Version{Major: 26, Minor: 8}

// tokenIndexTypes are the skip index types that already serve token search.
var tokenIndexTypes = []string{"text", "tokenbf_v1", "ngrambf_v1"}

// textIndexFindings is R2: a text index for hasToken and for a LIKE whose
// pattern holds a complete token. It also notes the LogchefQL ~ searches
// that no index serves.
func textIndexFindings(a *analysis) []finding {
	if !a.usageRules {
		return nil
	}
	isToken := func(item models.FilterItem) bool {
		return a.plainString(item) && (item.Kind == models.MatchToken || item.Kind == models.MatchLikeToken)
	}
	token := a.groupUsage(func(item models.FilterItem) (string, bool) { return item.Column, isToken(item) })
	perKind := a.groupUsage(func(item models.FilterItem) (string, bool) {
		return item.Column + "\x00" + string(item.Kind), isToken(item)
	})
	a.positionNotes()
	supported := proto.CheckMinVersion(textIndexMinVersion, proto.ParseVersion(a.meta.ServerVersion))
	var out []finding
	// Two columns can map to one index name; the more used one gets it.
	names := map[string]bool{}
	for _, name := range mostUsed(token) {
		stat := token[name]
		share := a.share(stat.queries)
		if share < textIndexMinShare || stat.queries < textIndexMinQueries {
			continue
		}
		if !supported {
			a.note("text index advice needs ClickHouse 26.8")
			return nil
		}
		index := generatedName("idx", name, "text")
		if names[index] || !a.indexesKnown() || a.meta.IndexesTruncated || a.hasTokenIndex(name, index) {
			continue
		}
		names[index] = true
		column := a.columns[name]
		table, ident := a.qualifiedTarget(), queryescape.ClickHouseIdentifier(name)
		evidence := a.usageEvidence("Queries with a token search", stat)
		for _, kind := range []struct {
			kind  models.MatchKind
			label string
		}{
			{models.MatchToken, "Queries with hasToken, hasAllTokens or hasAnyTokens"},
			{models.MatchLikeToken, "Queries with a LIKE on a complete token"},
		} {
			if stat := perKind[name+"\x00"+string(kind.kind)]; stat != nil {
				evidence = append(evidence, models.AdvisorEvidence{Label: kind.label, Value: fmt.Sprintf("%s of %s (%s)", formatCount(stat.queries), formatCount(a.q), formatPercent(a.share(stat.queries))), Source: "logchef"})
			}
		}
		evidence = append(evidence, a.sizeEvidence(column)...)
		evidence = append(evidence, models.AdvisorEvidence{Label: "Existing skip indexes", Value: a.indexList(), Source: clickhouse.AdvisorInputIndexes})
		out = append(out, finding{share: share, SchemaFinding: models.SchemaFinding{
			ID:       a.findingID(ruleTextIndex, name, nil, ""),
			Rule:     ruleTextIndex,
			Impact:   impactFor(share),
			Title:    fmt.Sprintf("Add a text index on %s", name),
			Column:   name,
			Evidence: evidence,
			DDL: []string{
				fmt.Sprintf("%s ADD INDEX IF NOT EXISTS %s %s TYPE text(tokenizer = splitByNonAlpha);", a.alterTable(), queryescape.ClickHouseIdentifier(index), ident),
				fmt.Sprintf("%s MATERIALIZE INDEX %s;", a.alterTable(), queryescape.ClickHouseIdentifier(index)),
			},
			Gain: "hasToken, hasAllTokens and hasAnyTokens read the index. LIKE uses it when the pattern holds a complete token.",
			Cost: "The index covers each whole part. MATERIALIZE INDEX is a mutation over every part, and on a column with very many tokens it uses much memory. Index size is not predictable from metadata. " +
				"The index has no preprocessor, so case-sensitive searches stay case-sensitive.",
			Verify: fmt.Sprintf("EXPLAIN indexes = 1 SELECT count() FROM %s WHERE hasToken(%s, '<token>')", table, ident),
		}})
	}
	return out
}

// positionNotes notes each String column that LogchefQL ~ searches often
// enough to pass R2's trigger: ~ compiles to positionCaseInsensitive, which
// no index serves.
func (a *analysis) positionNotes() {
	var logchefQL []ShapeUsage
	for _, shape := range a.usage.Shapes {
		if shape.Language == models.QueryLanguageLogchefQL {
			logchefQL = append(logchefQL, shape)
		}
	}
	position := groupShapes(logchefQL, func(item models.FilterItem) (string, bool) {
		return item.Column, a.plainString(item) && item.Kind == models.MatchPosition
	})
	for _, name := range mostUsed(position) {
		stat := position[name]
		if a.share(stat.queries) < textIndexMinShare || stat.queries < textIndexMinQueries {
			continue
		}
		a.note(fmt.Sprintf("%s LogchefQL queries search %#q with `~`, which compiles to `positionCaseInsensitive`. No index serves it.", formatCount(stat.queries), name))
	}
}

// plainString reports whether a filter item reads a whole String column of
// the target.
func (a *analysis) plainString(item models.FilterItem) bool {
	if item.Access != "" || len(item.Path) > 0 {
		return false
	}
	column, ok := a.column(item.Column)
	return ok && column.Type == "String"
}

// hasTokenIndex reports whether a token search index reads the column, or a
// skip index already has the name the finding would use.
func (a *analysis) hasTokenIndex(column, name string) bool {
	for _, index := range a.meta.Indexes {
		if index.Name == name {
			return true
		}
		kind, _, _ := strings.Cut(index.Type, "(")
		for _, t := range tokenIndexTypes {
			if strings.TrimSpace(kind) == t && mentions(index.Expr, column) {
				return true
			}
		}
	}
	return false
}
