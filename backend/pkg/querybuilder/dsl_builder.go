package querybuilder

import (
	"backend-v2/pkg/querybuilder/dsl"
	"fmt"
)

// LogchefQLBuilder builds queries from LogChef query language
type LogchefQLBuilder struct {
	TableName string
	Options   Options
	Query     string
}

// NewLogchefQLBuilder creates a new LogchefQLBuilder instance
func NewLogchefQLBuilder(tableName string, opts Options, query string) *LogchefQLBuilder {
	return &LogchefQLBuilder{
		TableName: tableName,
		Options:   opts,
		Query:     query,
	}
}

// Build implements the Builder interface for LogchefQLBuilder
func (b *LogchefQLBuilder) Build() (*Query, error) {
	// Parse LogchefQL query into filter groups
	filterGroups, err := dsl.ParseQuery(b.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogchefQL query: %w", err)
	}

	// Use FilterBuilder to build the actual query
	fb := NewFilterBuilder(b.TableName, b.Options, filterGroups)
	return fb.Build()
}
