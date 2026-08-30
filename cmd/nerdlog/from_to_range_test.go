package main

import (
	"testing"
	"time"
)

func TestParseFromToRangeUsesProvidedCurrentTime(t *testing.T) {
	now := time.Date(2037, time.June, 1, 0, 0, 0, 0, time.UTC)

	got, err := ParseFromToRange(time.UTC, now, "Mar9 15:00 to Mar12 11:00")
	if err != nil {
		t.Fatalf("parsing range: %s", err)
	}

	if got.From.Time.Year() != 2037 || got.To.Time.Year() != 2037 {
		t.Fatalf("range did not use provided year: from %s, to %s", got.From.Time, got.To.Time)
	}
}
