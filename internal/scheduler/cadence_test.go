package scheduler

import (
	"testing"
	"time"
)

func TestNextOccurrence_Daily(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	// Ref time: Nov 17, 2026, 06:00:00 IST
	ref := time.Date(2026, 11, 17, 6, 0, 0, 0, loc)

	next, err := NextOccurrence("daily 08:00 IST", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 11, 17, 8, 0, 0, 0, loc).UTC()
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}

	// Ref time past 08:00: Nov 17, 2026, 09:00:00 IST -> should schedule Nov 18, 08:00 IST
	refPast := time.Date(2026, 11, 17, 9, 0, 0, 0, loc)
	nextPast, err := NextOccurrence("daily 08:00 IST", refPast)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedPast := time.Date(2026, 11, 18, 8, 0, 0, 0, loc).UTC()
	if !nextPast.Equal(expectedPast) {
		t.Errorf("expected %v, got %v", expectedPast, nextPast)
	}
}

func TestNextOccurrence_DailyMulti(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	// Ref time: Nov 17, 2026, 06:00:00 IST -> next should be 07:30 IST today
	ref1 := time.Date(2026, 11, 17, 6, 0, 0, 0, loc)
	next1, err := NextOccurrence("daily 07:30 and 21:30 IST", ref1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected1 := time.Date(2026, 11, 17, 7, 30, 0, 0, loc).UTC()
	if !next1.Equal(expected1) {
		t.Errorf("expected %v, got %v", expected1, next1)
	}

	// Ref time: Nov 17, 2026, 12:00:00 IST -> next should be 21:30 IST today
	ref2 := time.Date(2026, 11, 17, 12, 0, 0, 0, loc)
	next2, err := NextOccurrence("daily 07:30 and 21:30 IST", ref2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected2 := time.Date(2026, 11, 17, 21, 30, 0, 0, loc).UTC()
	if !next2.Equal(expected2) {
		t.Errorf("expected %v, got %v", expected2, next2)
	}

	// Ref time: Nov 17, 2026, 22:00:00 IST -> next should be 07:30 IST tomorrow
	ref3 := time.Date(2026, 11, 17, 22, 0, 0, 0, loc)
	next3, err := NextOccurrence("daily 07:30 and 21:30 IST", ref3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected3 := time.Date(2026, 11, 18, 7, 30, 0, 0, loc).UTC()
	if !next3.Equal(expected3) {
		t.Errorf("expected %v, got %v", expected3, next3)
	}
}

func TestNextOccurrence_Weekday(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	// Ref time: Tuesday Nov 17, 2026
	ref := time.Date(2026, 11, 17, 10, 0, 0, 0, loc)

	// Saturday 10:00 IST -> Nov 21, 2026 10:00
	nextSat, err := NextOccurrence("Saturday 10:00 IST", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSat := time.Date(2026, 11, 21, 10, 0, 0, 0, loc).UTC()
	if !nextSat.Equal(expectedSat) {
		t.Errorf("expected %v, got %v", expectedSat, nextSat)
	}

	// Sunday 19:00 IST -> Nov 22, 2026 19:00
	nextSun, err := NextOccurrence("Sunday 19:00 IST", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSun := time.Date(2026, 11, 22, 19, 0, 0, 0, loc).UTC()
	if !nextSun.Equal(expectedSun) {
		t.Errorf("expected %v, got %v", expectedSun, nextSun)
	}
}

func TestNextOccurrence_Monthly(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	// Ref time: Nov 17, 2026
	ref := time.Date(2026, 11, 17, 10, 0, 0, 0, loc)

	next, err := NextOccurrence("monthly", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 12, 1, 10, 0, 0, 0, loc).UTC()
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}
