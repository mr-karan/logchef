package datasource

import (
	"errors"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

var ErrOperationNotSupported = errors.New("datasource operation not supported")

type QueryRequest struct {
	RawQuery         string
	StartTime        *time.Time
	EndTime          *time.Time
	Timezone         string
	Limit            int
	DefaultLimit     int
	MaxLimit         int
	MaxResponseBytes int
	QueryTimeout     *int
}

type HistogramRequest struct {
	StartTime    *time.Time
	EndTime      *time.Time
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

type AlertQueryRequest struct {
	Language        models.QueryLanguage
	Query           string
	LookbackSeconds int
	QueryTimeout    *int
}

// LogContextRequest asks for logs surrounding a specific timestamp.
type LogContextRequest struct {
	TargetTimestamp int64 // Unix timestamp in milliseconds
	BeforeLimit     int
	AfterLimit      int
	BeforeOffset    int
	AfterOffset     int
	ExcludeBoundary bool
	QueryTimeout    *int
}
