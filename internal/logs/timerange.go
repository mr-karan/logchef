package logs

import (
	"fmt"
	"time"
)

// TimeRange represents a time range for queries
type TimeRange struct {
	Start *time.Time `json:"start"`
	End   *time.Time `json:"end"`
}

// Validate checks if the time range is valid
func (tr *TimeRange) Validate() error {
	if tr.Start == nil || tr.End == nil {
		return fmt.Errorf("start and end times are required")
	}
	if tr.Start.After(*tr.End) {
		return fmt.Errorf("start time cannot be after end time")
	}
	return nil
}
