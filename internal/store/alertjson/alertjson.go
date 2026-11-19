// Package alertjson holds the JSON (un)marshalling shared by both store backends
// for the alert columns stored as JSON text (labels, annotations, recipient IDs,
// webhook URLs, history payloads). Only the null-type extraction differs between
// backends (sql.NullString vs pgtype.Text); the encoding logic lives here.
package alertjson

import (
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/pkg/models"
)

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

// EncodeGroup returns the JSON object of an instance's group labels. The
// column is NOT NULL, so an ungrouped instance stores "{}".
func EncodeGroup(group map[string]string) (string, error) {
	if len(group) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(group)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EncodeSilence returns a silence's matchers as a JSON object ("{}" when it
// has none) and its weekly window as JSON ("" when it has none).
func EncodeSilence(silence *models.AlertSilence) (matchers, weekly string, err error) {
	if matchers, err = EncodeGroup(silence.Matchers); err != nil {
		return "", "", fmt.Errorf("failed to marshal silence matchers: %w", err)
	}
	if weekly, err = Encode(silence.Weekly, silence.Weekly == nil); err != nil {
		return "", "", fmt.Errorf("failed to marshal silence weekly window: %w", err)
	}
	return matchers, weekly, nil
}

// DecodeSilence reads the columns that EncodeSilence wrote.
func DecodeSilence(matchersJSON, weeklyJSON string) (map[string]string, *models.SilenceWeeklyWindow, error) {
	matchers, err := Decode[map[string]string](matchersJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode silence matchers: %w", err)
	}
	weekly, err := Decode[*models.SilenceWeeklyWindow](weeklyJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode silence weekly window: %w", err)
	}
	return matchers, weekly, nil
}
