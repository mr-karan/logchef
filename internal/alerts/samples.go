package alerts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// maxGroupsPerEvaluation bounds the groups one evaluation reads. The
	// ClickHouse provider caps an alert result at the same number of rows.
	maxGroupsPerEvaluation = 100
	// A group key longer than this is stored as its SHA-256.
	maxGroupKeyBytes = 512
	// maxGroupValueChars cuts each value of a stored group.
	maxGroupValueChars = 256
)

// SamplesFromResult turns a query result into one sample per group. An
// ungrouped rule reads the first column of the first row. A null value is no
// sample, so the no-data policy decides: the aggregate had no input, such as
// max() over no row (RFC 0005). VictoriaLogs, and so LogchefQL and LogsQL,
// write that null as NaN or ""; ClickHouse SQL returns NULL, and any other
// text there stays an error.
func SamplesFromResult(result *models.QueryResult, groupBy []string, language models.QueryLanguage) ([]Sample, error) {
	isNull := isNullValue
	if language == models.QueryLanguageClickHouseSQL {
		isNull = isNullCell
	}
	if len(groupBy) == 0 {
		return ungroupedSamples(result, isNull)
	}
	return groupedSamples(result, groupBy, isNull)
}

// ungroupedSamples reads the first column of the first row. No rows is no sample.
func ungroupedSamples(result *models.QueryResult, isNull func(any) bool) ([]Sample, error) {
	if result == nil || len(result.Logs) == 0 {
		return nil, nil
	}
	if len(result.Columns) > 0 && isNull(result.Logs[0][result.Columns[0].Name]) {
		return nil, nil
	}
	value, err := util.ExtractFirstNumeric(result)
	if err != nil {
		return nil, err
	}
	return []Sample{{Value: value}}, nil
}

// groupedSamples reads one sample per row. The group columns are the names in
// groupBy; the value is the column named value, otherwise the first column
// that is not a group column. A cut result is an error, never a partial set
// of groups, because a missing group would read as resolved.
func groupedSamples(result *models.QueryResult, groupBy []string, isNull func(any) bool) ([]Sample, error) {
	if result == nil || len(result.Logs) == 0 {
		return nil, nil
	}
	if resultCut(result) || len(result.Logs) > maxGroupsPerEvaluation {
		return nil, fmt.Errorf("the query returned more than %d groups. Add a HAVING clause, or a LIMIT with an ORDER BY, so that it returns at most %d rows", maxGroupsPerEvaluation, maxGroupsPerEvaluation)
	}
	declared := make(map[string]struct{}, len(result.Columns))
	for _, column := range result.Columns {
		declared[column.Name] = struct{}{}
	}
	for _, column := range groupBy {
		if _, ok := declared[column]; !ok {
			return nil, fmt.Errorf("the query result has no column %q named in group_by", column)
		}
	}
	valueColumn, err := sampleValueColumn(result.Columns, groupBy)
	if err != nil {
		return nil, err
	}

	samples := make([]Sample, 0, len(result.Logs))
	seen := make(map[string]struct{}, len(result.Logs))
	for _, row := range result.Logs {
		values := make([]string, len(groupBy))
		for i, column := range groupBy {
			// A backend may leave out a label that is empty for one series.
			values[i] = groupValue(row[column])
		}
		group := groupLabels(groupBy, values)
		key, err := groupKey(values)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("the query returned more than one row for the group %s. Group by every column named in group_by", formatGroup(group))
		}
		seen[key] = struct{}{}
		if isNull(row[valueColumn]) {
			continue
		}
		value, err := util.NumericValue(row[valueColumn])
		if err != nil {
			return nil, fmt.Errorf("the value of group %s: %w", formatGroup(group), err)
		}
		samples = append(samples, Sample{GroupKey: key, Group: group, Value: value})
	}
	return samples, nil
}

// isNullCell reports a SQL NULL: no value, or a nil pointer.
func isNullCell(raw any) bool {
	value := reflect.ValueOf(raw)
	return !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil())
}

// isNullValue reports a null cell, or the NaN or empty text that
// VictoriaLogs returns for an aggregate without input.
func isNullValue(raw any) bool {
	if isNullCell(raw) {
		return true
	}
	text, ok := raw.(string)
	return ok && (text == "" || text == "NaN")
}

// resultCut reports a result the backend may have cut. A provider that does
// not report truncation leaves the state empty.
func resultCut(result *models.QueryResult) bool {
	state := result.Stats.Truncation
	return result.Stats.Truncated || (state != "" && state != models.TruncationNone)
}

func sampleValueColumn(columns []models.ColumnInfo, groupBy []string) (string, error) {
	first := ""
	for _, column := range columns {
		if slices.Contains(groupBy, column.Name) {
			continue
		}
		if column.Name == "value" {
			return column.Name, nil
		}
		if first == "" {
			first = column.Name
		}
	}
	if first == "" {
		return "", fmt.Errorf("the query result has no value column besides the group_by columns")
	}
	return first, nil
}

// groupKey is the JSON array of the group values in group_by order, or its
// SHA-256 in hex when that is longer than maxGroupKeyBytes.
func groupKey(values []string) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("failed to encode group key: %w", err)
	}
	if len(encoded) > maxGroupKeyBytes {
		sum := sha256.Sum256(encoded)
		return hex.EncodeToString(sum[:]), nil
	}
	return string(encoded), nil
}

func groupLabels(groupBy, values []string) map[string]string {
	group := make(map[string]string, len(groupBy))
	for i, column := range groupBy {
		group[column] = cutRunes(values[i], maxGroupValueChars)
	}
	return group
}

func groupValue(raw any) string {
	value := reflect.ValueOf(raw)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return ""
	}
	switch typed := value.Interface().(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(typed)
	}
}

func cutRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

// formatGroup renders a group as "a=1, b=2" in key order.
func formatGroup(group map[string]string) string {
	keys := make([]string, 0, len(group))
	for key := range group {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key + "=" + group[key]
	}
	return strings.Join(parts, ", ")
}
