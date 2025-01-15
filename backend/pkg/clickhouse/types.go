package clickhouse

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// ColumnType represents a ClickHouse column type
type ColumnType string

const (
	TypeDateTime64 ColumnType = "DateTime64"
	TypeDateTime   ColumnType = "DateTime"
	TypeDate       ColumnType = "Date"
	TypeDate32     ColumnType = "Date32"
	TypeUUID       ColumnType = "UUID"
	TypeString     ColumnType = "String"
	TypeUInt32     ColumnType = "UInt32"
	TypeInt32      ColumnType = "Int32"
	TypeUInt64     ColumnType = "UInt64"
	TypeInt64      ColumnType = "Int64"
	TypeFloat64    ColumnType = "Float64"
	TypeMap        ColumnType = "Map"
)

// TypeConverter handles converting ClickHouse types to Go types
type TypeConverter struct {
	// Add any necessary fields for configuration
}

// NewTypeConverter creates a new TypeConverter
func NewTypeConverter() *TypeConverter {
	return &TypeConverter{}
}

// ConvertValue converts a ClickHouse value to a Go value based on its type
func (c *TypeConverter) ConvertValue(value interface{}, columnType string) (interface{}, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case uuid.UUID:
		return v.String(), nil
	case *uuid.UUID:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case string, *string:
		return v, nil
	case uint32, *uint32, int32, *int32, uint64, *uint64, int64, *int64, float64, *float64:
		return v, nil
	case map[string]string, *map[string]string:
		return v, nil
	}

	// Handle string type conversion for datetime types
	if strVal, ok := value.(string); ok {
		switch ColumnType(columnType) {
		case TypeDateTime64, TypeDateTime:
			t, err := time.Parse("2006-01-02 15:04:05.999999999", strVal)
			if err != nil {
				// Try without nanoseconds
				t, err = time.Parse("2006-01-02 15:04:05", strVal)
				if err != nil {
					return nil, fmt.Errorf("failed to parse datetime: %v", err)
				}
			}
			return t, nil
		case TypeDate, TypeDate32:
			t, err := time.Parse("2006-01-02", strVal)
			if err != nil {
				return nil, fmt.Errorf("failed to parse date: %v", err)
			}
			return t, nil
		case TypeUUID:
			u, err := uuid.Parse(strVal)
			if err != nil {
				return nil, fmt.Errorf("failed to parse UUID: %v", err)
			}
			return u.String(), nil
		}
	}

	return value, nil
}

// isMapType checks if a type is a Map type and returns its key and value types
func (c *TypeConverter) isMapType(typeName string) (bool, string, string) {
	if !strings.HasPrefix(strings.ToLower(typeName), "map(") {
		return false, "", ""
	}

	// Remove "Map(" prefix and ")" suffix
	innerTypes := strings.TrimPrefix(typeName, "Map(")
	innerTypes = strings.TrimSuffix(innerTypes, ")")

	// Split into key and value types
	parts := strings.Split(innerTypes, ",")
	if len(parts) != 2 {
		return false, "", ""
	}

	keyType := strings.TrimSpace(parts[0])
	valueType := strings.TrimSpace(parts[1])

	return true, keyType, valueType
}

// getScanType returns the appropriate scan type for a ClickHouse column
func (c *TypeConverter) getScanType(columnType string) interface{} {
	// Check if it's a Map type first
	if isMap, keyType, valueType := c.isMapType(columnType); isMap {
		log.Printf("Map type detected: key=%s, value=%s", keyType, valueType)
		// For now, we only support Map(String, String) and Map(LowCardinality(String), String)
		return new(map[string]string)
	}

	// Normalize the type name and remove any parameters
	typeName := strings.ToLower(columnType)
	
	// Handle LowCardinality wrapper
	if strings.HasPrefix(typeName, "lowcardinality(") {
		// Extract the inner type
		innerType := strings.TrimPrefix(typeName, "lowcardinality(")
		innerType = strings.TrimSuffix(innerType, ")")
		typeName = innerType
	}

	// Remove other parameters
	if idx := strings.Index(typeName, "("); idx != -1 {
		typeName = typeName[:idx]
	}

	log.Printf("Normalized type name: %s -> %s", columnType, typeName)

	switch typeName {
	case "datetime64", "datetime":
		return new(time.Time)
	case "date32", "date":
		return new(time.Time)
	case "uuid":
		return new(uuid.UUID)
	case "string":
		return new(string)
	case "uint32":
		return new(uint32)
	case "int32":
		return new(int32)
	case "uint64":
		return new(uint64)
	case "int64":
		return new(int64)
	case "float64":
		return new(float64)
	default:
		log.Printf("Using default interface{} for type: %s (original: %s)", typeName, columnType)
		return new(interface{})
	}
}

// ScanRow scans a row into a map, handling type conversions
func (c *TypeConverter) ScanRow(rows driver.Rows) (map[string]interface{}, error) {
	// Get column information
	columnTypes := rows.ColumnTypes()

	// Create a slice to hold the values
	dest := make([]interface{}, len(columnTypes))
	for i, col := range columnTypes {
		typeName := col.DatabaseTypeName()
		scanType := c.getScanType(typeName)
		log.Printf("Column %s: type=%s, scanType=%T", col.Name(), typeName, scanType)
		dest[i] = scanType
	}

	// Scan the row into the values
	if err := rows.Scan(dest...); err != nil {
		return nil, fmt.Errorf("error scanning row: %v", err)
	}

	// Create result map
	result := make(map[string]interface{})
	for i, col := range columnTypes {
		var val interface{}
		colName := col.Name()
		typeName := col.DatabaseTypeName()

		log.Printf("Processing column %s: type=%s, value=%v", colName, typeName, dest[i])

		switch v := dest[i].(type) {
		case *interface{}:
			val = *v
		case *string:
			if v != nil {
				val = *v
			}
		case *uuid.UUID:
			if v != nil {
				val = v.String()
			}
		case *time.Time:
			if v != nil {
				val = *v
			}
		case *uint32:
			if v != nil {
				val = *v
			}
		case *int32:
			if v != nil {
				val = *v
			}
		case *uint64:
			if v != nil {
				val = *v
			}
		case *int64:
			if v != nil {
				val = *v
			}
		case *float64:
			if v != nil {
				val = *v
			}
		case *map[string]string:
			if v != nil {
				val = *v
			}
		default:
			log.Printf("Unexpected type for column %s: %T", colName, v)
			val = v
		}

		if val == nil {
			result[colName] = nil
			continue
		}

		// Convert the value based on column type
		converted, err := c.ConvertValue(val, typeName)
		if err != nil {
			return nil, fmt.Errorf("error converting value for column %s: %v", colName, err)
		}
		result[colName] = converted
	}

	return result, nil
}
