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
	StartTime    *time.Time
	EndTime      *time.Time
	Timezone     string
	Limit        int
	MaxLimit     int
	QueryTimeout *int
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
	Language     models.QueryLanguage
	Query        string
	QueryTimeout *int
}
