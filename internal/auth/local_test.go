package auth

import (
	"fmt"
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

func TestLoginRateLimiterCapacityAdmitsNewIdentities(t *testing.T) {
	l := NewLoginRateLimiter(time.Hour, 2, 2)
	for i := range MaxLimiterKeys {
		key := strconv.Itoa(i)
		if !l.Allow(key, key+"@example.com") {
			t.Fatalf("key %d rejected before capacity", i)
		}
	}
	if !l.Allow("overflow", "overflow@example.com") {
		t.Fatal("new identity rejected at capacity")
	}
	if len(l.ips.keys) != MaxLimiterKeys || len(l.emails.keys) != MaxLimiterKeys {
		t.Fatal("limiter exceeded its memory bound")
	}
	if !l.Allow("1", "1@example.com") {
		t.Fatal("tracked identity lost its remaining budget")
	}
	if l.Allow("1", "1@example.com") {
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

func TestLoginRateLimiterBlockedIPPreservesEmailWindow(t *testing.T) {
	l := NewLoginRateLimiter(time.Hour, 10, 5)
	for range 5 {
		if !l.Allow("victim-ip", "victim@example.com") {
			t.Fatal("victim budget exhausted early")
		}
	}
	for i := range 10_000 {
		l.Allow("flood-ip", strconv.Itoa(i)+"@example.com")
	}
	if l.Allow("fresh-ip", "victim@example.com") {
		t.Fatal("blocked IP flood reset victim email limit")
	}
}

func TestLoginRateLimiterRotatingIPFloodPreservesEmailWindow(t *testing.T) {
	for _, samePrefix := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_prefix=%t", samePrefix), func(t *testing.T) {
			l := NewLoginRateLimiter(time.Hour, 10, 5)
			for range 5 {
				if !l.Allow("192.0.2.1", "victim@example.com") {
					t.Fatal("victim budget exhausted early")
				}
			}
			for i := range 1000 {
				ip := fmt.Sprintf("2001:db8::%x", i+1)
				if !samePrefix {
					ip = fmt.Sprintf("2001:db8:%x::1", i+1)
				}
				for j := range 10 {
					l.Allow(ip, fmt.Sprintf("flood-%d-%d@example.com", i, j))
					if len(l.ips.keys) > MaxLimiterKeys || len(l.emails.keys) > MaxLimiterKeys {
						t.Fatal("flood exceeded limiter memory bound")
					}
				}
			}
			if l.Allow("192.0.2.2", "victim@example.com") {
				t.Fatal("rotating IP flood reset victim email limit")
			}
		})
	}
}

func TestLoginRateLimiterIPv6PrefixBudget(t *testing.T) {
	l := NewLoginRateLimiter(time.Hour, 10, 5)
	for i := range 10 {
		if !l.Allow(fmt.Sprintf("2001:db8:1:2::%x", i+1), fmt.Sprintf("user-%d@example.com", i)) {
			t.Fatalf("prefix attempt %d rejected before budget", i+1)
		}
	}
	if l.Allow("2001:db8:1:2:ffff::1", "fresh@example.com") {
		t.Fatal("new address bypassed shared IPv6 /64 budget")
	}
	for _, ip := range []string{"2001:db8:1:3::1", "192.0.2.1", "192.0.2.2"} {
		if !l.Allow(ip, "fresh@example.com") {
			t.Fatalf("independent IP budget rejected for %s", ip)
		}
	}
}

func TestWindowLimiterCapacityPreservesExhaustedWindows(t *testing.T) {
	l := NewWindowLimiter(time.Hour, 2)
	for range 2 {
		if !l.Allow("victim") {
			t.Fatal("victim budget exhausted early")
		}
	}
	for i := range MaxLimiterKeys * 2 {
		if !l.Allow(strconv.Itoa(i)) {
			t.Fatalf("new key %d rejected", i)
		}
		if len(l.keys) > MaxLimiterKeys || l.expiry.Len() > MaxLimiterKeys || l.evictable.Len() > MaxLimiterKeys {
			t.Fatal("limiter exceeded memory bound")
		}
	}
	if l.Allow("victim") {
		t.Fatal("capacity eviction reset exhausted window")
	}
}

func TestWindowLimiterAllBlockedAdmitsNewKeys(t *testing.T) {
	l := NewWindowLimiter(time.Hour, 1)
	for i := range MaxLimiterKeys * 2 {
		key := strconv.Itoa(i)
		if !l.Allow(key) {
			t.Fatalf("new key %d rejected", i)
		}
		if l.Allow(key) {
			t.Fatalf("key %d bypassed its budget", i)
		}
		if len(l.keys) > MaxLimiterKeys || l.expiry.Len() > MaxLimiterKeys || l.evictable.Len() > MaxLimiterKeys {
			t.Fatal("limiter exceeded memory bound")
		}
	}
	if !l.Allow("0") {
		t.Fatal("oldest blocked window was not evicted")
	}
	if l.Allow(strconv.Itoa(MaxLimiterKeys*2 - 1)) {
		t.Fatal("newest blocked window was evicted")
	}
}
