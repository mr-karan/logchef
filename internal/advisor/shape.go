// Package advisor turns the filters that queries use into schema advice for a
// ClickHouse source. It does no I/O.
package advisor

import (
	"cmp"
	"reflect"
	"slices"
	"strings"

	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// Relation is the table that a source reads.
type Relation struct {
	Database string
	Table    string
}

// Bounds on one shape. A query over a bound is unanalyzed.
const (
	maxShapeItems   = 16
	maxColumnBytes  = 128
	maxPathSegments = 8
	maxPathBytes    = 256
)

// ShapeOf returns the filter shape of a query and whether the query could be
// analyzed. A query with no filter has an empty shape.
func ShapeOf(query string, language models.QueryLanguage, rel Relation) (models.FilterShape, bool) {
	var items []models.FilterItem
	var ok bool
	switch models.NormalizeQueryLanguage(language) {
	case models.QueryLanguageLogchefQL:
		items, ok = logchefQLItems(query)
	case models.QueryLanguageClickHouseSQL:
		items, ok = sqlItems(query, rel)
	case models.QueryLanguageLogsQL:
		// VictoriaLogs has no user-defined schema to tune.
	}
	if !ok {
		return nil, false
	}
	return normalizeShape(items)
}

func normalizeShape(items []models.FilterItem) (models.FilterShape, bool) {
	shape := models.FilterShape{}
	for _, item := range items {
		if len(item.Path) == 0 {
			item.Path = nil
		}
		if len(item.Column) > maxColumnBytes || len(item.Path) > maxPathSegments {
			return nil, false
		}
		pathBytes := 0
		for _, segment := range item.Path {
			pathBytes += len(segment)
		}
		if pathBytes > maxPathBytes {
			return nil, false
		}
		shape = append(shape, item)
	}
	slices.SortFunc(shape, compareItems)
	shape = slices.CompactFunc(shape, func(a, b models.FilterItem) bool { return compareItems(a, b) == 0 })
	if len(shape) > maxShapeItems {
		return nil, false
	}
	return shape, true
}

func compareItems(a, b models.FilterItem) int {
	return cmp.Or(
		cmp.Compare(a.Column, b.Column),
		slices.Compare(a.Path, b.Path),
		cmp.Compare(a.Access, b.Access),
		cmp.Compare(a.Kind, b.Kind),
	)
}

func sameSubject(a, b models.FilterItem) bool {
	return a.Column == b.Column && slices.Equal(a.Path, b.Path) && a.Access == b.Access
}

func logchefQLItems(query string) ([]models.FilterItem, bool) {
	if strings.TrimSpace(query) == "" {
		return nil, true
	}
	parsed, err := logchefql.ParseLogchefQL(query)
	if err != nil {
		return nil, false
	}
	var items []models.FilterItem
	for _, filter := range logchefql.FilterItems(logchefql.ConvertToAST(parsed)) {
		item := models.FilterItem{Column: filter.Base, Path: filter.Path, Kind: logchefQLKind(filter)}
		if len(filter.Path) > 0 {
			item.Access = models.AccessNested
		}
		items = append(items, item)
	}
	return items, true
}

func logchefQLKind(filter logchefql.FilterItem) models.MatchKind {
	if filter.Disjunct {
		return models.MatchOther
	}
	switch filter.Operator {
	case logchefql.OpEquals:
		if filter.Null {
			return models.MatchNull
		}
		return models.MatchEq
	case logchefql.OpNotEquals:
		if filter.Null {
			return models.MatchNull
		}
		return models.MatchNeq
	case logchefql.OpRegex, logchefql.OpNotRegex:
		// The generator compiles ~ to positionCaseInsensitive(col, 'x') > 0.
		return models.MatchPosition
	case logchefql.OpGT, logchefql.OpLT, logchefql.OpGTE, logchefql.OpLTE:
		return models.MatchRange
	}
	return models.MatchOther
}

// sqlItems reads WHERE and PREWHERE of a single SELECT on the source's table.
func sqlItems(query string, rel Relation) ([]models.FilterItem, bool) {
	stmt, err := clickhouse.ParseSelectQuery(query)
	if err != nil {
		return nil, false
	}
	for {
		if stmt.UnionAll != nil || stmt.UnionDistinct != nil || stmt.Except != nil || stmt.Intersect != nil {
			return nil, false
		}
		if stmt.InnerQuery == nil {
			break
		}
		stmt = stmt.InnerQuery
	}
	qualifiers, ok := tableQualifiers(stmt.From, rel)
	if !ok || shadowsTable(stmt.With, rel.Table) {
		return nil, false
	}
	x := sqlExtractor{qualifiers: qualifiers}
	var items []models.FilterItem
	if stmt.Prewhere != nil {
		items = append(items, x.collect(stmt.Prewhere.Expr)...)
	}
	if stmt.Where != nil {
		items = append(items, x.collect(stmt.Where.Expr)...)
	}
	return items, true
}

// shadowsTable reports a WITH name equal to the table name, which a bare
// FROM name would read instead of the table.
func shadowsTable(with *chparser.WithClause, table string) bool {
	if with == nil {
		return false
	}
	for _, cte := range with.CTEs {
		for _, name := range []chparser.Expr{cte.Expr, cte.Alias} {
			if ident, ok := name.(*chparser.Ident); ok && identNamed(ident, table) {
				return true
			}
		}
	}
	return false
}

func identNamed(ident *chparser.Ident, name string) bool {
	decoded, ok := clickhouse.IdentName(ident)
	return ok && decoded == name
}

// tableQualifiers checks that FROM names only the source's table and returns
// the prefixes that qualify its columns, longest first.
func tableQualifiers(from *chparser.FromClause, rel Relation) ([][]string, bool) {
	if from == nil {
		return nil, false
	}
	joinTable, ok := from.Expr.(*chparser.JoinTableExpr)
	if !ok || joinTable.Table == nil {
		return nil, false
	}
	expr, alias := joinTable.Table.Expr, ""
	if aliased, ok := expr.(*chparser.AliasExpr); ok {
		ident, ok := aliased.Alias.(*chparser.Ident)
		if !ok {
			return nil, false
		}
		if alias, ok = clickhouse.IdentName(ident); !ok {
			return nil, false
		}
		expr = aliased.Expr
	}
	table, ok := expr.(*chparser.TableIdentifier)
	if !ok || table.Table == nil {
		return nil, false
	}
	if name, ok := clickhouse.IdentName(table.Table); !ok || name != rel.Table {
		return nil, false
	}
	if table.Database != nil {
		if name, ok := clickhouse.IdentName(table.Database); !ok || name != rel.Database {
			return nil, false
		}
	}
	qualifiers := [][]string{{rel.Database, rel.Table}, {rel.Table}}
	if alias != "" {
		qualifiers = append(qualifiers, []string{alias})
	}
	return qualifiers, true
}

type sqlExtractor struct {
	qualifiers [][]string
}

// columnRef is the column that an expression reads. Wrapped means a function
// or operator other than a map, JSON or subcolumn access surrounds it.
type columnRef struct {
	column  string
	path    []string
	access  string
	wrapped bool
}

func (r columnRef) item(kind models.MatchKind) models.FilterItem {
	if r.wrapped {
		return models.FilterItem{Column: r.column, Kind: models.MatchOther}
	}
	return models.FilterItem{Column: r.column, Path: r.path, Access: r.access, Kind: kind}
}

// collect returns the items of a boolean expression. An item under NOT stays
// useful only as a negated equality, and an OR across subjects narrows none.
func (x sqlExtractor) collect(expr chparser.Expr) []models.FilterItem {
	switch e := expr.(type) {
	case *chparser.BinaryOperation:
		switch strings.ToUpper(string(e.Operation)) {
		case chparser.KeywordAnd:
			return append(x.collect(e.LeftExpr), x.collect(e.RightExpr)...)
		case chparser.KeywordOr:
			items := append(x.collect(e.LeftExpr), x.collect(e.RightExpr)...)
			// A branch that yields no item, such as a = b, still reads columns.
			read := map[string]struct{}{}
			mixed := !x.columns(e, read) || len(read) > 1
			for _, item := range items {
				mixed = mixed || !sameSubject(item, items[0])
			}
			if mixed {
				return withKind(items, func(models.MatchKind) models.MatchKind { return models.MatchOther })
			}
			return items
		}
	case *chparser.UnaryExpr:
		if strings.EqualFold(string(e.Kind), chparser.KeywordNot) {
			return withKind(x.collect(e.Expr), negated)
		}
	case *chparser.ParamExprList:
		if inner, ok := parenthesized(e); ok {
			return x.collect(inner)
		}
	}
	if item, ok := x.leaf(expr); ok {
		return []models.FilterItem{item}
	}
	return nil
}

func withKind(items []models.FilterItem, kind func(models.MatchKind) models.MatchKind) []models.FilterItem {
	for i := range items {
		items[i].Kind = kind(items[i].Kind)
	}
	return items
}

func negated(kind models.MatchKind) models.MatchKind {
	if kind == models.MatchEq || kind == models.MatchIn {
		return models.MatchNeq
	}
	return models.MatchOther
}

func parenthesized(list *chparser.ParamExprList) (chparser.Expr, bool) {
	if list.Items == nil || len(list.Items.Items) != 1 || list.ColumnArgList != nil {
		return nil, false
	}
	if column, ok := list.Items.Items[0].(*chparser.ColumnExpr); ok && column.Alias == nil {
		return column.Expr, true
	}
	return nil, false
}

// leaf maps one predicate to an item. A predicate that compares two columns,
// or reads none, yields nothing.
func (x sqlExtractor) leaf(expr chparser.Expr) (models.FilterItem, bool) {
	switch e := expr.(type) {
	case *chparser.BinaryOperation:
		if item, ok, handled := x.comparison(e); handled {
			return item, ok
		}
	case *chparser.BetweenClause:
		ref, ok := x.ref(e.Expr)
		if !ok || !x.constant(e.Between) || !x.constant(e.And) {
			return models.FilterItem{}, false
		}
		if e.Not {
			return ref.item(models.MatchOther), true
		}
		return ref.item(models.MatchRange), true
	case *chparser.IsNullExpr:
		return x.refItem(e.Expr, models.MatchNull)
	case *chparser.IsNotNullExpr:
		return x.refItem(e.Expr, models.MatchNull)
	case *chparser.FunctionExpr:
		if item, ok, handled := x.predicate(e); handled {
			return item, ok
		}
	}
	ref, ok := x.wrappedRef(expr)
	if !ok {
		return models.FilterItem{}, false
	}
	return ref.item(models.MatchOther), true
}

func (x sqlExtractor) refItem(expr chparser.Expr, kind models.MatchKind) (models.FilterItem, bool) {
	ref, ok := x.ref(expr)
	if !ok {
		return models.FilterItem{}, false
	}
	return ref.item(kind), true
}

// comparison maps a binary predicate. handled is false for an operator that
// is not a comparison, such as arithmetic.
func (x sqlExtractor) comparison(e *chparser.BinaryOperation) (item models.FilterItem, ok, handled bool) {
	op := strings.ToUpper(string(e.Operation))
	var kind models.MatchKind
	symmetric := false
	switch op {
	case "=", "==":
		kind, symmetric = models.MatchEq, true
	case "!=", "<>":
		kind, symmetric = models.MatchNeq, true
	case "<", ">", "<=", ">=":
		kind, symmetric = models.MatchRange, true
	case "IN":
		kind = models.MatchIn
		if e.HasNot {
			kind = models.MatchNeq
		}
	case "NOT IN":
		kind = models.MatchNeq
	case "LIKE":
		kind = models.MatchLike
		if pattern, isLiteral := e.RightExpr.(*chparser.StringLiteral); isLiteral {
			kind = likeKind(pattern.Literal)
		}
	case "NOT LIKE", "NOT ILIKE":
		kind = models.MatchOther
	case "ILIKE":
		kind = models.MatchILike
	case "REGEXP":
		kind = models.MatchRegex
	default:
		return models.FilterItem{}, false, false
	}

	side, other := e.LeftExpr, e.RightExpr
	if symmetric && x.constant(side) {
		side, other = other, side
	}
	if !x.constant(other) {
		return models.FilterItem{}, false, true
	}
	if fn, isFunction := side.(*chparser.FunctionExpr); isFunction {
		if item, ok, handled := x.predicate(fn); handled {
			// A position is compared by nature. A boolean search compared
			// with a value may be negated, as in hasToken(c, 'x') = 0.
			if item.Kind != models.MatchPosition {
				item.Kind = models.MatchOther
			}
			return item, ok, true
		}
	}
	ref, ok := x.ref(side)
	if !ok {
		return models.FilterItem{}, false, true
	}
	return ref.item(kind), true, true
}

// predicate maps a search function whose first argument is the column, such
// as startsWith(col, 'x'). handled is false for any other function.
func (x sqlExtractor) predicate(fn *chparser.FunctionExpr) (item models.FilterItem, ok, handled bool) {
	if fn.Name == nil {
		return models.FilterItem{}, false, false
	}
	kind, isPredicate := predicateKind(fn.Name.Name)
	if !isPredicate {
		return models.FilterItem{}, false, false
	}
	args := functionArgs(fn)
	if len(args) == 0 {
		return models.FilterItem{}, false, true
	}
	for _, arg := range args[1:] {
		if !x.constant(arg) {
			return models.FilterItem{}, false, true
		}
	}
	ref, ok := x.ref(args[0])
	if !ok {
		return models.FilterItem{}, false, true
	}
	return ref.item(kind), true, true
}

func predicateKind(name string) (models.MatchKind, bool) {
	switch name {
	case "startsWith":
		return models.MatchPrefix, true
	case "match":
		return models.MatchRegex, true
	case "hasToken", "hasAllTokens", "hasAnyTokens":
		return models.MatchToken, true
	case "isNull", "isNotNull":
		return models.MatchNull, true
	}
	if strings.HasPrefix(name, "position") || strings.HasPrefix(name, "multiSearch") {
		return models.MatchPosition, true
	}
	return "", false
}

func functionArgs(fn *chparser.FunctionExpr) []chparser.Expr {
	if fn.Params == nil || fn.Params.Items == nil {
		return nil
	}
	args := make([]chparser.Expr, 0, len(fn.Params.Items.Items))
	for _, item := range fn.Params.Items.Items {
		if column, ok := item.(*chparser.ColumnExpr); ok && column.Alias == nil {
			item = column.Expr
		}
		args = append(args, item)
	}
	return args
}

// ref returns the one column that expr reads.
func (x sqlExtractor) ref(expr chparser.Expr) (columnRef, bool) {
	switch e := expr.(type) {
	case *chparser.Ident:
		if literalIdent(e) {
			return columnRef{}, false
		}
		name, ok := clickhouse.IdentName(e)
		return columnRef{column: name}, ok
	case *chparser.Path:
		if names, ok := x.unqualified(e.Fields); ok {
			if len(names) == 1 {
				return columnRef{column: names[0]}, true
			}
			return columnRef{column: names[0], path: names[1:], access: models.AccessSubcolumn}, true
		}
	case *chparser.ObjectParams:
		if ref, ok := x.mapElement(e); ok {
			return ref, true
		}
	case *chparser.FunctionExpr:
		if ref, ok := x.jsonExtract(e); ok {
			return ref, true
		}
	case *chparser.ParamExprList:
		if inner, ok := parenthesized(e); ok {
			return x.ref(inner)
		}
	}
	return x.wrappedRef(expr)
}

// wrappedRef returns the column of an expression that reads exactly one.
func (x sqlExtractor) wrappedRef(expr chparser.Expr) (columnRef, bool) {
	columns := map[string]struct{}{}
	if !x.columns(expr, columns) || len(columns) != 1 {
		return columnRef{}, false
	}
	for column := range columns {
		return columnRef{column: column, wrapped: true}, true
	}
	return columnRef{}, false
}

// unqualified strips a table or alias qualifier from the fields of a path.
func (x sqlExtractor) unqualified(fields []*chparser.Ident) ([]string, bool) {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		name, ok := clickhouse.IdentName(field)
		if !ok {
			return nil, false
		}
		names = append(names, name)
	}
	for _, qualifier := range x.qualifiers {
		if len(names) > len(qualifier) && slices.Equal(names[:len(qualifier)], qualifier) {
			return names[len(qualifier):], true
		}
	}
	return names, true
}

