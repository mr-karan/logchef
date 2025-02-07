package clickhouse

import (
	"backend-v2/pkg/models"
	"time"
)

// LogQueryParams represents parameters for querying logs
type LogQueryParams struct {
	StartTime  time.Time
	EndTime    time.Time
	Limit      int
	Conditions []models.FilterCondition
	Sort       *models.SortOptions
	Mode       models.QueryMode
	RawSQL     string // Used only for raw_sql mode
	LogChefQL  string // Used only for logchefql mode
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   models.QueryStats        `json:"stats"`
	Columns []models.ColumnInfo      `json:"columns"`
}
