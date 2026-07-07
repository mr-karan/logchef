package auth

import (
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
	for i := 0; i < 5; i++ {
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
	for i := 0; i < 5; i++ {
		l.Allow("1.2.3.4", "c@example.com")
	}
	if l.Allow("1.2.3.4", "d@example.com") {
		t.Error("per-IP budget should be exhausted")
	}
	if !l.Allow("9.9.9.9", "e@example.com") {
		t.Error("fresh IP should be allowed")
	}
}
