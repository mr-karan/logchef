package parser

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FlattenJSON converts a nested JSON object into a flat map with dot notation
// It's optimized for speed and memory usage
func FlattenJSON(jsonStr string) (map[string]string, error) {
	// Pre-allocate the result map with a reasonable size
	result := make(map[string]string, 32)

	// Parse JSON into a map
	var data map[string]interface{}
	d := json.NewDecoder(strings.NewReader(jsonStr))
	d.UseNumber() // Use Number instead of float64 for numbers
	if err := d.Decode(&data); err != nil {
		return nil, fmt.Errorf("error decoding JSON: %w", err)
	}

	// Process each key-value pair
	for k, v := range data {
		flattenValue(result, k, v)
	}

	return result, nil
}

// flattenValue recursively flattens a value into the result map
func flattenValue(result map[string]string, prefix string, v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		// For nested objects, recurse with prefix
		for k, v := range val {
			newKey := prefix + "." + k
			flattenValue(result, newKey, v)
		}
	case []interface{}:
		// For arrays, store as JSON string
		if jsonBytes, err := json.Marshal(val); err == nil {
			result[prefix] = string(jsonBytes)
		}
	case nil:
		result[prefix] = ""
	case json.Number:
		result[prefix] = val.String()
	case string:
		result[prefix] = val
	case bool:
		result[prefix] = fmt.Sprintf("%v", val)
	default:
		result[prefix] = fmt.Sprint(val)
	}
}

// BatchFlattenJSON processes multiple JSON strings in parallel
func BatchFlattenJSON(jsonStrings []string) ([]map[string]string, error) {
	// Pre-allocate result slice
	results := make([]map[string]string, len(jsonStrings))
	
	// Create error channel
	errCh := make(chan error, len(jsonStrings))
	
	// Process each JSON string in parallel
	for i, jsonStr := range jsonStrings {
		go func(idx int, js string) {
			flattened, err := FlattenJSON(js)
			if err != nil {
				errCh <- fmt.Errorf("error processing item %d: %w", idx, err)
				return
			}
			results[idx] = flattened
			errCh <- nil
		}(i, jsonStr)
	}

	// Collect errors
	for i := 0; i < len(jsonStrings); i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	return results, nil
}
