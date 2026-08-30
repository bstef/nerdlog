package main

import (
	"strings"
	"testing"
	"time"
)

func TestNewAppClockE2EOverride(t *testing.T) {
	t.Setenv(e2eTestNowEnv, "2025-06-01T00:00:00Z")

	appClock, err := newAppClock()
	if err != nil {
		t.Fatalf("creating app clock: %s", err)
	}

	got := appClock.Now()
	want := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)
	if delta := got.Sub(want); delta < 0 || delta > 5*time.Second {
		t.Fatalf("overridden time differs from requested time by %s: got %s, want %s", delta, got, want)
	}
}

func TestNewAppClockRejectsInvalidE2EOverride(t *testing.T) {
	t.Setenv(e2eTestNowEnv, "not-a-time")

	_, err := newAppClock()
	if err == nil || !strings.Contains(err.Error(), e2eTestNowEnv) {
		t.Fatalf("expected an error mentioning %s, got %v", e2eTestNowEnv, err)
	}
}
