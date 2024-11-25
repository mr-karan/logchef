package models

import (
	"time"
)

// Log represents a log entry from Clickhouse
type Log struct {
	ID             string            `json:"id"`
	Timestamp      time.Time         `json:"timestamp"`
	TraceID        string            `json:"trace_id"`
	SpanID         string            `json:"span_id"`
	TraceFlags     uint32            `json:"trace_flags"`
	SeverityText   string            `json:"severity_text"`
	SeverityNumber int32             `json:"severity_number"`
	ServiceName    string            `json:"service_name"`
	Namespace      string            `json:"namespace"`
	Body           string            `json:"body"`
	LogAttributes  map[string]string `json:"log_attributes"`
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
}

// LogResponse represents paginated log response
type LogResponse struct {
	Logs       []*Log `json:"logs"`
	TotalCount uint64 `json:"total_count"`
	HasMore    bool   `json:"has_more"`
}
