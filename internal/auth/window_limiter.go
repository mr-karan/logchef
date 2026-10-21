package auth

import (
	"container/list"
	"sync"
	"time"
)

// MaxLimiterKeys bounds memory without evicting active limits.
const MaxLimiterKeys = 10_000

// WindowLimiter counts requests per key in fixed windows ordered by expiration.
type WindowLimiter struct {
	mu     sync.Mutex
	window time.Duration
	limit  int
	keys   map[string]*list.Element
	expiry list.List
}

type limiterWindow struct {
	key   string
	start time.Time
	count int
}

// NewWindowLimiter allows up to limit requests per key within each window.
func NewWindowLimiter(window time.Duration, limit int) *WindowLimiter {
	return &WindowLimiter{window: window, limit: limit, keys: make(map[string]*list.Element)}
}

// Allow rejects new keys at capacity so existing limits cannot be bypassed.
func (l *WindowLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for head := l.expiry.Front(); head != nil; head = l.expiry.Front() {
		w := head.Value.(*limiterWindow)
		if now.Sub(w.start) < l.window {
			break
		}
		delete(l.keys, w.key)
		l.expiry.Remove(head)
	}
	if element, ok := l.keys[key]; ok {
		w := element.Value.(*limiterWindow)
		if w.count >= l.limit {
			return false
		}
		w.count++
		return true
	}
	if len(l.keys) >= MaxLimiterKeys {
		return false
	}
	l.keys[key] = l.expiry.PushBack(&limiterWindow{key: key, start: now, count: 1})
	return true
}
