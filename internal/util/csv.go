package util

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// CSVValue prints one result cell for a CSV file: NULL is empty, an instant
// is RFC 3339 with nanoseconds, and a composite value is its JSON. The log
// export and the report attachment both write cells with it.
func CSVValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case *time.Time:
		if v == nil {
			return ""
		}
		return v.Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}
