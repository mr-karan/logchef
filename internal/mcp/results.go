package mcp

import (
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryResult preserves the evidence and execution limits of a log query.
type QueryResult struct {
	Logs                   []map[string]any      `json:"logs"`
	Columns                []models.ColumnInfo   `json:"columns"`
	Stats                  models.QueryStats     `json:"stats"`
	QueryID                string                `json:"query_id"`
	RowCount               int                   `json:"row_count"`
	TeamID                 int                   `json:"team_id"`
	SourceID               int                   `json:"source_id"`
	StartTime              string                `json:"start_time,omitempty"`
	EndTime                string                `json:"end_time,omitempty"`
	GeneratedSQL           string                `json:"generated_sql,omitempty"`
	GeneratedQuery         string                `json:"generated_query,omitempty"`
	GeneratedQueryLanguage string                `json:"generated_query_language,omitempty"`
	Warnings               []models.QueryWarning `json:"warnings,omitempty"`
}

type HistogramResult struct {
	Granularity string                       `json:"granularity"`
	Data        []datasource.HistogramBucket `json:"data"`
	Notice      string                       `json:"notice,omitempty"`
}

// formatTime matches the encoding/json form of time.Time that the HTTP API
// returns.
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}