// mapElement reads m['k'], and m['k']['j'] as a two-segment path.
func (x sqlExtractor) mapElement(e *chparser.ObjectParams) (columnRef, bool) {
	if e.Params == nil || e.Params.Items == nil || len(e.Params.Items.Items) != 1 {
		return columnRef{}, false
	}
	key, ok := stringArg(e.Params.Items.Items[0])
	if !ok {
		return columnRef{}, false
	}
	var base columnRef
	switch object := e.Object.(type) {
	case *chparser.ObjectParams:
		if base, ok = x.mapElement(object); !ok {
			return columnRef{}, false
		}
	default:
		if base, ok = x.ref(object); !ok || base.wrapped || base.access != "" {
			return columnRef{}, false
		}
	}
	return columnRef{column: base.column, path: append(slices.Clone(base.path), key), access: models.AccessMap}, true
}

// jsonExtract reads JSONExtract*(col, 'a', 'b'). JSONExtract and
// JSONExtractKeysAndValues take the return type as their last argument.
func (x sqlExtractor) jsonExtract(fn *chparser.FunctionExpr) (columnRef, bool) {
	if fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "JSONExtract") {
		return columnRef{}, false
	}
	args := functionArgs(fn)
	if fn.Name.Name == "JSONExtract" || fn.Name.Name == "JSONExtractKeysAndValues" {
		if len(args) < 2 {
			return columnRef{}, false
		}
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		return columnRef{}, false
	}
	base, ok := x.ref(args[0])
	if !ok || base.wrapped || base.access != "" {
		return columnRef{}, false
	}
	var path []string
	for _, arg := range args[1:] {
		segment, ok := stringArg(arg)
		if !ok {
			return columnRef{}, false
		}
		path = append(path, segment)
	}
	access := models.AccessJSONOther
	if fn.Name.Name == "JSONExtractString" {
		access = models.AccessJSONString
	}
	return columnRef{column: base.column, path: path, access: access}, true
}

