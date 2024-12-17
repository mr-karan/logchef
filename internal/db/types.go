package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// ColumnType represents a Clickhouse column type
type ColumnType struct {
	Name     string
	CHType   string // Clickhouse type
	GoType   any    // Go type for scanning
	JSONType string // Type to use for JSON marshaling
}

// GetColumnTypes returns the appropriate Go types for Clickhouse columns
func GetColumnTypes(columns []string, types []driver.ColumnType) []ColumnType {
	result := make([]ColumnType, len(columns))
	for i, col := range columns {
		chType := types[i].DatabaseTypeName()
		result[i] = ColumnType{
			Name:     col,
			CHType:   chType,
			GoType:   getGoType(chType),
			JSONType: getJSONType(chType),
		}
	}
	return result
}

func getGoType(chType string) any {
	// Extract base type for parameterized types like DateTime64(3)
	baseType := strings.Split(chType, "(")[0]

	switch baseType {
	case "UUID":
		return &uuid.UUID{}
	case "DateTime64", "DateTime":
		return &time.Time{}
	case "String", "LowCardinality(String)":
		return new(string)
	case "UInt32":
		return new(uint32)
	case "Int32":
		return new(int32)
	case "Map":
		if strings.Contains(chType, "Map(LowCardinality(String), String)") {
			return &map[string]string{}
		}
		return new(string) // fallback for unknown map types
	default:
		return new(string) // fallback
	}
}

func getJSONType(chType string) string {
	baseType := strings.Split(chType, "(")[0]

	switch baseType {
	case "UUID":
		return "string"
	case "DateTime64", "DateTime":
		return "datetime"
	case "String", "LowCardinality":
		return "string"
	case "UInt32", "Int32":
		return "number"
	case "Map":
		if strings.Contains(chType, "Map(LowCardinality(String), String)") {
			return "object"
		}
		return "string"
	default:
		return "string"
	}
}

// convertToJSON converts a scanned value to its JSON representation
func convertToJSON(value any, jsonType string) interface{} {
	if value == nil {
		return nil
	}

	switch jsonType {
	case "string":
		switch v := value.(type) {
		case *string:
			return *v
		case *uuid.UUID:
			return v.String()
		default:
			return fmt.Sprintf("%v", value)
		}
	case "datetime":
		if t, ok := value.(*time.Time); ok {
			return t.Format(time.RFC3339Nano)
		}
		return nil
	case "number":
		switch v := value.(type) {
		case *int32:
			return *v
		case *uint32:
			return *v
		default:
			return 0
		}
	case "object":
		if m, ok := value.(*map[string]string); ok {
			return *m
		}
		return map[string]string{}
	default:
		return fmt.Sprintf("%v", value)
	}
}
