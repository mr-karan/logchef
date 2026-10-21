package util

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestCompareAlertThreshold(t *testing.T) {
	for _, tc := range []struct {
		operator            models.AlertThresholdOperator
		below, equal, above bool
	}{
		{models.AlertThresholdGreaterThan, false, false, true},
		{models.AlertThresholdGreaterThanOrEqual, false, true, true},
		{models.AlertThresholdLessThan, true, false, false},
		{models.AlertThresholdLessThanOrEqual, true, true, false},
		{models.AlertThresholdEqual, false, true, false},
		{models.AlertThresholdNotEqual, true, false, true},
		{"invalid", false, false, false},
	} {
		t.Run(string(tc.operator), func(t *testing.T) {
			for _, value := range []struct {
				value float64
				want  bool
			}{{9, tc.below}, {10, tc.equal}, {11, tc.above}} {
				if got := CompareAlertThreshold(value.value, 10, tc.operator); got != value.want {
					t.Errorf("CompareAlertThreshold(%v, 10, %s) = %t, want %t", value.value, tc.operator, got, value.want)
				}
			}
		})
	}
	if !CompareAlertThreshold(10+1e-10, 10, models.AlertThresholdEqual) {
		t.Error("equal comparison lost its tolerance")
	}
	if CompareAlertThreshold(10+1e-10, 10, models.AlertThresholdNotEqual) {
		t.Error("not-equal comparison lost its tolerance")
	}
}