func stringArg(expr chparser.Expr) (string, bool) {
	if column, ok := expr.(*chparser.ColumnExpr); ok && column.Alias == nil {
		expr = column.Expr
	}
	literal, ok := expr.(*chparser.StringLiteral)
	if !ok {
		return "", false
	}
	return decodeString(literal.Literal), true
}

func (x sqlExtractor) constant(expr chparser.Expr) bool {
	columns := map[string]struct{}{}
	return x.columns(expr, columns) && len(columns) == 0
}

// columns adds the columns that expr reads. It reports false for a node it
// does not know, so an unknown expression never counts as a constant.
func (x sqlExtractor) columns(expr chparser.Expr, into map[string]struct{}) bool {
	if expr == nil || reflect.ValueOf(expr).IsNil() {
		return true
	}
	switch e := expr.(type) {
	case *chparser.StringLiteral, *chparser.NumberLiteral, *chparser.NullLiteral,
		*chparser.BoolLiteral, *chparser.QueryParam, *chparser.PlaceHolder, *chparser.SubQuery:
		return true
	case *chparser.Ident:
		if literalIdent(e) {
			return true
		}
		name, ok := clickhouse.IdentName(e)
		into[name] = struct{}{}
		return ok
	case *chparser.Path:
		names, ok := x.unqualified(e.Fields)
		if ok {
			into[names[0]] = struct{}{}
		}
		return ok
	}
	children, ok := operands(expr)
	for _, child := range children {
		ok = ok && x.columns(child, into)
	}
	return ok
}

