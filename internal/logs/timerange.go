package logs

import (
	"fmt"
	"time"
)

func validateTimeRange(start, end *time.Time) error {
	if start == nil || end == nil {
		return fmt.Errorf("both start_time and end_time are required")
	}
	if start.After(*end) {
		return fmt.Errorf("start_time cannot be after end_time")
	}
	return nil
}
