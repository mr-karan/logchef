package core

import (
	"sort"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func inferColumnType(value any) string {
	switch v := value.(type) {
	case nil:
		return "String"
	case bool:
		return "Bool"
	case int, int8, int16, int32, int64:
		return "Int64"
	case uint, uint8, uint16, uint32, uint64:
		return "UInt64"
	case float32, float64:
		return "Float64"
	case string:
		if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return "DateTime64"
		}
		return "String"
	case []any:
		return "Array"
	default:
		return "JSON"
	}
}

// ResultColumns returns the columns of a query result. When the provider
// returned none (VictoriaLogs), it lists the fields of the first 25 rows:
// the source's known columns first, in schema order, then the rest by name.
func ResultColumns(source *models.Source, result *models.QueryResult) []models.ColumnInfo {
	if result != nil && len(result.Columns) > 0 {
		return result.Columns
	}

	if result == nil || len(result.Logs) == 0 {
		return []models.ColumnInfo{}
	}

	sampledRows := result.Logs
	if len(sampledRows) > 25 {
		sampledRows = sampledRows[:25]
	}

	present := make(map[string]struct{}, len(result.Logs[0]))
	inferredTypes := make(map[string]string, len(result.Logs[0]))
	for _, row := range sampledRows {
		for key, value := range row {
			present[key] = struct{}{}
			if _, ok := inferredTypes[key]; !ok && value != nil {
				inferredTypes[key] = inferColumnType(value)
			}
		}
	}

	columns := make([]models.ColumnInfo, 0, len(present))
	if source != nil {
		for _, col := range source.Columns {
			if _, ok := present[col.Name]; !ok {
				continue
			}

			colType := col.Type
			if colType == "" {
				colType = inferredTypes[col.Name]
			}
			if colType == "" {
				colType = "String"
			}

			columns = append(columns, models.ColumnInfo{
				Name: col.Name,
				Type: colType,
			})
			delete(present, col.Name)
		}
	}

	extraNames := make([]string, 0, len(present))
	for name := range present {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)

	for _, name := range extraNames {
		colType := inferredTypes[name]
		if colType == "" {
			colType = "String"
		}
		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: colType,
		})
	}

	return columns
}