// operands returns the subexpressions of an operator or function that can
// read a column: not a function name, a type, an interval unit or a member
// name.
func operands(expr chparser.Expr) ([]chparser.Expr, bool) {
	switch e := expr.(type) {
	case *chparser.ObjectParams:
		return []chparser.Expr{e.Object, e.Params}, true
	case *chparser.IndexOperation:
		if _, member := e.Index.(*chparser.Ident); member {
			return []chparser.Expr{e.Object}, true
		}
		return []chparser.Expr{e.Object, e.Index}, true
	case *chparser.FunctionExpr:
		return []chparser.Expr{e.Params}, true
	case *chparser.ParamExprList:
		return []chparser.Expr{e.Items}, e.ColumnArgList == nil
	case *chparser.ArrayParamList:
		return []chparser.Expr{e.Items}, true
	case *chparser.ColumnExprList:
		return e.Items, true
	case *chparser.ColumnExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.BinaryOperation:
		if e.Operation == chparser.TokenKindDash {
			return []chparser.Expr{e.LeftExpr}, true
		}
		return []chparser.Expr{e.LeftExpr, e.RightExpr}, true
	case *chparser.UnaryExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.CastExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.IntervalExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.BetweenClause:
		return []chparser.Expr{e.Expr, e.Between, e.And}, true
	case *chparser.IsNullExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.IsNotNullExpr:
		return []chparser.Expr{e.Expr}, true
	case *chparser.TernaryOperation:
		return []chparser.Expr{e.Condition, e.TrueExpr, e.FalseExpr}, true
	}
	return nil, false
}

