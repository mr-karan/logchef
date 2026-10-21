package metrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

// roleLabel returns the user's role, or "" for an unauthenticated request.
// Metrics never carry the user's identity: it is personal data and every user
// would add a series per endpoint.
func roleLabel(user *models.User) string {
	if user == nil {
		return ""
	}
	return string(user.Role)
}

// RecordHTTPRequest records HTTP request metrics, labelled by the caller's role only.
func RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration, responseSize int64, user *models.User) {
	labels := fmt.Sprintf(`logchef_http_requests_total{method=%q,endpoint=%q,status="%d",user_role=%q}`,
		method, endpoint, statusCode, roleLabel(user))
	metrics.GetOrCreateCounter(labels).Inc()

	durationLabels := fmt.Sprintf(`logchef_http_request_duration_seconds{method=%q,endpoint=%q}`, method, endpoint)
	metrics.GetOrCreateHistogram(durationLabels).Update(duration.Seconds())

	sizeLabels := fmt.Sprintf(`logchef_http_response_size_bytes{method=%q,endpoint=%q}`, method, endpoint)
	metrics.GetOrCreateHistogram(sizeLabels).Update(float64(responseSize))
}

// RecordHTTPError records HTTP error metrics
func RecordHTTPError(method, endpoint, errorType string, statusCode int, user *models.User) {
	labels := fmt.Sprintf(`logchef_http_errors_total{method=%q,endpoint=%q,error_type=%q,status="%d",user_role=%q}`,
		method, endpoint, errorType, statusCode, roleLabel(user))
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordAuthAttempt records authentication attempt metrics
func RecordAuthAttempt(method string, success bool, user *models.User) {
	result := "success"
	if !success {
		result = "failure"
	}

	labels := fmt.Sprintf(`logchef_auth_attempts_total{method=%q,result=%q,user_role=%q}`,
		method, result, roleLabel(user))
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordSessionOperation records session operation metrics
func RecordSessionOperation(operation string, success bool, user *models.User) {
	result := "success"
	if !success {
		result = "failure"
	}

	labels := fmt.Sprintf(`logchef_session_operations_total{operation=%q,result=%q,user_role=%q}`,
		operation, result, roleLabel(user))
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordAuthorizationFailure records authorization failure metrics
func RecordAuthorizationFailure(endpoint string, user *models.User, reason string) {
	labels := fmt.Sprintf(`logchef_authorization_failures_total{endpoint=%q,reason=%q,user_role=%q}`,
		endpoint, reason, roleLabel(user))
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordRateLimitRejection records a request rejected by a rate limiter.
// scope is "auth" (unauthenticated auth/token endpoints) or "query"
// (per-user query endpoints).
func RecordRateLimitRejection(scope string) {
	labels := fmt.Sprintf(`logchef_rate_limit_rejections_total{scope=%q}`, scope)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordDashboardCacheRequest records a dashboard result-cache lookup outcome.
// result is "hit", "miss", "coalesced", or "bypass".
func RecordDashboardCacheRequest(result string) {
	labels := fmt.Sprintf(`logchef_dashboard_cache_requests_total{result=%q}`, result)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordDashboardCacheEviction records an entry evicted from the dashboard
// result cache under the byte/entry budget.
func RecordDashboardCacheEviction() {
	metrics.GetOrCreateCounter("logchef_dashboard_cache_evictions_total").Inc()
}

// SetDashboardCacheBytes reports the current total bytes held by the dashboard
// result cache.
func SetDashboardCacheBytes(n int64) {
	metrics.GetOrCreateGauge("logchef_dashboard_cache_bytes", nil).Set(float64(n))
}

// SetDashboardCacheEntries reports the current number of entries in the
// dashboard result cache.
func SetDashboardCacheEntries(n int) {
	metrics.GetOrCreateGauge("logchef_dashboard_cache_entries", nil).Set(float64(n))
}

func IncrementActiveRequests() {
	metrics.GetOrCreateGauge("logchef_http_active_requests", nil).Inc()
}

func DecrementActiveRequests() {
	metrics.GetOrCreateGauge("logchef_http_active_requests", nil).Dec()
}

// Utility functions for query analysis

// DetermineQueryType analyzes the query string to determine its type
func DetermineQueryType(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))

	if strings.HasPrefix(query, "select") {
		return "select"
	}
	if strings.HasPrefix(query, "show") {
		return "show"
	}
	if strings.HasPrefix(query, "describe") || strings.HasPrefix(query, "desc") {
		return "describe"
	}
	if strings.HasPrefix(query, "explain") {
		return "explain"
	}
	if strings.HasPrefix(query, "insert") {
		return "insert"
	}
	if strings.HasPrefix(query, "create") {
		return "create"
	}
	if strings.HasPrefix(query, "drop") {
		return "drop"
	}
	if strings.HasPrefix(query, "alter") {
		return "alter"
	}

	return "other"
}

// DetermineErrorType determines the type of error from an error object
func DetermineErrorType(err error) string {
	if err == nil {
		return ""
	}

	errStr := strings.ToLower(err.Error())

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return "timeout"
	}
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "network") {
		return "connection"
	}
	if strings.Contains(errStr, "syntax") || strings.Contains(errStr, "parse") {
		return "syntax"
	}
	if strings.Contains(errStr, "permission") || strings.Contains(errStr, "access") {
		return "permission"
	}
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "missing") {
		return "not_found"
	}

	return "other"
}
