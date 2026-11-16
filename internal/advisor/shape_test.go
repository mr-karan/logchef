package advisor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

var testRelation = Relation{Database: "logs", Table: "app"}

func plain(column string, kind models.MatchKind) models.FilterItem {
	return models.FilterItem{Column: column, Kind: kind}
}

func pathItem(column, access string, kind models.MatchKind, path ...string) models.FilterItem {
	return models.FilterItem{Column: column, Path: path, Access: access, Kind: kind}
}

func shape(items ...models.FilterItem) models.FilterShape {
	return append(models.FilterShape{}, items...)
}

func assertShape(t *testing.T, query string, language models.QueryLanguage, want models.FilterShape) {
	t.Helper()
	got, ok := ShapeOf(query, language, testRelation)
	if !ok {
		t.Fatalf("ShapeOf(%q) is unanalyzed, want %v", query, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ShapeOf(%q)\n got %+v\nwant %+v", query, got, want)
	}
}

func TestShapeOfLogchefQL(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  models.FilterShape
	}{
		{"equals NULL", `user_id = NULL`, shape(plain("user_id", models.MatchNull))},
		{"not equals NULL", `user_id != null`, shape(plain("user_id", models.MatchNull))},
		{"quoted null is a string", `user_id = "null"`, shape(plain("user_id", models.MatchEq))},
		{"equals", `level = "error"`, shape(plain("level", models.MatchEq))},
		{"not equals", `level != "error"`, shape(plain("level", models.MatchNeq))},
		{"contains", `msg ~ "timeout"`, shape(plain("msg", models.MatchPosition))},
		{"not contains", `msg !~ "timeout"`, shape(plain("msg", models.MatchPosition))},
		{"greater", `status > 499`, shape(plain("status", models.MatchRange))},
		{"less", `status < 300`, shape(plain("status", models.MatchRange))},
		{"greater or equal", `status >= 500`, shape(plain("status", models.MatchRange))},
		{"less or equal", `status <= 299`, shape(plain("status", models.MatchRange))},
		{"nested field keeps segments", `log_attributes.user.id = "u1"`,
			shape(pathItem("log_attributes", models.AccessNested, models.MatchEq, "user", "id"))},
		{"quoted segment stays one segment", `log_attributes."user.id" = "u1"`,
			shape(pathItem("log_attributes", models.AccessNested, models.MatchEq, "user.id"))},
		{"and keeps both", `level = "error" and service = "api"`,
			shape(plain("level", models.MatchEq), plain("service", models.MatchEq))},
		{"or on one field keeps kinds", `level = "error" or level = "warn"`, shape(plain("level", models.MatchEq))},
		{"mixed-column or", `level = "error" or service = "api"`,
			shape(plain("level", models.MatchOther), plain("service", models.MatchOther))},
		{"or across paths of one column", `attrs.a = "1" or attrs.b = "2"`,
			shape(pathItem("attrs", models.AccessNested, models.MatchOther, "a"), pathItem("attrs", models.AccessNested, models.MatchOther, "b"))},
		{"mixed or inside and", `host = "h1" and (level = "error" or service = "api")`,
			shape(plain("host", models.MatchEq), plain("level", models.MatchOther), plain("service", models.MatchOther))},
		{"aggregation reads the filter only", `level = "error" | count() by service`, shape(plain("level", models.MatchEq))},
		{"empty query", `  `, shape()},
		{"duplicates collapse", `level = "a" and level = "b"`, shape(plain("level", models.MatchEq))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertShape(t, tt.query, models.QueryLanguageLogchefQL, tt.want)
		})
	}
}

