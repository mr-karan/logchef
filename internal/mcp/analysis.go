package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// Composite analysis tools that orchestrate several core calls.

// --- Input schemas ---

type CompareWindowsParams struct {
	TeamID       int    `json:"team_id" jsonschema:"Team ID"`
	SourceID     int    `json:"source_id" jsonschema:"Source ID"`
	Query        string `json:"query" jsonschema:"LogchefQL filter expression to run in both windows"`
	Window1Start string `json:"window1_start" jsonschema:"Start time for window 1 (YYYY-MM-DD HH:MM:SS)"`
	Window1End   string `json:"window1_end" jsonschema:"End time for window 1 (YYYY-MM-DD HH:MM:SS)"`
	Window2Start string `json:"window2_start" jsonschema:"Start time for window 2 (YYYY-MM-DD HH:MM:SS)"`
	Window2End   string `json:"window2_end" jsonschema:"End time for window 2 (YYYY-MM-DD HH:MM:SS)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max rows per window (default 100)"`
	Timezone     string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
}

type TopValuesParams struct {
	TeamID    int      `json:"team_id" jsonschema:"Team ID"`
	SourceID  int      `json:"source_id" jsonschema:"Source ID"`
	Fields    []string `json:"fields" jsonschema:"List of field names to get top values for"`
	StartTime string   `json:"start_time" jsonschema:"Start time (RFC3339)"`
	EndTime   string   `json:"end_time" jsonschema:"End time (RFC3339)"`
	Limit     int      `json:"limit,omitempty" jsonschema:"Max values per field (default 10 max 50)"`
}

// --- Output schemas ---

type CompareWindowsResult struct {
	Window1 WindowResult `json:"window1" jsonschema:"Results from time window 1"`
	Window2 WindowResult `json:"window2" jsonschema:"Results from time window 2"`
	Delta   DeltaResult  `json:"delta" jsonschema:"Difference between the two windows"`
}

type WindowResult struct {
	Start     string `json:"start" jsonschema:"Window start time"`
	End       string `json:"end" jsonschema:"Window end time"`
	RowCount  int    `json:"row_count" jsonschema:"Number of returned rows, not total matching logs"`
	Truncated bool   `json:"truncated" jsonschema:"Whether this window returned incomplete results"`
	QueryID   string `json:"query_id" jsonschema:"ClickHouse query ID"`
}

type DeltaResult struct {
	RowCountDiff    int      `json:"row_count_diff" jsonschema:"Row count difference (window2 - window1)"`
	RowCountPercent *float64 `json:"row_count_percent" jsonschema:"Percentage change in returned rows; null when either sample is incomplete or baseline is zero"`
}

type TopValuesResult struct {
	Fields []FieldTopValues `json:"fields" jsonschema:"Top values for each requested field"`
}

type FieldTopValues struct {
	FieldName string       `json:"field_name" jsonschema:"The field name"`
	Values    []FieldValue `json:"values" jsonschema:"Top values with counts"`
}

type FieldValue struct {
	Value string `json:"value" jsonschema:"The field value"`
	Count int64  `json:"count" jsonschema:"Number of occurrences"`
}

// --- Handlers ---

func (t *tools) handleCompareWindows(ctx context.Context, _ mcp.CallToolRequest, params CompareWindowsParams) (CompareWindowsResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	run1, err := t.runLogchefQL(ctx, params.TeamID, params.SourceID, core.LogchefQLQueryRequest{
		Query:     params.Query,
		Limit:     limit,
		StartTime: params.Window1Start,
		EndTime:   params.Window1End,
		Timezone:  params.Timezone,
	})
	if err != nil {
		return CompareWindowsResult{}, fmt.Errorf("window 1: %w", err)
	}

	run2, err := t.runLogchefQL(ctx, params.TeamID, params.SourceID, core.LogchefQLQueryRequest{
		Query:     params.Query,
		Limit:     limit,
		StartTime: params.Window2Start,
		EndTime:   params.Window2End,
		Timezone:  params.Timezone,
	})
	if err != nil {
		return CompareWindowsResult{}, fmt.Errorf("window 2: %w", err)
	}

	count1 := len(run1.result.Logs)
	count2 := len(run2.result.Logs)
	truncated1 := run1.result.Stats.Truncated
	truncated2 := run2.result.Stats.Truncated
	diff := count2 - count1
	var pct *float64
	if count1 > 0 && !truncated1 && !truncated2 && count1 < limit && count2 < limit {
		value := float64(diff) / float64(count1) * 100
		pct = &value
	}

	return CompareWindowsResult{
		Window1: WindowResult{
			Start: params.Window1Start, End: params.Window1End,
			RowCount: count1, QueryID: run1.queryID, Truncated: truncated1 || count1 >= limit,
		},
		Window2: WindowResult{
			Start: params.Window2Start, End: params.Window2End,
			RowCount: count2, QueryID: run2.queryID, Truncated: truncated2 || count2 >= limit,
		},
		Delta: DeltaResult{
			RowCountDiff:    diff,
			RowCountPercent: pct,
		},
	}, nil
}

