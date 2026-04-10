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
