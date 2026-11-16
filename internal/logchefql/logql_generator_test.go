package logchefql

import (
	"strings"
	"testing"
)

// The expected strings follow RFC 0011 "LogchefQL to LogQL", with {...}
// spelled out as the test source's selector.
func TestTranslateToLogQL(t *testing.T) {
	base := LogQLTranslateOptions{
		Selector:     []LogQLMatcher{{Name: "service_name", Op: "=~", Value: ".+"}},
		StreamLabels: []string{"service_name", "app"},
	}
	withC := base
	withC.StreamLabels = []string{"service_name", "app", "c"}
	withJSON := base
	withJSON.Parser = "json"
	withLogfmt := base
	withLogfmt.Parser = "logfmt"

	tests := []struct {
		name    string
		query   string
		options LogQLTranslateOptions
		want    string
	}{
		{"empty query", "", base, `{service_name=~".+"}`},
		{"blank query", "  ", base, `{service_name=~".+"}`},
		{"stream label equality is hoisted", `app="api"`, base, `{service_name=~".+", app="api"}`},
		{"stream label inequality is hoisted", `app!="api"`, base, `{service_name=~".+", app!="api"}`},
		{"stream label substring is an anchored case-insensitive regex", `app~"pay"`, base, `{service_name=~".+", app=~"(?i).*pay.*"}`},
		{"stream label negated substring", `app!~"pay"`, base, `{service_name=~".+", app!~"(?i).*pay.*"}`},
		{"substring value is regex-quoted", `app~"a.b"`, base, `{service_name=~".+", app=~"(?i).*a\\.b.*"}`},
		{"message substring is a line filter", `_msg~"timeout"`, base, `{service_name=~".+"} |~ "(?i)timeout"`},
		{"message negated substring", `_msg!~"timeout"`, base, `{service_name=~".+"} !~ "(?i)timeout"`},
		{"message equality is an anchored regex", `_msg="exact line"`, base, `{service_name=~".+"} |~ "^exact line$"`},
		{"message inequality", `_msg!="exact line"`, base, `{service_name=~".+"} !~ "^exact line$"`},
		{"message value is regex-quoted", `_msg="GET /a?b=1"`, base, `{service_name=~".+"} |~ "^GET /a\\?b=1$"`},
		{"or of message substrings is one line filter", `_msg~"a" or _msg~"b"`, base, `{service_name=~".+"} |~ "(?i)(a|b)"`},
		{"grouped or of message substrings beside a field", `x="y" and (_msg~"a" or _msg~"b")`, base, `{service_name=~".+"} |~ "(?i)(a|b)" | x="y"`},
		{"parenthesized and is flattened for hoisting", `(app="api" and x="y")`, base, `{service_name=~".+", app="api"} | x="y"`},
		{"or of a message match and a message substring", `_msg="a" or (_msg~"b")`, base, `{service_name=~".+"} |~ "^a$|(?i:b)"`},
		{"other field is a label filter", `trace_id="abc"`, base, `{service_name=~".+"} | trace_id="abc"`},
		{"numeric comparison drops conversion errors", `status>=500`, base, `{service_name=~".+"} | status>=500 | __error__=""`},
		{"quoted number in a comparison", `status<"600"`, base, `{service_name=~".+"} | status<600 | __error__=""`},
		{"numeric comparison on a stream label stays a label filter", `app>3`, base, `{service_name=~".+"} | app>3 | __error__=""`},
		{"or across classes", `a="x" or b="y"`, base, `{service_name=~".+"} | a="x" or b="y"`},
		// Loki's or stops at the first true branch, and a value that is not a
		// number makes a numeric comparison true with __error__ set. The
		// numeric branch goes last, so it runs only when the others are false.
		{"numeric branch of an or goes last", `status>=500 or level="error"`, base, `{service_name=~".+"} | level="error" or status>=500 | __error__=""`},
		{"numeric and branch of an or goes last", `(a>1 and b="x") or c="y"`, base, `{service_name=~".+"} | c="y" or (a>1 and b="x") | __error__=""`},
		{"numeric comparisons on one field in an or", `status>=500 or status<100`, base, `{service_name=~".+"} | status>=500 or status<100 | __error__=""`},
		{"number before a closing parenthesis", `(x=42 or y="a") and z="b"`, base, `{service_name=~".+"} | (y="a" or x=42) and z="b" | __error__=""`},
		{"or of stream labels is not hoisted", `app="x" or app="y"`, base, `{service_name=~".+"} | app="x" or app="y"`},
		{"grouped or and a field", `(a="x" or b="y") and c!="z"`, base, `{service_name=~".+"} | (a="x" or b="y") and c!="z"`},
		{"grouped or and a hoisted stream label", `(a="x" or b="y") and c!="z"`, withC, `{service_name=~".+", c!="z"} | (a="x" or b="y")`},
		{"and binds tighter than or", `a="x" and b="y" or c="z"`, base, `{service_name=~".+"} | a="x" and b="y" or c="z"`},
		{"nested and under an or keeps its parentheses", `a="x" or (b="y" and c="z")`, base, `{service_name=~".+"} | a="x" or (b="y" and c="z")`},
		{"dotted path joins with underscores", `k8s.pod.name="p"`, base, `{service_name=~".+"} | k8s_pod_name="p"`},
		{"quoted path segment with a valid name", `k8s."pod_name"="p"`, base, `{service_name=~".+"} | k8s_pod_name="p"`},
		{"json parser runs on every query", `trace_id="abc"`, withJSON, `{service_name=~".+"} | json | drop __error__, __error_details__ | trace_id="abc"`},
		{"json parser on an empty query", ``, withJSON, `{service_name=~".+"} | json | drop __error__, __error_details__`},
		{"logfmt parser with a numeric filter", `status>=500`, withLogfmt, `{service_name=~".+"} | logfmt | drop __error__, __error_details__ | status>=500 | __error__=""`},
		{"select pipe becomes keep with every stream label", `level="error" | ts msg`, base, `{service_name=~".+"} | level="error" | keep ts, msg, app, service_name`},
		{"select pipe skips _time and _msg and duplicates", `| _time app _msg app`, base, `{service_name=~".+"} | keep app, service_name`},
		{"function name as a label name", `ip="10.0.0.1" and count>1`, base, `{service_name=~".+"} | ip="10.0.0.1" and count>1 | __error__=""`},
		{"bare identifier is a string", `x=true`, base, `{service_name=~".+"} | x="true"`},
		{"number stays a number", `x=42`, base, `{service_name=~".+"} | x=42 | __error__=""`},
		{"negative number", `x!=-5`, base, `{service_name=~".+"} | x!=-5 | __error__=""`},
		{"string escapes", `msg_id="a\"b"`, base, `{service_name=~".+"} | msg_id="a\"b"`},
		{"every class together", `app="api" and _msg~"gateway" and level="error" and status>=500`, withJSON,
			`{service_name=~".+", app="api"} |~ "(?i)gateway" | json | drop __error__, __error_details__ | level="error" and status>=500 | __error__=""`},
		{"a hoisted matcher makes an empty-matching selector valid", `app="api"`, LogQLTranslateOptions{
			Selector: []LogQLMatcher{{Name: "env", Op: "=~", Value: ".*"}}, StreamLabels: []string{"app", "env"},
		}, `{env=~".*", app="api"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := TranslateToLogQL(tc.query, tc.options)
			if !result.Valid {
				t.Fatalf("TranslateToLogQL(%q) failed: %v", tc.query, result.Error)
			}
			if result.Query != tc.want {
				t.Fatalf("TranslateToLogQL(%q)\n got %s\nwant %s", tc.query, result.Query, tc.want)
			}
		})
	}
}

func TestTranslateToLogQLUnsupported(t *testing.T) {
	base := LogQLTranslateOptions{
		Selector:     []LogQLMatcher{{Name: "service_name", Op: "=~", Value: ".+"}},
		StreamLabels: []string{"service_name", "app"},
	}
	tests := []struct {
		name    string
		query   string
		options LogQLTranslateOptions
		code    string
		message string
	}{
		{"greater than on the message", `_msg>"a"`, base, ErrUnsupportedFeature, "cannot compare the log line with >"},
		{"greater or equal on the message", `_msg>=5`, base, ErrUnsupportedFeature, "cannot compare the log line with >="},
		{"less than on the message", `_msg<"a"`, base, ErrUnsupportedFeature, "cannot compare the log line with <"},
		{"less or equal on the message", `_msg<="a"`, base, ErrUnsupportedFeature, "cannot compare the log line with <="},
		{"or of the message and another field", `_msg~"a" or level="error"`, base, ErrUnsupportedFeature, "Move the _msg condition out of the or"},
		{"negated message inside an or", `_msg!~"a" or _msg~"b"`, base, ErrUnsupportedFeature, "Move the _msg condition out of the or"},
		{"message nested under an or", `(_msg~"a" and x="y") or z="w"`, base, ErrUnsupportedFeature, "Move the _msg condition out of the or"},
		{"null on a field", `x=null`, base, ErrUnsupportedFeature, "null comparisons"},
		{"null on a stream label", `app!=null`, base, ErrUnsupportedFeature, "null comparisons"},
		{"null on the message", `_msg=null`, base, ErrUnsupportedFeature, "null comparisons"},
		{"comparison with a word", `status>abc`, base, ErrUnsupportedFeature, "needs a number"},
		{"comparison with NaN", `status>"NaN"`, base, ErrUnsupportedFeature, "needs a number"},
		{"comparison with infinity", `status>"+Inf"`, base, ErrUnsupportedFeature, "needs a number"},
		{"comparison with an exponent", `status>"1e3"`, base, ErrUnsupportedFeature, "needs a number"},
		{"or of numeric comparisons on two fields", `a>1 or b>2`, base, ErrUnsupportedFeature, "numeric comparisons on one field"},
		{"or of a numeric comparison and a nested numeric or", `a>5 or (b="x" or a>1)`, base, ErrUnsupportedFeature, "numeric comparisons on one field"},
		{"quoted segment with a space", `attrs."pod name"="p"`, base, ErrUnsupportedFeature, "not a valid Loki label name"},
		{"leading @", `@timestamp="x"`, base, ErrUnsupportedFeature, "not a valid Loki label name"},
		{"dash in a name", `k8s-pod="x"`, base, ErrUnsupportedFeature, "not a valid Loki label name"},
		{"LogQL keyword as a name", `Pattern="x"`, base, ErrUnsupportedFeature, "LogQL keyword"},
		{"keyword in a select pipe", `| json`, base, ErrUnsupportedFeature, "LogQL keyword"},
		{"aggregation", `level=error | count() by service`, base, ErrAggregationNotSupported, "not supported"},
		{"no source selector", `x="1"`, LogQLTranslateOptions{}, ErrUnsupportedFeature, "matches the empty string"},
		{"selector that matches the empty string", `x="1"`, LogQLTranslateOptions{
			Selector: []LogQLMatcher{{Name: "app", Op: "=~", Value: ".*"}},
		}, ErrUnsupportedFeature, "matches the empty string"},
		{"hoisted matcher that matches the empty string", `app=""`, LogQLTranslateOptions{
			Selector: []LogQLMatcher{{Name: "env", Op: "!=", Value: "x"}}, StreamLabels: []string{"app"},
		}, ErrUnsupportedFeature, "matches the empty string"},
		{"invalid regex in the selector", ``, LogQLTranslateOptions{
			Selector: []LogQLMatcher{{Name: "app", Op: "=~", Value: "("}},
		}, ErrUnsupportedFeature, "invalid regex"},
		{"unknown parser", ``, LogQLTranslateOptions{Selector: base.Selector, Parser: "regexp"}, ErrUnsupportedFeature, "unsupported Loki parser"},
		{"syntax error", `level=`, base, ErrUnexpectedEnd, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := TranslateToLogQL(tc.query, tc.options)
			if result.Valid || result.Error == nil {
				t.Fatalf("TranslateToLogQL(%q) = %q, want an error", tc.query, result.Query)
			}
			if result.Error.Code != tc.code || !strings.Contains(result.Error.Message, tc.message) {
				t.Fatalf("TranslateToLogQL(%q) error = %s %q, want %s containing %q", tc.query, result.Error.Code, result.Error.Message, tc.code, tc.message)
			}
		})
	}
}

func TestTranslateToLogQLReportsConditions(t *testing.T) {
	result := TranslateToLogQL(`app="api" and status>=500`, LogQLTranslateOptions{
		Selector:     []LogQLMatcher{{Name: "service_name", Op: "=~", Value: ".+"}},
		StreamLabels: []string{"app"},
	})
	if !result.Valid {
		t.Fatalf("translate failed: %v", result.Error)
	}
	if strings.Join(result.FieldsUsed, ",") != "app,status" || len(result.Conditions) != 2 {
		t.Fatalf("fields %v conditions %+v", result.FieldsUsed, result.Conditions)
	}
}
