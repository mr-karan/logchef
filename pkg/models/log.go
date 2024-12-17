package models

import (
	"time"
)

// Log represents a log entry with dynamic fields
type Log map[string]interface{}

// Helper methods to access common fields if they exist
func (l Log) GetTimestamp() (time.Time, bool) {
	if ts, ok := l["timestamp"].(time.Time); ok {
		return ts, true
	}
	return time.Time{}, false
}

func (l Log) GetString(field string) (string, bool) {
	if val, ok := l[field].(string); ok {
		return val, true
	}
	return "", false
}

func (l Log) GetInt(field string) (int64, bool) {
	switch v := l[field].(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

// LogQueryParams represents query parameters for log search
type LogQueryParams struct {
	TableName    string     `json:"table_name"`
	StartTime    *time.Time `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	ServiceName  string     `json:"service_name"`
	Namespace    string     `json:"namespace"`
	SeverityText string     `json:"severity_text"`
	SearchQuery  string     `json:"search_query"`
	Limit        int        `json:"limit"`
	Offset       int        `json:"offset"`
	SQLQuery     string     `json:"sql_query"`
}

// LogResponse represents paginated log response
type LogResponse struct {
	Logs       []Log      `json:"logs"`
	TotalCount int        `json:"total_count"`
	HasMore    bool       `json:"has_more"`
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
}
