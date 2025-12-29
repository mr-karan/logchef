// Package victorialogs provides a VictoriaLogs backend client implementation.
package victorialogs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/backends"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryResponse represents the parsed response from VictoriaLogs query endpoints.
type QueryResponse struct {
	Logs    []map[string]interface{}
	Columns []models.ColumnInfo
	Stats   models.QueryStats
}

// HitsResponse represents the response from /select/logsql/hits endpoint.
type HitsResponse struct {
	Hits []HitsBucket `json:"hits"`
}

// HitsBucket represents a single bucket in the hits response.
type HitsBucket struct {
	Fields     map[string]string `json:"fields"`
	Timestamps []string          `json:"timestamps"`
	Values     []uint64          `json:"values"`
	Total      uint64            `json:"total"`
}

// FieldNamesResponse represents the response from /select/logsql/field_names endpoint.
// VictoriaLogs returns: {"values": [{"value": "_msg", "hits": 1033300623}, ...]}
type FieldNamesResponse struct {
	Values []ValueWithHits `json:"values"`
}

// ValueWithHits represents a value with its hit count.
// Used by both field_names and field_values endpoints.
type ValueWithHits struct {
	Value string `json:"value"`
	Hits  uint64 `json:"hits"`
}

// FieldValuesResponse represents the response from /select/logsql/field_values endpoint.
// VictoriaLogs returns: {"values": [{"value": "host-1", "hits": 69426656}, ...]}
type FieldValuesResponse struct {
	Values []ValueWithHits `json:"values"`
}

// parseJSONLResponse parses a JSONL (newline-delimited JSON) response from VictoriaLogs.
// VictoriaLogs returns query results as JSONL where each line is a JSON object representing a log entry.
func parseJSONLResponse(reader io.Reader) (*QueryResponse, error) {
	result := &QueryResponse{
		Logs:    make([]map[string]interface{}, 0),
		Columns: make([]models.ColumnInfo, 0),
	}

	// Track seen columns to build schema
	seenColumns := make(map[string]bool)

	scanner := bufio.NewScanner(reader)
	// Increase buffer size for potentially large log lines
	const maxScanTokenSize = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxScanTokenSize)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			// Skip malformed lines but log them
			continue
		}

		// Track columns from this log entry
		for key := range logEntry {
			if !seenColumns[key] {
				seenColumns[key] = true
				result.Columns = append(result.Columns, models.ColumnInfo{
					Name: key,
					Type: inferVictoriaLogsType(logEntry[key]),
				})
			}
		}

		result.Logs = append(result.Logs, logEntry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning response: %w", err)
	}

	result.Stats.RowsRead = len(result.Logs)

	return result, nil
}

// inferVictoriaLogsType infers the type of a value from VictoriaLogs.
// VictoriaLogs is schemaless, so we infer types from actual values.
func inferVictoriaLogsType(value interface{}) string {
	switch v := value.(type) {
	case string:
		// Check if it looks like a timestamp
		if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return "DateTime64"
		}
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return "DateTime"
		}
		return "String"
	case float64:
		// JSON numbers are always float64
		if v == float64(int64(v)) {
			return "Int64"
		}
		return "Float64"
	case bool:
		return "Bool"
	case nil:
		return "Nullable(String)"
	case []interface{}:
		return "Array(String)"
	case map[string]interface{}:
		return "JSON"
	default:
		return "String"
	}
}

// parseHitsResponse parses the response from /select/logsql/hits endpoint.
func parseHitsResponse(reader io.Reader) (*HitsResponse, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading hits response: %w", err)
	}

	var response HitsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parsing hits response: %w", err)
	}

	return &response, nil
}

// convertHitsToHistogram converts VictoriaLogs hits response to HistogramResult.
func convertHitsToHistogram(hits *HitsResponse, granularity string) (*backends.HistogramResult, error) {
	result := &backends.HistogramResult{
		Granularity: granularity,
		Data:        make([]backends.HistogramData, 0),
	}

	if len(hits.Hits) == 0 {
		return result, nil
	}

	// VictoriaLogs returns hits grouped by fields (for grouped histograms)
	// or a single hit entry for non-grouped histograms
	for _, hit := range hits.Hits {
		groupValue := ""
		// Extract group value from fields if present
		for _, v := range hit.Fields {
			groupValue = v
			break
		}

		// Parse timestamps and values
		for i, ts := range hit.Timestamps {
			if i >= len(hit.Values) {
				break
			}

			// Parse timestamp - VictoriaLogs uses RFC3339 format
			bucket, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				// Try Unix timestamp
				if unixTs, parseErr := strconv.ParseInt(ts, 10, 64); parseErr == nil {
					bucket = time.Unix(unixTs, 0)
				} else {
					continue // Skip unparseable timestamps
				}
			}

			result.Data = append(result.Data, backends.HistogramData{
				Bucket:     bucket,
				LogCount:   int(hit.Values[i]),
				GroupValue: groupValue,
			})
		}
	}

	return result, nil
}

func parseFieldNamesResponse(reader io.Reader) (*FieldNamesResponse, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading field names response: %w", err)
	}

	var response FieldNamesResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parsing field names response: %w", err)
	}

	return &response, nil
}

func parseFieldValuesResponse(reader io.Reader) (*FieldValuesResponse, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading field values response: %w", err)
	}

	var response FieldValuesResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parsing field values response: %w", err)
	}

	return &response, nil
}

func convertFieldNamesToTableInfo(names *FieldNamesResponse) *backends.TableInfo {
	columns := make([]models.ColumnInfo, 0, len(names.Values))

	for _, entry := range names.Values {
		colType := "String"
		switch entry.Value {
		case "_time":
			colType = "DateTime64"
		case "_msg", "_stream", "_stream_id":
			colType = "String"
		}

		columns = append(columns, models.ColumnInfo{
			Name: entry.Value,
			Type: colType,
		})
	}

	return &backends.TableInfo{
		Database: "victorialogs",
		Name:     "logs",
		Engine:   "VictoriaLogs",
		Columns:  columns,
	}
}

func convertFieldValuesToResult(fieldName string, values *FieldValuesResponse) *backends.FieldValuesResult {
	result := &backends.FieldValuesResult{
		FieldName:        fieldName,
		FieldType:        "String",
		IsLowCardinality: false,
		Values:           make([]backends.FieldValueInfo, 0, len(values.Values)),
		TotalDistinct:    int64(len(values.Values)),
	}

	for _, entry := range values.Values {
		result.Values = append(result.Values, backends.FieldValueInfo{
			Value: entry.Value,
			Count: int64(entry.Hits),
		})
	}

	return result
}

// parseStatsFromHeaders extracts query statistics from response headers.
func parseStatsFromHeaders(headers map[string][]string) models.QueryStats {
	stats := models.QueryStats{}

	if rowsRead, ok := headers["X-Stats-Rows-Read"]; ok && len(rowsRead) > 0 {
		if n, err := strconv.Atoi(rowsRead[0]); err == nil {
			stats.RowsRead = n
		}
	}

	if bytesRead, ok := headers["X-Stats-Bytes-Read"]; ok && len(bytesRead) > 0 {
		if n, err := strconv.ParseInt(bytesRead[0], 10, 64); err == nil {
			stats.BytesRead = int(n)
		}
	}

	if execTime, ok := headers["X-Stats-Execution-Time-Seconds"]; ok && len(execTime) > 0 {
		if f, err := strconv.ParseFloat(execTime[0], 64); err == nil {
			stats.ExecutionTimeMs = f * 1000
		}
	}

	return stats
}