// literalIdent reports TRUE, FALSE and NULL, which the parser reads as
// identifiers.
func literalIdent(ident *chparser.Ident) bool {
	if ident.QuoteType != chparser.Unquoted {
		return false
	}
	return strings.EqualFold(ident.Name, "true") || strings.EqualFold(ident.Name, "false") || strings.EqualFold(ident.Name, "null")
}

// decodeString reads the body of a ClickHouse string literal. ClickHouse
// keeps the backslash before a character with no escape meaning, so that
// 'a\%' stays an escaped wildcard for LIKE.
func decodeString(raw string) string {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\'' && i+1 < len(raw) && raw[i+1] == '\'':
			out.WriteByte('\'')
			i++
		case c == '\\' && i+1 < len(raw):
			i++
			switch next := raw[i]; next {
			case '\\', '\'', '"', '`', '/', '=':
				out.WriteByte(next)
			case 'n', 't', 'r', '0', 'b', 'f', 'a', 'v', 'e':
				// A control character: not a letter or digit either way.
				out.WriteByte(0)
			default:
				out.WriteByte('\\')
				out.WriteByte(next)
			}
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

type likeChar struct {
	c        byte
	wildcard bool
}

func likeChars(pattern string) []likeChar {
	chars := make([]likeChar, 0, len(pattern))
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\\' && i+1 < len(pattern):
			i++
			chars = append(chars, likeChar{c: pattern[i]})
		case c == '%' || c == '_':
			chars = append(chars, likeChar{c: c, wildcard: true})
		default:
			chars = append(chars, likeChar{c: c})
		}
	}
	return chars
}

