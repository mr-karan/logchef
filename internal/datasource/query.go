package datasource

import (
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

var ErrOperationNotSupported = errors.New("datasource operation not supported")

func unsupportedOperationError(sourceType models.SourceType, operation string) error {
	return fmt.Errorf("%s does not support %s: %w", sourceType, operation, ErrOperationNotSupported)
}

type QueryRequest struct {
	RawQuery     string
	Limit        int
	MaxLimit     int
	QueryTimeout *int
}

type HistogramRequest struct {
	Window       string
	Query        string
	GroupBy      string
	Timezone     string
	QueryTimeout *int
}

type HistogramBucket struct {
	Bucket     time.Time `json:"bucket"`
	LogCount   int       `json:"log_count"`
	GroupValue string    `json:"group_value,omitempty"`
}

type HistogramResult struct {
	Granularity string            `json:"granularity"`
	Data        []HistogramBucket `json:"data"`
}

type LogContextRequest struct {
	TargetTimestamp int64
	BeforeLimit     int
	AfterLimit      int
	BeforeOffset    int
	AfterOffset     int
	ExcludeBoundary bool
	QueryTimeout    *int
}

type LogContextResult struct {
	TargetTimestamp int64                    `json:"target_timestamp"`
	BeforeLogs      []map[string]interface{} `json:"before_logs"`
	TargetLogs      []map[string]interface{} `json:"target_logs"`
	AfterLogs       []map[string]interface{} `json:"after_logs"`
	Stats           models.QueryStats        `json:"stats"`
}

type AlertQueryRequest struct {
	Query        string
	QueryTimeout *int
}
