package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var dashboardDecimalLiteral = regexp.MustCompile(`^([+-]?)(\d+)(?:\.(\d*))?(?:[eE]([+-]?\d{1,4}))?$`)

// Keep the grammar and bounds aligned with frontend/src/utils/exactNumber.ts.
func dashboardExact(raw json.RawMessage) (*big.Rat, bool) {
	literal := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &literal) != nil {
			return nil, false
		}
	}
	if len(literal) > 512 {
		return nil, false
	}
	m := dashboardDecimalLiteral.FindStringSubmatch(literal)
	if m == nil {
		return nil, false
	}
	exponent := 0
	if m[4] != "" {
		exponent, _ = strconv.Atoi(m[4])
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return new(big.Rat), true
	}
	trimmed := strings.TrimRight(digits, "0")
	scale := len(m[3]) - exponent - (len(digits) - len(trimmed))
	if len(trimmed) > 100 || scale < -400 || scale > 400 {
		return nil, false
	}
	coefficient, ok := new(big.Int).SetString(m[1]+trimmed, 10)
	if !ok {
		return nil, false
	}
	power := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(absDashboardScale(scale))), nil)
	if scale < 0 {
		return new(big.Rat).SetInt(coefficient.Mul(coefficient, power)), true
	}
	return new(big.Rat).SetFrac(coefficient, power), true
}
func absDashboardScale(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func dashboardPalette(color string) bool {
	return color == "green" || color == "amber" || color == "red" || color == "blue" || color == "purple" || color == "gray"
}
func dashboardColumnNames(names []string) bool {
	if len(names) == 0 || len(names) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, name := range names {
		if n := utf8.RuneCountInString(name); n < 1 || n > 128 || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
func validateDashboardPresentation(id string, v *dashboardVizV2) error {
	invalid := func(field string) error { return fmt.Errorf("panel %q has invalid viz.%s", id, field) }
	if v.ColorMode != nil && (v.Type != DashboardVizStat || (*v.ColorMode != "value" && *v.ColorMode != "background")) {
		return invalid("color_mode")
	}
	for _, validate := range []func(string, *dashboardVizV2) error{validateDashboardThresholds, validateDashboardRules, validateDashboardColumnFormats} {
		if err := validate(id, v); err != nil {
			return err
		}
	}
	return nil
}
func validateDashboardThresholds(id string, v *dashboardVizV2) error {
	invalid := func(field string) error { return fmt.Errorf("panel %q has invalid viz.%s", id, field) }
	if v.Thresholds != nil {
		if (v.Type != DashboardVizStat && v.Type != DashboardVizChart) || len(v.Thresholds) < 1 || len(v.Thresholds) > 10 {
			return invalid("thresholds")
		}
		var previous *big.Rat
		for i, step := range v.Thresholds {
			if !dashboardPalette(step.Color) {
				return invalid("thresholds.color")
			}
			if i == 0 {
				if !bytes.Equal(step.Value, []byte("null")) {
					return invalid("thresholds base value")
				}
				continue
			}
			value, ok := dashboardExact(step.Value)
			if !ok || (previous != nil && value.Cmp(previous) <= 0) {
				return invalid("thresholds order or value")
			}
			previous = value
		}
	}

	return nil
}
func validateDashboardRules(id string, v *dashboardVizV2) error {
	invalid := func(field string) error { return fmt.Errorf("panel %q has invalid viz.%s", id, field) }
	if v.Rules != nil {
		if v.Type != DashboardVizTable || len(v.Rules) > 20 {
			return invalid("rules")
		}
		for _, rule := range v.Rules {
			if !dashboardColumnNames(rule.Columns) || !dashboardPalette(rule.Color) || (rule.Scope != "cell" && rule.Scope != "row") {
				return invalid("rules")
			}
			switch rule.Op {
			case "empty", "not_empty":
				if len(rule.Value) != 0 {
					return invalid("rules.value")
				}
			case "contains", "not_contains":
				var text string
				if json.Unmarshal(rule.Value, &text) != nil {
					return invalid("rules.value")
				}
			case ">", ">=", "<", "<=":
				if _, ok := dashboardExact(rule.Value); !ok {
					return invalid("rules.value")
				}
			case "=", "!=":
				var text string
				if json.Unmarshal(rule.Value, &text) != nil {
					if _, ok := dashboardExact(rule.Value); !ok {
						return invalid("rules.value")
					}
				}
			default:
				return invalid("rules.op")
			}
		}
	}

	return nil
}
func validateDashboardColumnFormats(id string, v *dashboardVizV2) error {
	invalid := func(field string) error { return fmt.Errorf("panel %q has invalid viz.%s", id, field) }
	if v.ColumnFormats != nil {
		if v.Type != DashboardVizTable || len(v.ColumnFormats) > 100 {
			return invalid("column_formats")
		}
		for name, f := range v.ColumnFormats {
			if !dashboardColumnNames([]string{name}) {
				return invalid("column_formats name")
			}
			if err := validateDashboardColumnFormat(id, f); err != nil {
				return err
			}
		}
	}

	return nil
}
func validateDashboardColumnFormat(id string, f dashboardColumnFormatV2) error {
	invalid := func(field string) error { return fmt.Errorf("panel %q has invalid viz.%s", id, field) }
	number := &dashboardNumberFormatV2{Notation: f.Notation, Decimals: f.Decimals, Grouping: f.Grouping, Scale: f.Scale, Unit: f.Unit, Prefix: f.Prefix, Suffix: f.Suffix}
	if f.Kind != nil && *f.Kind == "time" {
		if f.Notation != nil || f.Decimals != nil || f.Grouping != nil || f.Scale != nil || f.Unit != nil || f.Prefix != nil || f.Suffix != nil {
			return invalid("column_formats time")
		}
		if f.Date != nil && !slices.Contains([]string{"iso", "medium", "none"}, *f.Date) {
			return invalid("column_formats.date")
		}
		if f.Time != nil && !slices.Contains([]string{"none", "minutes", "seconds", "millis"}, *f.Time) {
			return invalid("column_formats.time")
		}
	} else {
		if (f.Kind != nil && *f.Kind != "number") || f.Date != nil || f.Time != nil {
			return invalid("column_formats.kind")
		}
		if err := validateDashboardNumberFormat(id, &dashboardVizV2{Type: DashboardVizStat, Format: number}); err != nil {
			return err
		}
	}
	return nil
}