// likeKind classifies a LIKE pattern. A complete token wins over a prefix:
// 'conn refused%' is both, and a text index serves it.
func likeKind(raw string) models.MatchKind {
	chars := likeChars(decodeString(raw))
	switch {
	case hasCompleteToken(chars):
		return models.MatchLikeToken
	case isPrefixPattern(chars):
		return models.MatchPrefix
	}
	return models.MatchLike
}

// hasCompleteToken reports a run of at least 3 ASCII letters or digits with
// a literal ASCII separator or a pattern edge on each side.
func hasCompleteToken(chars []likeChar) bool {
	separator := func(i int) bool {
		if i < 0 || i >= len(chars) {
			return true
		}
		ch := chars[i]
		return !ch.wildcard && ch.c < 0x80 && !isASCIIAlnum(ch.c)
	}
	for i := 0; i < len(chars); {
		if chars[i].wildcard || !isASCIIAlnum(chars[i].c) {
			i++
			continue
		}
		end := i
		for end < len(chars) && !chars[end].wildcard && isASCIIAlnum(chars[end].c) {
			end++
		}
		if end-i >= 3 && separator(i-1) && separator(end) {
			return true
		}
		i = end
	}
	return false
}

// isPrefixPattern reports literal characters followed by one trailing %.
func isPrefixPattern(chars []likeChar) bool {
	if len(chars) < 2 || !chars[len(chars)-1].wildcard || chars[len(chars)-1].c != '%' {
		return false
	}
	for _, ch := range chars[:len(chars)-1] {
		if ch.wildcard {
			return false
		}
	}
	return true
}

func isASCIIAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}
