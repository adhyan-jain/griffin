package scheduler

import (
	"testing"
	"time"
)

// The schedules table stores 5-field cron in IST; the tick must read it, not pause the routine.
func TestNextOccurrenceCron(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	after := time.Date(2026, 11, 28, 10, 0, 0, 0, ist)
	cases := map[string]time.Time{
		"45 7 * * *":   time.Date(2026, 11, 29, 7, 45, 0, 0, ist),
		"30 21 * * *":  time.Date(2026, 11, 28, 21, 30, 0, 0, ist),
		"0 10 * * 1-5": time.Date(2026, 11, 30, 10, 0, 0, 0, ist), // 28 Nov 2026 is a Saturday
		"*/15 * * * *": time.Date(2026, 11, 28, 10, 15, 0, 0, ist),
		"0 10 1 * *":   time.Date(2026, 12, 1, 10, 0, 0, 0, ist),
	}
	for expr, want := range cases {
		got, err := NextOccurrence(expr, after)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q: got %v, %v; want %v", expr, got.In(ist), err, want)
		}
	}
	if _, err := NextOccurrence("61 7 * * *", after); err == nil {
		t.Error("an invalid cron is still an error")
	}
}