const (
	// maxTopValuesFields caps the distinct fields one top_values call reads.
	maxTopValuesFields = 20
	// topValuesWorkers bounds the field queries one call runs at a time.
	topValuesWorkers = 4
)

func (t *tools) handleTopValues(ctx context.Context, _ mcp.CallToolRequest, params TopValuesParams) (TopValuesResult, error) {
	fields := uniqueFields(params.Fields)
	if len(fields) == 0 {
		return TopValuesResult{}, fmt.Errorf("at least one field is required")
	}
	if len(fields) > maxTopValuesFields {
		return TopValuesResult{}, fmt.Errorf("top_values accepts at most %d distinct fields, got %d", maxTopValuesFields, len(fields))
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return TopValuesResult{}, t.storeError("top values", err)
	}

	// The whole operation (schema plus every field query) holds one
	// preview admission slot, and at most topValuesWorkers field queries
	// run at once inside it.
	fieldResults := make([]FieldTopValues, len(fields))
	err = t.admit(ctx, QueryClassPreview, src, "top_values: "+strings.Join(fields, ", "), func(ctx context.Context, _ string) error {
		schema, err := t.sourceSchema(ctx, params.TeamID, params.SourceID)
		if err != nil {
			return err
		}
		fieldTypes := make(map[string]string, len(schema))
		for _, col := range schema {
			fieldTypes[col.Name] = col.Type
		}

		workers := make(chan struct{}, topValuesWorkers)
		var wg sync.WaitGroup
		for i, fieldName := range fields {
			fieldResults[i] = FieldTopValues{FieldName: fieldName}
			fieldType, ok := fieldTypes[fieldName]
			if !ok {
				continue
			}
			wg.Go(func() {
				workers <- struct{}{}
				defer func() { <-workers }()
				resp, err := t.fieldValues(ctx, src, fieldName, fieldType, params.StartTime, params.EndTime, limit)
				if err != nil {
					// Non-fatal: skip this field rather than fail the whole request.
					return
				}
				values := make([]FieldValue, len(resp.Values))
				for j, v := range resp.Values {
					values[j] = FieldValue{Value: v.Value, Count: v.Count}
				}
				fieldResults[i].Values = values
			})
		}
		wg.Wait()
		return nil
	})
	if err != nil {
		if _, ok := errors.AsType[*admissionError](err); ok {
			return TopValuesResult{}, t.queryError("top values", err)
		}
		return TopValuesResult{}, err
	}
	return TopValuesResult{Fields: fieldResults}, nil
}

// uniqueFields drops empty and repeated field names and keeps the order.
func uniqueFields(fields []string) []string {
	seen := make(map[string]struct{}, len(fields))
	unique := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, dup := seen[field]; dup || field == "" {
			continue
		}
		seen[field] = struct{}{}
		unique = append(unique, field)
	}
	return unique
}

func (t *tools) addAnalysisTools(s *server.MCPServer) {
	compareWindowsTool := mcp.NewTool("compare_windows",
		mcp.WithDescription("Compare returned log samples across two time windows. Runs the same LogchefQL filter in both windows. Row counts are not totals when a sample reaches its limit or is truncated. Percentage change is null for incomplete samples or a zero baseline."),
		mcp.WithInputSchema[CompareWindowsParams](),
		mcp.WithOutputSchema[CompareWindowsResult](),
		mcp.WithTitleAnnotation("Compare Time Windows"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(compareWindowsTool, mcp.NewStructuredToolHandler(t.handleCompareWindows))

	topValuesTool := mcp.NewTool("top_values",
		mcp.WithDescription("Get the top distinct values for multiple fields in one call. Fetches the schema to determine field types, then queries each field's top values. Useful for quickly exploring the dimensions of a log source."),
		mcp.WithInputSchema[TopValuesParams](),
		mcp.WithOutputSchema[TopValuesResult](),
		mcp.WithTitleAnnotation("Top Field Values"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(topValuesTool, mcp.NewStructuredToolHandler(t.handleTopValues))
}
