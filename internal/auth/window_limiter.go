package auth

import (
	"container/list"
	"sync"
	"time"
)

// MaxLimiterKeys bounds the number of tracked windows.
const MaxLimiterKeys = 10_000

// WindowLimiter counts requests per key in fixed windows ordered by expiration.
type WindowLimiter struct {
	mu        sync.Mutex
	window    time.Duration
	limit     int
	keys      map[string]*list.Element
	expiry    list.List
	evictable list.List
}

type limiterWindow struct {
	key       string
	start     time.Time
	count     int
	evictable *list.Element
}

// NewWindowLimiter allows up to limit requests per key within each window.
func NewWindowLimiter(window time.Duration, limit int) *WindowLimiter {
	return &WindowLimiter{window: window, limit: limit, keys: make(map[string]*list.Element)}
}

// Allow records an attempt and reports whether the key is within its limit.
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
		l.remove(head)
	}
	if element, ok := l.keys[key]; ok {
		w := element.Value.(*limiterWindow)
		if w.count >= l.limit {
			return false
		}
		w.count++
		if w.count == l.limit {
			l.evictable.Remove(w.evictable)
			w.evictable = nil
		}
		return true
	}
	if len(l.keys) >= MaxLimiterKeys {
		// Evict the oldest window under its limit; if all are blocked, the oldest one, so new users are still admitted.
		head := l.expiry.Front()
		if candidate := l.evictable.Front(); candidate != nil {
			head = candidate.Value.(*list.Element)
		}
		l.remove(head)
	}
	w := &limiterWindow{key: key, start: now, count: 1}
	element := l.expiry.PushBack(w)
	l.keys[key] = element
	if w.count < l.limit {
		w.evictable = l.evictable.PushBack(element)
	}
	return true
}

func (l *WindowLimiter) remove(element *list.Element) {
	w := element.Value.(*limiterWindow)
	delete(l.keys, w.key)
	l.expiry.Remove(element)
	if w.evictable != nil {
		l.evictable.Remove(w.evictable)
	}
}
