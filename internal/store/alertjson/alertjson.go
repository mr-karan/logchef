// Package alertjson holds the JSON (un)marshalling shared by both store backends
// for the alert columns stored as JSON text (labels, annotations, recipient IDs,
// webhook URLs, history payloads). Only the null-type extraction differs between
// backends (sql.NullString vs pgtype.Text); the encoding logic lives here.
package alertjson

import "encoding/json"

// Encode returns the JSON encoding of v, or "" when empty is true — so an absent
// collection is stored as NULL/empty rather than "null"/"[]"/"{}".
func Encode[T any](v T, empty bool) (string, error) {
	if empty {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Decode unmarshals a stored JSON string into T. An empty string yields the zero
// value of T with no error (the column was NULL/unset).
func Decode[T any](raw string) (T, error) {
	var out T
	if raw == "" {
		return out, nil
	}
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}
