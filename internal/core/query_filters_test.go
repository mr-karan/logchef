package core

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestApplyQueryFilters(t *testing.T) {
	t.Parallel()

	region := models.QueryFilter{Field: "cloud.region", Operator: models.QueryFilterIs, Values: []any{"us-central1"}}
	cases := []struct {
		name     string
		text     string
		language models.QueryLanguage
		filters  []models.QueryFilter
		want     string
	}{
		{"no filters", `level="error"`, models.QueryLanguageLogchefQL, nil, `level="error"`},
		{"logchefql is", `level="error"`, models.QueryLanguageLogchefQL, []models.QueryFilter{region}, `level="error" and cloud.region="us-central1"`},
		{"logchefql empty query", "", models.QueryLanguageLogchefQL, []models.QueryFilter{region}, `cloud.region="us-central1"`},
		{"logchefql or is grouped", `a=1 or b=2`, models.QueryLanguageLogchefQL, []models.QueryFilter{region}, `(a=1 or b=2) and cloud.region="us-central1"`},
		{"logchefql before pipe", `a=1 | count() by b`, models.QueryLanguageLogchefQL, []models.QueryFilter{region}, `a=1 and cloud.region="us-central1" | count() by b`},
		{"logchefql negated one of", "", models.QueryLanguageLogchefQL, []models.QueryFilter{{Field: "status", Operator: models.QueryFilterIsOneOf, Values: []any{float64(500), float64(502)}, Negate: true}}, `status!=500 and status!=502`},
		{"logchefql one of", "", models.QueryLanguageLogchefQL, []models.QueryFilter{{Field: "status", Operator: models.QueryFilterIsOneOf, Values: []any{"a", "b"}}}, `(status="a" or status="b")`},
		{"logchefql negated range", "", models.QueryLanguageLogchefQL, []models.QueryFilter{{Field: "ms", Operator: models.QueryFilterGT, Values: []any{float64(100)}, Negate: true}}, `ms<=100`},
		{"logchefql exists", "", models.QueryLanguageLogchefQL, []models.QueryFilter{{Field: "user", Operator: models.QueryFilterExists}}, `user!=""`},
		{"disabled filter skipped", "x=1", models.QueryLanguageLogchefQL, []models.QueryFilter{{Field: "a", Operator: models.QueryFilterIs, Values: []any{"b"}, Disabled: true}}, "x=1"},
		{"logsql is", `error`, models.QueryLanguageLogsQL, []models.QueryFilter{region}, `error cloud.region:="us-central1"`},
		{"logsql star replaced", `*`, models.QueryLanguageLogsQL, []models.QueryFilter{region}, `cloud.region:="us-central1"`},
		{"logsql before pipes", `error | stats count()`, models.QueryLanguageLogsQL, []models.QueryFilter{region}, `error cloud.region:="us-central1" | stats count()`},
		{"logsql or grouped", `a:1 or b:2`, models.QueryLanguageLogsQL, []models.QueryFilter{region}, `(a:1 or b:2) cloud.region:="us-central1"`},
		{"logsql negated one of", "", models.QueryLanguageLogsQL, []models.QueryFilter{{Field: "level", Operator: models.QueryFilterIsOneOf, Values: []any{"info", "debug"}, Negate: true}}, `NOT level:in("info","debug")`},
		{"logsql contains escapes regex", "", models.QueryLanguageLogsQL, []models.QueryFilter{{Field: "msg", Operator: models.QueryFilterContains, Values: []any{"a.b"}}}, `msg:~"a\\.b"`},
		{"logsql quoted field", "", models.QueryLanguageLogsQL, []models.QueryFilter{{Field: `"http status"`, Operator: models.QueryFilterExists, Negate: true}}, `NOT "http status":*`},
		{"logsql range", "", models.QueryLanguageLogsQL, []models.QueryFilter{{Field: "ms", Operator: models.QueryFilterGTE, Values: []any{float64(1.5)}}}, `ms:>=1.5`},
		{"sql untouched", "SELECT 1", models.QueryLanguageClickHouseSQL, []models.QueryFilter{region}, "SELECT 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ApplyQueryFilters(tc.text, tc.language, tc.filters); got != tc.want {
				t.Errorf("ApplyQueryFilters() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSavedQueryContentAppliesFilters(t *testing.T) {
	t.Parallel()

	sq := &models.SavedQuery{
		ID:            1,
		QueryLanguage: models.QueryLanguageLogsQL,
		QueryContent:  `{"version":2,"limit":10,"content":"error","filters":[{"field":"level","operator":"is","values":["warn"],"negate":true}]}`,
	}
	content, err := savedQueryContent(sq)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := `error NOT level:="warn"`; content.Content != want {
		t.Errorf("content = %q, want %q", content.Content, want)
	}
}
