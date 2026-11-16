package datasource

import (
	"errors"
	"time"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

var ErrOperationNotSupported = errors.New("datasource operation not supported")

type QueryRequest struct {
	ExtraStreamFilters []string
	RawQuery           string
	StartTime          *time.Time
	EndTime            *time.Time
	Timezone           string
	Limit              int
	DefaultLimit       int
	MaxLimit           int
	MaxResponseBytes   int
	QueryTimeout       *int
	// Step is the request interval. Loki sends it as the step of a metric
	// range query; the other providers ignore it. Zero means none was sent.
	Step time.Duration
	// Aggregation describes the result of a compiled LogchefQL aggregation:
	// the column names, order and types, and the query's own limit. Nil for
	// a query that returns log rows.
	Aggregation *logchefql.AggregationInfo
	// AggregationMaxMemoryBytes bounds the memory of an aggregated query on
	// ClickHouse. 0 means DefaultAggregationMaxMemoryBytes.
	AggregationMaxMemoryBytes int64
}

// DefaultAggregationMaxMemoryBytes is the memory limit of an aggregated query
// when the caller sets none: 2 GiB.
const DefaultAggregationMaxMemoryBytes int64 = 2 << 30

type HistogramRequest struct {
	ExtraStreamFilters []string
	StartTime          *time.Time
	EndTime            *time.Time
	Window             string
	Query              string
	GroupBy            string
	Timezone           string
	QueryTimeout       *int
}

type HistogramBucket struct {
	Bucket     time.Time `json:"bucket"`
	LogCount   int       `json:"log_count"`
	GroupValue string    `json:"group_value,omitempty"`
	// IsOther is true when this row represents the synthetic remainder bucket
	// (groups beyond the top-N series limit folded together).
	IsOther bool `json:"is_other,omitempty"`
	// IsNull distinguishes a SQL NULL group from an ordinary string value.
	IsNull bool `json:"is_null,omitempty"`
}

type HistogramResult struct {
	Granularity string            `json:"granularity"`
	Data        []HistogramBucket `json:"data"`
	// Notice carries a non-fatal message (e.g. group-by series were capped to
	// a top-N set). Empty when there is nothing to surface.
	Notice string `json:"notice,omitempty"`
}

type AlertQueryRequest struct {
	Language models.QueryLanguage
	Query    string
	// At is the instant the evaluation stands for. The window is the
	// lookback that ends at At. Required.
	At              time.Time
	LookbackSeconds int
	QueryTimeout    *int
}

// LogContextRequest asks for logs surrounding a specific timestamp.
type LogContextRequest struct {
	TargetTimestamp int64      // Unix timestamp in milliseconds
	TargetTime      *time.Time // Optional precise target time; takes precedence over TargetTimestamp
	BeforeLimit     int
	AfterLimit      int
	BeforeOffset    int
	AfterOffset     int
	ExcludeBoundary bool
	QueryTimeout    *int
	// StreamLabels are the target row's stream labels. Loki reads context
	// from that one stream; ClickHouse ignores them.
	StreamLabels map[string]string
}