func TestShapeOfSQL(t *testing.T) {
	where := func(cond string) string { return "SELECT * FROM logs.app WHERE " + cond }
	tests := []struct {
		name  string
		query string
		want  models.FilterShape
	}{
		{"equals", where("a = 1"), shape(plain("a", models.MatchEq))},
		{"double equals", where("a == 1"), shape(plain("a", models.MatchEq))},
		{"not equals", where("a != 1"), shape(plain("a", models.MatchNeq))},
		{"angle not equals", where("a <> 1"), shape(plain("a", models.MatchNeq))},
		{"not in", where("a NOT IN (1, 2)"), shape(plain("a", models.MatchNeq))},
		{"global not in", where("a GLOBAL NOT IN (1, 2)"), shape(plain("a", models.MatchNeq))},
		{"in", where("a IN ('x', 'y')"), shape(plain("a", models.MatchIn))},
		{"in subquery", where("a IN (SELECT b FROM other)"), shape(plain("a", models.MatchIn))},
		{"less", where("a < 1"), shape(plain("a", models.MatchRange))},
		{"greater", where("a > 1"), shape(plain("a", models.MatchRange))},
		{"less or equal", where("a <= 1"), shape(plain("a", models.MatchRange))},
		{"greater or equal", where("a >= 1"), shape(plain("a", models.MatchRange))},
		{"between", where("ts BETWEEN now() - INTERVAL 1 HOUR AND now()"), shape(plain("ts", models.MatchRange))},
		{"literal on the left", where("5 < a"), shape(plain("a", models.MatchRange))},
		{"is null", where("a IS NULL"), shape(plain("a", models.MatchNull))},
		{"is not null", where("a IS NOT NULL"), shape(plain("a", models.MatchNull))},
		{"isNull", where("isNull(a)"), shape(plain("a", models.MatchNull))},
		{"isNotNull", where("isNotNull(a)"), shape(plain("a", models.MatchNull))},
		{"like prefix", where("path LIKE '/api%'"), shape(plain("path", models.MatchPrefix))},
		{"startsWith", where("startsWith(path, '/api')"), shape(plain("path", models.MatchPrefix))},
		{"like spaced token", where("msg LIKE '% timeout %'"), shape(plain("msg", models.MatchLikeToken))},
		{"like token at the edge", where("msg LIKE 'conn refused%'"), shape(plain("msg", models.MatchLikeToken))},
		{"like without wildcard", where("msg LIKE 'timeout'"), shape(plain("msg", models.MatchLikeToken))},
		{"like token between escaped wildcards", where(`msg LIKE '%\\_timeout\\_%'`), shape(plain("msg", models.MatchLikeToken))},
		{"like bare substring", where("msg LIKE '%timeout%'"), shape(plain("msg", models.MatchLike))},
		{"like short runs", where("msg LIKE '% ab %'"), shape(plain("msg", models.MatchLike))},
		{"like underscore wildcard", where("msg LIKE '%conn_refused%'"), shape(plain("msg", models.MatchLike))},
		{"like token before an escaped underscore", where(`msg LIKE '%conn\\_refused%'`), shape(plain("msg", models.MatchLike))},
		{"like non-literal pattern", where("msg LIKE concat('%', 'x')"), shape(plain("msg", models.MatchLike))},
		{"ilike", where("msg ILIKE '% timeout %'"), shape(plain("msg", models.MatchILike))},
		{"match", where("match(msg, '[0-9]+')"), shape(plain("msg", models.MatchRegex))},
		{"regexp", where("msg REGEXP 'time.*out'"), shape(plain("msg", models.MatchRegex))},
		{"hasToken", where("hasToken(msg, 'timeout')"), shape(plain("msg", models.MatchToken))},
		{"hasAllTokens", where("hasAllTokens(msg, ['a', 'b'])"), shape(plain("msg", models.MatchToken))},
		{"hasAnyTokens", where("hasAnyTokens(msg, ['a', 'b'])"), shape(plain("msg", models.MatchToken))},
		{"positionCaseInsensitive", where("positionCaseInsensitive(msg, 'x') > 0"), shape(plain("msg", models.MatchPosition))},
		{"compared hasToken may be negated", where("hasToken(msg, 'x') = 0"), shape(plain("msg", models.MatchOther))},
		{"compared startsWith may be negated", where("startsWith(path, '/api') = 0"), shape(plain("path", models.MatchOther))},
		{"position", where("position(msg, 'x') > 0"), shape(plain("msg", models.MatchPosition))},
		{"multiSearchAny", where("multiSearchAny(msg, ['x', 'y'])"), shape(plain("msg", models.MatchPosition))},
		{"map element", where("attrs['user_id'] = 'u1'"), shape(pathItem("attrs", models.AccessMap, models.MatchEq, "user_id"))},
		{"map element in", where("attrs['k'] IN ('a', 'b')"), shape(pathItem("attrs", models.AccessMap, models.MatchIn, "k"))},
		{"JSONExtractString", where("JSONExtractString(body, 'a', 'b') = 'x'"),
			shape(pathItem("body", models.AccessJSONString, models.MatchEq, "a", "b"))},
		{"JSONExtractString dotted key", where("JSONExtractString(body, 'a.b') = 'x'"),
			shape(pathItem("body", models.AccessJSONString, models.MatchEq, "a.b"))},
		{"JSONExtractInt", where("JSONExtractInt(body, 'status') >= 500"),
			shape(pathItem("body", models.AccessJSONOther, models.MatchRange, "status"))},
		{"JSONExtract drops the type argument", where("JSONExtract(body, 'a', 'Int64') = 1"),
			shape(pathItem("body", models.AccessJSONOther, models.MatchEq, "a"))},
		{"subcolumn", where("resource.service.name = 'api'"),
			shape(pathItem("resource", models.AccessSubcolumn, models.MatchEq, "service", "name"))},
		{"function around the column", where("lower(level) = 'error'"), shape(plain("level", models.MatchOther))},
		{"cast around the column", where("toString(status) = '500'"), shape(plain("status", models.MatchOther))},
		{"bare boolean column", where("is_error"), shape(plain("is_error", models.MatchOther))},
		{"array index", where("tags[1] = 'x'"), shape(plain("tags", models.MatchOther))},
		{"true and null are literals", where("a = true AND b = NULL"), shape(plain("a", models.MatchEq), plain("b", models.MatchEq))},
		{"not equality", where("NOT (a = 1)"), shape(plain("a", models.MatchNeq))},
		{"not in-list", where("NOT a IN (1, 2)"), shape(plain("a", models.MatchNeq))},
		{"not range", where("NOT (a > 1)"), shape(plain("a", models.MatchOther))},
		{"not like", where("msg NOT LIKE '% x %'"), shape(plain("msg", models.MatchOther))},
		{"not ilike", where("msg NOT ILIKE 'x'"), shape(plain("msg", models.MatchOther))},
		{"not between", where("a NOT BETWEEN 1 AND 2"), shape(plain("a", models.MatchOther))},
		{"not over a group", where("NOT (a = 1 AND b IN (2))"), shape(plain("a", models.MatchNeq), plain("b", models.MatchNeq))},
		{"or on one column", where("a = 1 OR a IN (2, 3)"), shape(plain("a", models.MatchEq), plain("a", models.MatchIn))},
		{"mixed-column or", where("a = 1 OR b = 2"), shape(plain("a", models.MatchOther), plain("b", models.MatchOther))},
		{"mixed or under and", where("c = 1 AND (a = 1 OR b = 2)"),
			shape(plain("a", models.MatchOther), plain("b", models.MatchOther), plain("c", models.MatchEq))},
		{"or across map keys", where("attrs['a'] = '1' OR attrs['b'] = '2'"),
			shape(pathItem("attrs", models.AccessMap, models.MatchOther, "a"), pathItem("attrs", models.AccessMap, models.MatchOther, "b"))},
		{"column to column", where("a = b"), shape()},
		{"column to column under and", where("a = b AND c = 1"), shape(plain("c", models.MatchEq))},
		{"or with a column-to-column branch", where("a = b OR c = 1"), shape(plain("c", models.MatchOther))},
		{"or with a function over two columns", where("c = 1 OR concat(a, b) = 'x'"), shape(plain("c", models.MatchOther))},
		{"column to column through arithmetic", where("a = b + 1"), shape()},
		{"two columns in one function", where("concat(a, b) = 'x'"), shape()},
		{"lowercase keywords", "select * from logs.app where a = 1 and (b = 2 or b = 3) and not c in (1)",
			shape(plain("a", models.MatchEq), plain("b", models.MatchEq), plain("c", models.MatchNeq))},
		{"prewhere and where", "SELECT * FROM logs.app PREWHERE a = 1 WHERE b > 2",
			shape(plain("a", models.MatchEq), plain("b", models.MatchRange))},
		{"bare table name", "SELECT * FROM app WHERE a = 1", shape(plain("a", models.MatchEq))},
		{"quoted table name", "SELECT * FROM `logs`.`app` WHERE a = 1", shape(plain("a", models.MatchEq))},
		{"qualified columns", "SELECT * FROM logs.app AS t WHERE t.a = 1 AND app.b = 2 AND logs.app.c = 3",
			shape(plain("a", models.MatchEq), plain("b", models.MatchEq), plain("c", models.MatchEq))},
		{"parenthesized query", "(SELECT * FROM logs.app WHERE a = 1)", shape(plain("a", models.MatchEq))},
		{"no where", "SELECT count() FROM logs.app", shape()},
		{"unrelated cte", "WITH 5 AS lim SELECT * FROM logs.app WHERE a = 1", shape(plain("a", models.MatchEq))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertShape(t, tt.query, models.QueryLanguageClickHouseSQL, tt.want)
		})
	}
}

