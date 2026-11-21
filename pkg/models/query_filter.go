package models

import (
	"fmt"
	"strings"
)

// QueryFilterOperator is how a filter pill compares its field.
type QueryFilterOperator string

const (
	QueryFilterIs       QueryFilterOperator = "is"
	QueryFilterIsOneOf  QueryFilterOperator = "is_one_of"
	QueryFilterContains QueryFilterOperator = "contains"
	QueryFilterExists   QueryFilterOperator = "exists"
	QueryFilterGT       QueryFilterOperator = "gt"
	QueryFilterGTE      QueryFilterOperator = "gte"
	QueryFilterLT       QueryFilterOperator = "lt"
	QueryFilterLTE      QueryFilterOperator = "lte"
)

// maxQueryFilters bounds the pills one saved query holds.
const maxQueryFilters = 100

// QueryFilter is one Explorer filter pill: a field filter kept beside the
// query text, which joins it with "and" when the query runs. Field is a
// LogchefQL field path. Values holds strings, numbers or booleans: one
// value, several for is_one_of, none for exists.
type QueryFilter struct {
	Field    string              `json:"field"`
	Operator QueryFilterOperator `json:"operator"`
	Values   []any               `json:"values"`
	Negate   bool                `json:"negate,omitempty"`
	Disabled bool                `json:"disabled,omitempty"`
	Label    string              `json:"label,omitempty"`
}

// Validate checks that the filter names a field, a known operator and the
// values that operator takes.
func (f QueryFilter) Validate() error {
	if strings.TrimSpace(f.Field) == "" {
		return fmt.Errorf("filter field is required")
	}
	for _, value := range f.Values {
		switch value.(type) {
		case string, float64, bool:
		default:
			return fmt.Errorf("filter on %q: values must be strings, numbers or booleans", f.Field)
		}
	}
	switch f.Operator {
	case QueryFilterExists:
		if len(f.Values) != 0 {
			return fmt.Errorf("filter on %q: exists takes no values", f.Field)
		}
	case QueryFilterIsOneOf:
		if len(f.Values) == 0 {
			return fmt.Errorf("filter on %q: is_one_of takes at least one value", f.Field)
		}
	case QueryFilterIs, QueryFilterContains, QueryFilterGT, QueryFilterGTE, QueryFilterLT, QueryFilterLTE:
		if len(f.Values) != 1 {
			return fmt.Errorf("filter on %q: %s takes one value", f.Field, f.Operator)
		}
	default:
		return fmt.Errorf("filter on %q: unknown operator %q", f.Field, f.Operator)
	}
	return nil
}

// ValidateQueryFilters checks every filter of a saved query.
func ValidateQueryFilters(filters []QueryFilter) error {
	if len(filters) > maxQueryFilters {
		return fmt.Errorf("at most %d filters are allowed", maxQueryFilters)
	}
	for _, filter := range filters {
		if err := filter.Validate(); err != nil {
			return err
		}
	}
	return nil
}
