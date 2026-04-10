package datasource

import "time"

type InspectionDetail struct {
	Key       string `json:"key,omitempty"`
	Label     string `json:"label"`
	Value     string `json:"value"`
	Monospace bool   `json:"monospace,omitempty"`
	Multiline bool   `json:"multiline,omitempty"`
}

type InspectionMetric struct {
	Key   string `json:"key,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

type SourceSchemaField struct {
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	IsNullable        bool    `json:"is_nullable,omitempty"`
	IsPrimaryKey      bool    `json:"is_primary_key,omitempty"`
	DefaultExpression string  `json:"default_expression,omitempty"`
	Comment           string  `json:"comment,omitempty"`
	Compressed        string  `json:"compressed,omitempty"`
	Uncompressed      string  `json:"uncompressed,omitempty"`
	CompressionRatio  float64 `json:"compression_ratio,omitempty"`
	AvgRowSize        float64 `json:"avg_row_size,omitempty"`
	RowCount          uint64  `json:"row_count,omitempty"`
}

type SourceSchemaInspection struct {
	Fields      []SourceSchemaField `json:"fields"`
	SortKeys    []string            `json:"sort_keys,omitempty"`
	CreateQuery string              `json:"create_query,omitempty"`
	TTL         string              `json:"ttl,omitempty"`
}

type IngestionBucket struct {
	Bucket time.Time `json:"bucket"`
	Rows   uint64    `json:"rows"`
}

type SourceActivity struct {
	Rows1h        uint64            `json:"rows_1h"`
	Rows24h       uint64            `json:"rows_24h"`
	Rows7d        uint64            `json:"rows_7d"`
	LatestTS      *time.Time        `json:"latest_ts,omitempty"`
	HourlyBuckets []IngestionBucket `json:"hourly_buckets"`
	DailyBuckets  []IngestionBucket `json:"daily_buckets"`
}

type SourceInspection struct {
	Details  []InspectionDetail      `json:"details,omitempty"`
	Storage  []InspectionMetric      `json:"storage,omitempty"`
	Activity *SourceActivity         `json:"activity,omitempty"`
	Schema   *SourceSchemaInspection `json:"schema,omitempty"`
}
