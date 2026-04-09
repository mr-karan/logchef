package datasource

import (
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

type FieldValueInfo struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type FieldValuesResult struct {
	FieldName        string           `json:"field_name"`
	FieldType        string           `json:"field_type"`
	IsLowCardinality bool             `json:"is_low_cardinality"`
	Values           []FieldValueInfo `json:"values"`
	TotalDistinct    int64            `json:"total_distinct"`
}

type FieldValuesRequest struct {
	FieldName      string
	FieldType      string
	Language       models.QueryLanguage
	TimestampField string
	StartTime      time.Time
	EndTime        time.Time
	Timezone       string
	Limit          int
	Timeout        *int
	QueryText      string
}

type AllFieldValuesRequest struct {
	Language       models.QueryLanguage
	TimestampField string
	StartTime      time.Time
	EndTime        time.Time
	Timezone       string
	Limit          int
	Timeout        *int
	QueryText      string
}

type AllFieldValuesResult map[string]*FieldValuesResult

type SourceExtendedColumnInfo struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	IsNullable        bool   `json:"is_nullable"`
	IsPrimaryKey      bool   `json:"is_primary_key"`
	DefaultExpression string `json:"default_expression,omitempty"`
	Comment           string `json:"comment,omitempty"`
}

type SourceTableInfo struct {
	Database     string                     `json:"database"`
	Name         string                     `json:"name"`
	Engine       string                     `json:"engine"`
	EngineParams []string                   `json:"engine_params,omitempty"`
	Columns      []models.ColumnInfo        `json:"columns,omitempty"`
	ExtColumns   []SourceExtendedColumnInfo `json:"ext_columns,omitempty"`
	SortKeys     []string                   `json:"sort_keys,omitempty"`
	CreateQuery  string                     `json:"create_query,omitempty"`
}

type TableStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Compressed   string  `json:"compressed"`
	Uncompressed string  `json:"uncompressed"`
	ComprRate    float64 `json:"compr_rate"`
	Rows         uint64  `json:"rows"`
	PartCount    uint64  `json:"part_count"`
}

type TableColumnStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Column       string  `json:"column"`
	Compressed   string  `json:"compressed"`
	Uncompressed string  `json:"uncompressed"`
	ComprRatio   float64 `json:"compr_ratio"`
	RowsCount    uint64  `json:"rows_count"`
	AvgRowSize   float64 `json:"avg_row_size"`
}

type IngestionBucket struct {
	Bucket time.Time `json:"bucket"`
	Rows   uint64    `json:"rows"`
}

type IngestionStats struct {
	Rows1h        uint64            `json:"rows_1h"`
	Rows24h       uint64            `json:"rows_24h"`
	Rows7d        uint64            `json:"rows_7d"`
	LatestTS      *time.Time        `json:"latest_ts,omitempty"`
	HourlyBuckets []IngestionBucket `json:"hourly_buckets"`
	DailyBuckets  []IngestionBucket `json:"daily_buckets"`
}

type SourceStats struct {
	TableStats  *TableStat        `json:"table_stats"`
	ColumnStats []TableColumnStat `json:"column_stats"`
	TableInfo   *SourceTableInfo  `json:"table_info"`
	Ingestion   *IngestionStats   `json:"ingestion_stats,omitempty"`
	TTL         string            `json:"ttl,omitempty"`
}