func TestShapeOfUnanalyzed(t *testing.T) {
	columns := func(n int) string {
		conds := make([]string, n)
		for i := range conds {
			conds[i] = fmt.Sprintf("c%02d = 1", i)
		}
		return strings.Join(conds, " AND ")
	}
	tests := []struct {
		name     string
		query    string
		language models.QueryLanguage
	}{
		{"join", "SELECT * FROM logs.app AS a JOIN logs.other AS b ON a.id = b.id WHERE a.x = 1", models.QueryLanguageClickHouseSQL},
		{"comma join", "SELECT * FROM logs.app, logs.other WHERE x = 1", models.QueryLanguageClickHouseSQL},
		{"subquery in from", "SELECT * FROM (SELECT * FROM logs.app) WHERE x = 1", models.QueryLanguageClickHouseSQL},
		{"union", "SELECT * FROM logs.app WHERE x = 1 UNION ALL SELECT * FROM logs.app WHERE y = 1", models.QueryLanguageClickHouseSQL},
		{"table function", "SELECT * FROM numbers(10) WHERE number = 1", models.QueryLanguageClickHouseSQL},
		{"another table", "SELECT * FROM logs.other WHERE x = 1", models.QueryLanguageClickHouseSQL},
		{"another database", "SELECT * FROM audit.app WHERE x = 1", models.QueryLanguageClickHouseSQL},
		{"cte named like the table", "WITH app AS (SELECT * FROM logs.other) SELECT * FROM app WHERE a = 1", models.QueryLanguageClickHouseSQL},
		{"scalar named like the table", "WITH 1 AS app SELECT * FROM app WHERE a = 1", models.QueryLanguageClickHouseSQL},
		{"unparsable sql", "SELECT * FROM logs.app WHERE", models.QueryLanguageClickHouseSQL},
		{"not a select", "DROP TABLE logs.app", models.QueryLanguageClickHouseSQL},
		{"unparsable logchefql", `level = `, models.QueryLanguageLogchefQL},
		{"logsql", `level:error`, models.QueryLanguageLogsQL},
		{"17 sql items", "SELECT * FROM logs.app WHERE " + columns(17), models.QueryLanguageClickHouseSQL},
		{"17 logchefql items", strings.ReplaceAll(columns(17), "AND", "and"), models.QueryLanguageLogchefQL},
		{"long column", "SELECT * FROM logs.app WHERE " + strings.Repeat("c", 129) + " = 1", models.QueryLanguageClickHouseSQL},
		{"nine path segments", "SELECT * FROM logs.app WHERE JSONExtractString(b, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i') = 'x'", models.QueryLanguageClickHouseSQL},
		{"long path", "SELECT * FROM logs.app WHERE attrs['" + strings.Repeat("k", 257) + "'] = 'x'", models.QueryLanguageClickHouseSQL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := ShapeOf(tt.query, tt.language, testRelation); ok {
				t.Fatalf("ShapeOf(%q) = %v, want unanalyzed", tt.query, got)
			}
		})
	}
}

