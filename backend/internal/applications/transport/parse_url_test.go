package transport

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimitPerMinute(t *testing.T) {
	rl := newRateLimiter(3)
	if !rl.Allow(1) || !rl.Allow(1) || !rl.Allow(1) {
		t.Fatal("first three calls should be allowed")
	}
	if rl.Allow(1) {
		t.Fatal("fourth call in the same minute must be rejected")
	}
	// Different users have independent budgets.
	if !rl.Allow(2) {
		t.Fatal("a different user must not be affected")
	}
}

func TestRateLimiterWindowRollsOver(t *testing.T) {
	rl := newRateLimiter(1)
	if !rl.Allow(7) {
		t.Fatal("first call allowed")
	}
	if rl.Allow(7) {
		t.Fatal("second call within the window must be rejected")
	}
	// Fast-forward past the one-minute window.
	rl.mu.Lock()
	rl.hits[7] = []time.Time{time.Now().Add(-2 * time.Minute)}
	rl.mu.Unlock()
	if !rl.Allow(7) {
		t.Fatal("after the window rolls over the call must be allowed again")
	}
}
