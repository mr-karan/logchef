package querybuilder

import (
	"backend-v2/pkg/querybuilder/dsl"
	"fmt"
)

// LogChefQLBuilder builds queries from LogChef query language
type LogChefQLBuilder struct {
	TableName string
	Options   Options
	Query     string
}

// NewLogChefQLBuilder creates a new LogChefQLBuilder instance
func NewLogChefQLBuilder(tableRef string, opts Options, query string) *LogChefQLBuilder {
	return &LogChefQLBuilder{
		TableName: tableRef,
		Options:   opts,
		Query:     query,
	}
}

// Build implements the Builder interface for LogChefQLBuilder
func (b *LogChefQLBuilder) Build() (*Query, error) {
	// Parse the LogChefQL query
	conditions, err := dsl.ParseQuery(b.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogChefQL query: %w", err)
	}

	// Create a filter builder with the parsed conditions
	fb := NewFilterBuilder(b.TableName, b.Options, conditions)
	return fb.Build()
}
