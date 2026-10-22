package auth

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func BenchmarkWindowLimiterAllocation(b *testing.B) {
	for _, size := range []int{0, 1000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			keys := make([]string, size)
			for i := range keys {
				keys[i] = fmt.Sprintf("user-%05d@example.com", i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				l := NewWindowLimiter(time.Hour, int(^uint(0)>>1))
				for _, key := range keys {
					if !l.Allow(key) {
						b.Fatal("setup rejected")
					}
				}
				runtime.KeepAlive(l)
			}
		})
	}
}
