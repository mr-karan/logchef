package querybuilder

import (
	"backend-v2/pkg/models"
	"time"
)

// Query represents a SQL query with its parameters
type Query struct {
	SQL  string
	Args []interface{}
}

// Builder is the interface implemented by query builders
type Builder interface {
	Build() (*Query, error)
}

// Options represents common query options
type Options struct {
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Sort      *models.SortOptions
}
