package clickhouse

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClickhouseType represents a Clickhouse data type
type ClickhouseType string

// Clickhouse data types
const (
	TypeBool      ClickhouseType = "Bool"
	TypeInt8      ClickhouseType = "Int8"
	TypeInt16     ClickhouseType = "Int16"
	TypeInt32     ClickhouseType = "Int32"
	TypeInt64     ClickhouseType = "Int64"
	TypeUInt8     ClickhouseType = "UInt8"
	TypeUInt16    ClickhouseType = "UInt16"
	TypeUInt32    ClickhouseType = "UInt32"
	TypeUInt64    ClickhouseType = "UInt64"
	TypeFloat32   ClickhouseType = "Float32"
	TypeFloat64   ClickhouseType = "Float64"
	TypeString    ClickhouseType = "String"
	TypeUUID      ClickhouseType = "UUID"
	TypeDateTime  ClickhouseType = "DateTime"
	TypeDateTime64 ClickhouseType = "DateTime64"
	TypeDate      ClickhouseType = "Date"
	TypeJSON      ClickhouseType = "JSON"
	TypeMap       ClickhouseType = "Map"
)

var (
	// Common Go types used for reflection
	boolType    = reflect.TypeOf(false)
	int8Type    = reflect.TypeOf(int8(0))
	int16Type   = reflect.TypeOf(int16(0))
	int32Type   = reflect.TypeOf(int32(0))
	int64Type   = reflect.TypeOf(int64(0))
	uint8Type   = reflect.TypeOf(uint8(0))
	uint16Type  = reflect.TypeOf(uint16(0))
	uint32Type  = reflect.TypeOf(uint32(0))
	uint64Type  = reflect.TypeOf(uint64(0))
	float32Type = reflect.TypeOf(float32(0))
	float64Type = reflect.TypeOf(float64(0))
	stringType  = reflect.TypeOf("")
	timeType    = reflect.TypeOf(time.Time{})
	uuidType    = reflect.TypeOf(uuid.UUID{})
	mapStringStringType = reflect.TypeOf(map[string]string{})
)

// TypeInfo holds information about a Clickhouse column type
type TypeInfo struct {
	Type        ClickhouseType
	Nullable    bool
	Array       bool
	LowCard     bool
	Map         bool
	KeyType     *TypeInfo
	ValueType   *TypeInfo
	Precision   int // For DateTime64
	ElementType *TypeInfo
}

// ParseTypeInfo parses a Clickhouse type string into TypeInfo
func ParseTypeInfo(typeStr string) (*TypeInfo, error) {
	info := &TypeInfo{}

	// Handle Map type
	if strings.HasPrefix(typeStr, "Map(") {
		info.Type = TypeMap
		info.Map = true
		mapTypes := strings.TrimPrefix(typeStr, "Map(")
		mapTypes = strings.TrimSuffix(mapTypes, ")")
		
		// Split into key and value types
		parts := strings.Split(mapTypes, ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid Map type format: %s", typeStr)
		}

		// Parse key type
		keyType := strings.TrimSpace(parts[0])
		keyInfo, err := ParseTypeInfo(keyType)
		if err != nil {
			return nil, fmt.Errorf("invalid Map key type: %w", err)
		}
		info.KeyType = keyInfo

		// Parse value type
		valueType := strings.TrimSpace(parts[1])
		valueInfo, err := ParseTypeInfo(valueType)
		if err != nil {
			return nil, fmt.Errorf("invalid Map value type: %w", err)
		}
		info.ValueType = valueInfo

		return info, nil
	}

	// Handle Nullable
	if strings.HasPrefix(typeStr, "Nullable(") {
		info.Nullable = true
		typeStr = strings.TrimPrefix(typeStr, "Nullable(")
		typeStr = strings.TrimSuffix(typeStr, ")")
	}

	// Handle Array
	if strings.HasPrefix(typeStr, "Array(") {
		info.Array = true
		typeStr = strings.TrimPrefix(typeStr, "Array(")
		typeStr = strings.TrimSuffix(typeStr, ")")
		elementInfo, err := ParseTypeInfo(typeStr)
		if err != nil {
			return nil, err
		}
		info.ElementType = elementInfo
		return info, nil
	}

	// Handle LowCardinality
	if strings.HasPrefix(typeStr, "LowCardinality(") {
		info.LowCard = true
		typeStr = strings.TrimPrefix(typeStr, "LowCardinality(")
		typeStr = strings.TrimSuffix(typeStr, ")")
	}

	// Handle DateTime64
	if strings.HasPrefix(typeStr, "DateTime64(") {
		info.Type = TypeDateTime64
		precStr := strings.TrimPrefix(typeStr, "DateTime64(")
		precStr = strings.TrimSuffix(precStr, ")")
		if _, err := fmt.Sscanf(precStr, "%d", &info.Precision); err != nil {
			return nil, fmt.Errorf("invalid DateTime64 precision: %s", precStr)
		}
		return info, nil
	}

	// Set base type
	info.Type = ClickhouseType(typeStr)
	return info, nil
}

// GetScanType returns the appropriate Go type for scanning values
func (t *TypeInfo) GetScanType() reflect.Type {
	if t.Map {
		// For Map types, we'll use map[string]string regardless of the key/value types
		// since Clickhouse will handle the conversion
		return reflect.PtrTo(mapStringStringType)
	}

	if t.Nullable {
		return reflect.PtrTo(t.getBaseType())
	}
	return t.getBaseType()
}

// getBaseType returns the base Go type for the Clickhouse type
func (t *TypeInfo) getBaseType() reflect.Type {
	if t.Array {
		elemType := t.ElementType.getBaseType()
		return reflect.SliceOf(elemType)
	}

	if t.Map {
		return mapStringStringType
	}

	switch t.Type {
	case TypeBool:
		return boolType
	case TypeInt8:
		return int8Type
	case TypeInt16:
		return int16Type
	case TypeInt32:
		return int32Type
	case TypeInt64:
		return int64Type
	case TypeUInt8:
		return uint8Type
	case TypeUInt16:
		return uint16Type
	case TypeUInt32:
		return uint32Type
	case TypeUInt64:
		return uint64Type
	case TypeFloat32:
		return float32Type
	case TypeFloat64:
		return float64Type
	case TypeString:
		return stringType
	case TypeUUID:
		return uuidType
	case TypeDateTime, TypeDateTime64:
		return timeType
	case TypeDate:
		return timeType
	default:
		return stringType // fallback
	}
}

// FormatValue formats a value according to its Clickhouse type
func (t *TypeInfo) FormatValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch t.Type {
	case TypeDateTime, TypeDateTime64, TypeDate:
		if t, ok := value.(time.Time); ok {
			return t.Format(time.RFC3339)
		}
	case TypeUUID:
		if id, ok := value.(uuid.UUID); ok {
			return id.String()
		}
	case TypeMap:
		// Map values are already in the correct format (map[string]string)
		return value
	}

	return value
}
