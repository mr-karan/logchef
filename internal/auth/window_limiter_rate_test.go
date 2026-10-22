package auth

import (
	"strconv"
	"testing"
	"time"
)

func TestWindowLimiterAdmittedRate(t *testing.T) {
	for _, limit := range []int{120, 300} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			l := NewWindowLimiter(time.Minute, limit)
			total := 100 * limit
			admitted := 0
			for i := range total {
				if l.allowAt("key", time.Duration(i)*time.Minute/time.Duration(limit)) {
					admitted++
				}
			}
			t.Logf("admitted %d/%d requests at %d/min", admitted, total, limit)
			if admitted*100 < total*99 {
				t.Fatalf("admitted %d/%d requests, want at least 99%%", admitted, total)
			}
		})
	}
}

func TestWindowLimiterBurstReleasesWithinWindow(t *testing.T) {
	for _, offset := range []time.Duration{0, 10 * time.Second, time.Minute - time.Nanosecond} {
		t.Run(offset.String(), func(t *testing.T) {
			l := NewWindowLimiter(time.Minute, 120)
			for range 120 {
				if !l.allowAt("key", offset) {
					t.Fatal("burst exhausted early")
				}
			}
			if l.allowAt("key", offset) {
				t.Fatal("burst exceeded limit")
			}
			if !l.allowAt("key", offset+time.Minute+time.Millisecond) {
				t.Fatal("burst remained blocked beyond one window")
			}
		})
	}
}
