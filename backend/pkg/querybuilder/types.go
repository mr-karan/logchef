package querybuilder

import (
	"backend-v2/pkg/models"
	"time"
)

// Query represents a built query ready for execution
type Query struct {
	SQL  string
	Args []interface{}
}

// Builder is the interface that all query builders must implement
type Builder interface {
	// Build constructs the final query
	Build() (*Query, error)
}

// Options contains common options for all query modes
type Options struct {
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Sort      *models.SortOptions
	Mode      models.QueryMode
}

// FilterBuilder builds queries using filter groups
type FilterBuilder struct {
	Options      Options
	FilterGroups []models.FilterGroup
	TableName    string
}

// LogChefQLBuilder builds queries from LogChef query language (to be implemented)
type LogChefQLBuilder struct {
	Options Options
	Query   string
}
