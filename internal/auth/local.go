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
	mu     sync.Mutex
	ips    *WindowLimiter
	emails *WindowLimiter
}

// NewLoginRateLimiter limits local login attempts by IP and email.
func NewLoginRateLimiter(window time.Duration, perIP, perEmail int) *LoginRateLimiter {
	return &LoginRateLimiter{
		ips:    NewWindowLimiter(window, perIP),
		emails: NewWindowLimiter(window, perEmail),
	}
}

// Allow records an attempt and reports whether it is within both limits.
func (l *LoginRateLimiter) Allow(ip, email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ips.Allow(strings.TrimSpace(ip)) && l.emails.Allow(strings.ToLower(strings.TrimSpace(email)))
}
