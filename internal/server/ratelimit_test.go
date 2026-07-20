package server

import (
	"testing"
	"time"
)

func TestWindowLimiterAllowsUpToLimit(t *testing.T) {
	l := newWindowLimiter(time.Minute, 3)
	for i := 1; i <= 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("request %d rejected, want allowed", i)
		}
	}
	if l.Allow("k") {
		t.Fatal("request 4 allowed, want rejected")
	}
	if l.Allow("k") {
		t.Fatal("request 5 allowed, want rejected")
	}
}

func TestWindowLimiterIsolatesKeys(t *testing.T) {
	l := newWindowLimiter(time.Minute, 1)
	if !l.Allow("a") {
		t.Fatal("first request for a rejected")
	}
	if l.Allow("a") {
		t.Fatal("second request for a allowed, want rejected")
	}
	// A different key has its own independent window.
	if !l.Allow("b") {
		t.Fatal("first request for b rejected; keys are not isolated")
	}
}

func TestWindowLimiterEmptyKeyAlwaysAllowed(t *testing.T) {
	l := newWindowLimiter(time.Minute, 1)
	for i := 0; i < 5; i++ {
		if !l.Allow("") {
			t.Fatalf("empty key rejected on request %d", i)
		}
	}
}

func TestWindowLimiterResetsAfterWindow(t *testing.T) {
	l := newWindowLimiter(20*time.Millisecond, 1)
	if !l.Allow("k") {
		t.Fatal("first request rejected")
	}
	if l.Allow("k") {
		t.Fatal("second request within window allowed, want rejected")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("request after window elapsed rejected, want allowed")
	}
}

func TestWindowLimiterPrunesStaleKeys(t *testing.T) {
	l := newWindowLimiter(20*time.Millisecond, 5)
	l.Allow("stale")
	if got := len(l.keys); got != 1 {
		t.Fatalf("keys after first insert = %d, want 1", got)
	}
	// After the window elapses, a call for a different key should prune the
	// stale one during its lazy prune pass.
	time.Sleep(30 * time.Millisecond)
	l.Allow("fresh")
	if _, ok := l.keys["stale"]; ok {
		t.Fatal("stale key was not pruned")
	}
	if _, ok := l.keys["fresh"]; !ok {
		t.Fatal("fresh key missing after insert")
	}
}
