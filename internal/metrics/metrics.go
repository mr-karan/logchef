package metrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

// Metrics system with meaningful labels for monitoring Logchef usage

// RecordHTTPRequest records HTTP request metrics with optional user context
func RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration, responseSize int64, user *models.User) {
	// Build labels with optional user context
	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_http_requests_total{method=%q,endpoint=%q,status="%d",user_email=%q,user_role=%q}`,
			method, endpoint, statusCode, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_http_requests_total{method=%q,endpoint=%q,status="%d",user_email="",user_role=""}`,
			method, endpoint, statusCode)
	}
	metrics.GetOrCreateCounter(labels).Inc()

	durationLabels := fmt.Sprintf(`logchef_http_request_duration_seconds{method=%q,endpoint=%q}`, method, endpoint)
	metrics.GetOrCreateHistogram(durationLabels).Update(duration.Seconds())

	sizeLabels := fmt.Sprintf(`logchef_http_response_size_bytes{method=%q,endpoint=%q}`, method, endpoint)
	metrics.GetOrCreateHistogram(sizeLabels).Update(float64(responseSize))
}

// RecordHTTPError records HTTP error metrics
func RecordHTTPError(method, endpoint, errorType string, statusCode int, user *models.User) {
	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_http_errors_total{method=%q,endpoint=%q,error_type=%q,status="%d",user_email=%q,user_role=%q}`,
			method, endpoint, errorType, statusCode, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_http_errors_total{method=%q,endpoint=%q,error_type=%q,status="%d",user_email="",user_role=""}`,
			method, endpoint, errorType, statusCode)
	}
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordQuery records query execution metrics with source context
func RecordQuery(source *models.Source, queryType string, success bool, duration time.Duration, rowsReturned int64, user *models.User) {
	result := "success"
	if !success {
		result = "failure"
	}

	// Query metrics with meaningful source and user labels
	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_query_total{source_id="%d",source_name=%q,database=%q,table=%q,query_type=%q,result=%q,user_email=%q,user_role=%q}`,
			source.ID, source.Name, source.Connection.Database, source.Connection.TableName, queryType, result, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_query_total{source_id="%d",source_name=%q,database=%q,table=%q,query_type=%q,result=%q,user_email="",user_role=""}`,
			source.ID, source.Name, source.Connection.Database, source.Connection.TableName, queryType, result)
	}
	metrics.GetOrCreateCounter(labels).Inc()

	// Duration histogram with source context
	durationLabels := fmt.Sprintf(`logchef_query_duration_seconds{source_name=%q,database=%q,table=%q}`,
		source.Name, source.Connection.Database, source.Connection.TableName)
	metrics.GetOrCreateHistogram(durationLabels).Update(duration.Seconds())

	if success && rowsReturned >= 0 {
		rowsLabels := fmt.Sprintf(`logchef_query_rows_returned{source_name=%q,database=%q,table=%q}`,
			source.Name, source.Connection.Database, source.Connection.TableName)
		metrics.GetOrCreateHistogram(rowsLabels).Update(float64(rowsReturned))
	}
}

// RecordQueryTimeout records query timeout metrics
func RecordQueryTimeout(source *models.Source, queryType string) {
	labels := fmt.Sprintf(`logchef_query_timeouts_total{source_id="%d",source_name=%q,database=%q,table=%q,query_type=%q}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, queryType)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordQueryError records query error metrics
func RecordQueryError(source *models.Source, errorType string) {
	labels := fmt.Sprintf(`logchef_query_errors_total{source_id="%d",source_name=%q,database=%q,table=%q,error_type=%q}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, errorType)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordClickHouseConnectionStatus sets connection status for a source
func RecordClickHouseConnectionStatus(source *models.Source, healthy bool) {
	status := 0.0
	if healthy {
		status = 1.0
	}

	labels := fmt.Sprintf(`logchef_clickhouse_connection_status{source_id="%d",source_name=%q,database=%q,table=%q,host=%q}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, source.Connection.Host)
	metrics.GetOrCreateGauge(labels, nil).Set(status)
}

// RecordClickHouseValidation records connection validation metrics
func RecordClickHouseValidation(source *models.Source, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	labels := fmt.Sprintf(`logchef_clickhouse_connection_validation_total{source_id="%d",source_name=%q,database=%q,table=%q,host=%q,result=%q}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, source.Connection.Host, result)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordClickHouseReconnection records reconnection attempts
func RecordClickHouseReconnection(source *models.Source, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	labels := fmt.Sprintf(`logchef_clickhouse_reconnections_total{source_id="%d",source_name=%q,database=%q,table=%q,host=%q,result=%q}`,
		source.ID, source.Name, source.Connection.Database, source.Connection.TableName, source.Connection.Host, result)
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordAuthAttempt records authentication attempt metrics
func RecordAuthAttempt(method string, success bool, user *models.User) {
	result := "success"
	if !success {
		result = "failure"
	}

	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_auth_attempts_total{method=%q,result=%q,user_email=%q,user_role=%q}`,
			method, result, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_auth_attempts_total{method=%q,result=%q,user_email="",user_role=""}`,
			method, result)
	}
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordSessionOperation records session operation metrics
func RecordSessionOperation(operation string, success bool, user *models.User) {
	result := "success"
	if !success {
		result = "failure"
	}

	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_session_operations_total{operation=%q,result=%q,user_email=%q,user_role=%q}`,
			operation, result, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_session_operations_total{operation=%q,result=%q,user_email="",user_role=""}`,
			operation, result)
	}
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordAuthorizationFailure records authorization failure metrics
func RecordAuthorizationFailure(endpoint string, user *models.User, reason string) {
	var labels string
	if user != nil {
		labels = fmt.Sprintf(`logchef_authorization_failures_total{endpoint=%q,reason=%q,user_email=%q,user_role=%q}`,
			endpoint, reason, user.Email, string(user.Role))
	} else {
		labels = fmt.Sprintf(`logchef_authorization_failures_total{endpoint=%q,reason=%q,user_email="",user_role=""}`,
			endpoint, reason)
	}
	metrics.GetOrCreateCounter(labels).Inc()
}

// RecordRateLimitRejection records a request rejected by a rate limiter.
// scope is "auth" (unauthenticated auth/token endpoints) or "query"
// (per-user query endpoints).
func RecordRateLimitRejection(scope string) {
	labels := fmt.Sprintf(`logchef_rate_limit_rejections_total{scope=%q}`, scope)
	metrics.GetOrCreateCounter(labels).Inc()
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

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// trimAndLower trims whitespace and converts to lowercase
func trimAndLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
