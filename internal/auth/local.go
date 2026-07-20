package auth

import (
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// MinLocalPasswordLength is enforced everywhere a local-auth password is set.
const MinLocalPasswordLength = 10

// dummyHash is compared against when the target user has no usable password,
// keeping the login path's timing roughly constant for unknown emails.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// HashLocalPassword bcrypt-hashes a password for local authentication.
func HashLocalPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyLocalPassword reports whether password matches the stored bcrypt hash.
// An empty hash always fails, after a dummy comparison to keep timing flat.
func VerifyLocalPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// LoginRateLimiter is a fixed-window in-memory limiter for the local login
// endpoint, keyed independently by client IP and by (lowercased) email so a
// single address can't brute-force many accounts and a distributed attempt
// can't hammer one account.
type LoginRateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	perIP    int
	perEmail int
	ips      map[string]*rateWindow
	emails   map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func NewLoginRateLimiter(window time.Duration, perIP, perEmail int) *LoginRateLimiter {
	return &LoginRateLimiter{
		window:   window,
		perIP:    perIP,
		perEmail: perEmail,
		ips:      make(map[string]*rateWindow),
		emails:   make(map[string]*rateWindow),
	}
}

// Allow records an attempt and reports whether it is within both limits.
func (l *LoginRateLimiter) Allow(ip, email string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	okIP := bump(l.ips, strings.TrimSpace(ip), now, l.window, l.perIP)
	okEmail := bump(l.emails, strings.ToLower(strings.TrimSpace(email)), now, l.window, l.perEmail)
	return okIP && okEmail
}

func bump(m map[string]*rateWindow, key string, now time.Time, window time.Duration, limit int) bool {
	if key == "" {
		return true
	}
	w, ok := m[key]
	if !ok || now.Sub(w.start) >= window {
		m[key] = &rateWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= limit
}

// pruneLocked drops expired windows so the maps can't grow unbounded.
func (l *LoginRateLimiter) pruneLocked(now time.Time) {
	for k, w := range l.ips {
		if now.Sub(w.start) >= l.window {
			delete(l.ips, k)
		}
	}
	for k, w := range l.emails {
		if now.Sub(w.start) >= l.window {
			delete(l.emails, k)
		}
	}
}
