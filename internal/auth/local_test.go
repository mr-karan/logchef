package auth

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHashAndVerifyLocalPassword(t *testing.T) {
	hash, err := HashLocalPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !VerifyLocalPassword(hash, "correct horse battery") {
		t.Error("correct password rejected")
	}
	if VerifyLocalPassword(hash, "wrong password!!") {
		t.Error("wrong password accepted")
	}
	if VerifyLocalPassword("", "anything at all") {
		t.Error("empty hash accepted a password")
	}
}

func TestLoginRateLimiter(t *testing.T) {
	l := NewLoginRateLimiter(time.Minute, 10, 5)
	for i := range 5 {
		if !l.Allow("1.2.3.4", "a@example.com") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.Allow("1.2.3.4", "a@example.com") {
		t.Error("6th attempt for same email should be blocked")
	}
	// Different email from the same IP is still within the per-IP budget (10).
	if !l.Allow("1.2.3.4", "b@example.com") {
		t.Error("different email should be allowed under per-IP budget")
	}
	// Exhaust the per-IP budget.
	for range 5 {
		l.Allow("1.2.3.4", "c@example.com")
	}
	if l.Allow("1.2.3.4", "d@example.com") {
		t.Error("per-IP budget should be exhausted")
	}
	if !l.Allow("9.9.9.9", "e@example.com") {
		t.Error("fresh IP should be allowed")
	}
}

func TestLoginRateLimiterCapacityPreservesLimits(t *testing.T) {
	l := NewLoginRateLimiter(time.Hour, 2, 2)
	for i := range MaxLimiterKeys {
		key := strconv.Itoa(i)
		if !l.Allow(key, key+"@example.com") {
			t.Fatalf("key %d rejected before capacity", i)
		}
	}
	if l.Allow("overflow", "overflow@example.com") {
		t.Fatal("new identity admitted at capacity")
	}
	if !l.Allow("0", "0@example.com") {
		t.Fatal("tracked identity lost its remaining budget")
	}
	if l.Allow("0", "0@example.com") {
		t.Fatal("tracked identity limit was evicted")
	}
}

func TestLoginRateLimiterConcurrentBudget(t *testing.T) {
	l := NewLoginRateLimiter(time.Minute, 10, 5)
	var allowed atomic.Int32
	var calls sync.WaitGroup
	for range 100 {
		calls.Go(func() {
			if l.Allow("ip", "USER@example.com") {
				allowed.Add(1)
			}
		})
	}
	calls.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed %d concurrent attempts, want 5", got)
	}
	if l.Allow("ip2", "user@example.com") {
		t.Fatal("email normalization bypassed the shared budget")
	}
}
