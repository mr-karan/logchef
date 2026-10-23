package auth

import (
	"hash/maphash"
	"math"
	"sync"
	"time"
)

// Colliding keys share a budget capped at the limit per epoch and twice the limit per rolling window.
const limiterBucketCount = 1 << 16

// WindowLimiter counts requests in bounded, keyed hash buckets.
type WindowLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	seed    maphash.Seed
	base    time.Time
	buckets *[limiterBucketCount]limiterWindow
	global  limiterWindow
}

type limiterWindow struct {
	epoch    int64
	current  int32
	previous int32
}

// NewWindowLimiter limits each key using a sliding-window counter.
func NewWindowLimiter(window time.Duration, limit int) *WindowLimiter {
	l := NewGlobalWindowLimiter(window, limit)
	l.buckets = new([limiterBucketCount]limiterWindow)
	return l
}

// NewGlobalWindowLimiter shares one sliding-window counter across all keys.
func NewGlobalWindowLimiter(window time.Duration, limit int) *WindowLimiter {
	return &WindowLimiter{
		window: window, limit: min(limit, math.MaxInt32), seed: maphash.MakeSeed(), base: time.Now(),
	}
}

func (l *WindowLimiter) bucket(key string) *limiterWindow {
	if l.buckets == nil {
		return &l.global
	}
	return &l.buckets[maphash.String(l.seed, key)%limiterBucketCount]
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
	w := l.bucket(key)
	epoch := int64(elapsed / l.window)
	if epoch != w.epoch {
		w.previous = 0
		if epoch-w.epoch == 1 {
			w.previous = w.current
		}
		w.current = 0
		w.epoch = epoch
	}
	remaining := float64(l.window-elapsed%l.window) / float64(l.window)
	if float64(w.current)+float64(w.previous)*remaining >= float64(l.limit) {
		return false
	}
	w.current++
	return true
}
