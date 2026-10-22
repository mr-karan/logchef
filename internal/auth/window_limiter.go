package auth

import (
	"hash/maphash"
	"sync"
	"time"
)

// 65,536 buckets keep each limiter near 1.5 MiB without retaining keys.
const limiterBucketCount = 1 << 16

// WindowLimiter counts requests in bounded, keyed hash buckets.
type WindowLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	seed    maphash.Seed
	base    time.Time
	buckets [limiterBucketCount]limiterWindow
}

type limiterWindow struct {
	epoch    int64
	current  int
	previous int
}

// NewWindowLimiter allows up to limit requests per key within each window.
func NewWindowLimiter(window time.Duration, limit int) *WindowLimiter {
	return &WindowLimiter{window: window, limit: limit, seed: maphash.MakeSeed(), base: time.Now()}
}

// Allow records an attempt and reports whether the key is within its limit.
func (l *WindowLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.allowAt(key, time.Since(l.base))
}

func (l *WindowLimiter) allowAt(key string, elapsed time.Duration) bool {
	if key == "" || l.window <= 0 {
		return true
	}
	w := &l.buckets[maphash.String(l.seed, key)%limiterBucketCount]
	epoch := int64(elapsed / l.window)
	if epoch != w.epoch {
		w.previous = 0
		if epoch-w.epoch == 1 {
			w.previous = w.current
		}
		w.current = 0
		w.epoch = epoch
	}
	// Keep both windows so collisions stay conservative and a lone key resets within two windows.
	if w.current+w.previous >= l.limit {
		return false
	}
	w.current++
	return true
}
