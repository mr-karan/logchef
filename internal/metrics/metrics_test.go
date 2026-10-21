package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestRecordRateLimitRejection(t *testing.T) {
	const label = `logchef_rate_limit_rejections_total{scope="auth"}`

	before := metrics.GetOrCreateCounter(label).Get()
	RecordRateLimitRejection("auth")
	after := metrics.GetOrCreateCounter(label).Get()

	if after != before+1 {
		t.Fatalf("counter = %d, want %d", after, before+1)
	}

	// A different scope must use a distinct series and not affect the first.
	queryBefore := metrics.GetOrCreateCounter(`logchef_rate_limit_rejections_total{scope="query"}`).Get()
	RecordRateLimitRejection("query")
	if got := metrics.GetOrCreateCounter(label).Get(); got != after {
		t.Fatalf("auth counter changed after recording query rejection: %d, want %d", got, after)
	}
	if got := metrics.GetOrCreateCounter(`logchef_rate_limit_rejections_total{scope="query"}`).Get(); got != queryBefore+1 {
		t.Fatalf("query counter = %d, want %d", got, queryBefore+1)
	}
}

func exposition() string {
	var buf bytes.Buffer
	metrics.WritePrometheus(&buf, false)
	return buf.String()
}

func TestMetricsCarryNoUserEmail(t *testing.T) {
	user := &models.User{Email: "alice@example.com", Role: models.UserRoleAdmin}
	source := &models.Source{ID: 901, Name: "labels-src"}

	RecordHTTPRequest("GET", "/labels-test", 200, time.Millisecond, 10, user)
	RecordHTTPError("GET", "/labels-test", "internal_error", 500, user)
	RecordQuery(source, "select", true, time.Millisecond, 3)
	RecordAuthAttempt("token", true, user)
	RecordSessionOperation("validate", true, user)
	RecordAuthorizationFailure("/labels-test", user, "insufficient_role")

	out := exposition()
	for _, banned := range []string{"user_email", "alice@example.com"} {
		if strings.Contains(out, banned) {
			t.Fatalf("exposition contains %q", banned)
		}
	}
}

func TestUserMetricsLabelSets(t *testing.T) {
	admin := &models.User{Email: "alice@example.com", Role: models.UserRoleAdmin}

	RecordHTTPRequest("GET", "/labelset", 200, time.Millisecond, 10, admin)
	RecordHTTPRequest("GET", "/labelset", 401, time.Millisecond, 10, nil)
	RecordAuthAttempt("labelset", true, admin)

	out := exposition()
	for _, want := range []string{
		`logchef_http_requests_total{method="GET",endpoint="/labelset",status="200",user_role="admin"} 1`,
		`logchef_http_requests_total{method="GET",endpoint="/labelset",status="401",user_role=""} 1`,
		`logchef_auth_attempts_total{method="labelset",result="success",user_role="admin"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q", want)
		}
	}
}

func TestRecordQueryLabelSet(t *testing.T) {
	source := &models.Source{ID: 902, Name: "labelset-src"}
	source.Connection.Database = "db"
	source.Connection.TableName = "tbl"

	RecordQuery(source, "select", true, time.Millisecond, 3)

	out := exposition()
	want := `logchef_query_total{source_id="902",source_name="labelset-src",database="db",table="tbl",query_type="select",result="success"} 1`
	if !strings.Contains(out, want) {
		t.Errorf("exposition missing %q", want)
	}
}

func TestRemoveSourceSeries(t *testing.T) {
	doomed := &models.Source{ID: 903, Name: "doomed"}
	kept := &models.Source{ID: 9030, Name: "kept"}

	for _, src := range []*models.Source{doomed, kept} {
		RecordQuery(src, "select", true, time.Millisecond, 1)
		RecordQueryTimeout(src, "select")
		RecordQueryError(src, "timeout")
		RecordClickHouseConnectionStatus(src, true)
		RecordClickHouseValidation(src, true)
		RecordClickHouseReconnection(src, true)
	}

	RemoveSourceSeries(doomed.ID)

	out := exposition()
	if strings.Contains(out, `source_id="903"`) {
		t.Errorf("series for removed source still exposed:\n%s", out)
	}
	if !strings.Contains(out, `source_id="9030"`) {
		t.Errorf("series for another source whose id shares a prefix was removed")
	}
}