func TestShapeOfBoundsAreInclusive(t *testing.T) {
	conds := make([]string, 16)
	for i := range conds {
		conds[i] = fmt.Sprintf("c%02d = 1", i)
	}
	got, ok := ShapeOf("SELECT * FROM logs.app WHERE "+strings.Join(conds, " AND "), models.QueryLanguageClickHouseSQL, testRelation)
	if !ok || len(got) != 16 {
		t.Fatalf("16 items: ok=%v len=%d, want ok with 16 items", ok, len(got))
	}
	long := "SELECT * FROM logs.app WHERE " + strings.Repeat("c", 128) + " = 1 AND attrs['" + strings.Repeat("k", 256) + "'] = 'x'"
	if _, ok := ShapeOf(long, models.QueryLanguageClickHouseSQL, testRelation); !ok {
		t.Fatal("a 128-byte column and a 256-byte path are within the bounds")
	}
}

func TestShapeHoldsNoLiteral(t *testing.T) {
	literals := []string{"needle-4711", "secret-token", "8675309", "2026-11-16", "haystack"}
	queries := []struct {
		query    string
		language models.QueryLanguage
	}{
		{`SELECT * FROM logs.app WHERE request_id = 'needle-4711' AND token IN ('secret-token', '8675309')
			AND ts > toDateTime('2026-11-16 00:00:00') AND msg LIKE '% haystack %' AND hasToken(msg, 'haystack')
			AND match(msg, 'needle-4711') AND positionCaseInsensitive(msg, 'secret-token') > 0
			AND NOT (lower(level) = 'needle-4711') AND (user = 'secret-token' OR team = '8675309')`, models.QueryLanguageClickHouseSQL},
		{`request_id = "needle-4711" and token != "secret-token" and status > 8675309 and msg ~ "haystack"
			and (user = "secret-token" or team = "2026-11-16")`, models.QueryLanguageLogchefQL},
	}
	for _, q := range queries {
		got, ok := ShapeOf(q.query, q.language, testRelation)
		if !ok || len(got) == 0 {
			t.Fatalf("ShapeOf(%q) = %v, %v; want a non-empty shape", q.query, got, ok)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		for _, literal := range literals {
			if strings.Contains(string(encoded), literal) {
				t.Errorf("shape %s holds the literal %q", encoded, literal)
			}
		}
	}
}
