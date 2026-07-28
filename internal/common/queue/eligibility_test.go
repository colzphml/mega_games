package queue

import (
	"testing"
	"time"
)

func TestStaleThresholdIsLargerThanTickerInterval(t *testing.T) {
	interval := time.Minute
	got := StaleThreshold(interval)
	if got <= interval {
		t.Fatalf("StaleThreshold(%v) = %v; must exceed the ticker interval, "+
			"otherwise a send that takes exactly one interval is picked up twice", interval, got)
	}
	if got != 3*time.Minute {
		t.Errorf("StaleThreshold(%v) = %v, want 3m", interval, got)
	}
}

func TestStaleCutoffExcludesRecentAttempt(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cutoff := StaleCutoff(now, time.Minute)

	startedOneIntervalAgo := now.Add(-time.Minute)
	if !startedOneIntervalAgo.After(cutoff) {
		t.Error("an attempt started one interval ago must NOT be eligible for retry")
	}

	startedLongAgo := now.Add(-10 * time.Minute)
	if !startedLongAgo.Before(cutoff) {
		t.Error("an attempt started ten intervals ago must be eligible for retry")
	}
}

func TestStaleThresholdZeroInterval(t *testing.T) {
	if got := StaleThreshold(0); got != 0 {
		t.Errorf("StaleThreshold(0) = %v, want 0 (retry loop disabled)", got)
	}
}
