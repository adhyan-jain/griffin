package domain

import (
	"testing"
	"time"
)

func TestOutreachTransitionsAreCompleteAndConsistent(t *testing.T) {
	for _, s := range AllOutreachStatuses {
		if _, ok := outreachTransitions[s]; !ok {
			t.Errorf("%s has no row in the transition table", s)
		}
		for _, to := range OutreachTransitionsFrom(s) {
			if !to.Valid() {
				t.Errorf("%s -> unknown %s", s, to)
			}
			found := false
			for _, p := range OutreachPrevious(to) {
				found = found || p == s
			}
			if !found {
				t.Errorf("OutreachPrevious(%s) misses %s", to, s)
			}
		}
	}
	if CanOutreachMove(OutreachFound, OutreachSent) {
		t.Fatal("a person cannot be sent a message before it is drafted and approved")
	}
	if len(OutreachTransitionsFrom(OutreachClosed)) != 0 {
		t.Fatal("closed is final")
	}
}

func TestOutreachLimits(t *testing.T) {
	l := DefaultOutreachLimits("linkedin")
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	loc := time.UTC
	noon := time.Date(2026, 11, 13, 12, 0, 0, 0, loc)

	if ok, _, _ := l.NextSend(noon, 0, nil); !ok {
		t.Fatal("first send at noon should be allowed")
	}
	if ok, at, why := l.NextSend(noon, l.DailyCap, nil); ok || at.Day() != 14 || why == "" {
		t.Fatalf("cap should wait for tomorrow: %v %v %q", ok, at, why)
	}
	last := noon.Add(-time.Minute)
	if ok, at, _ := l.NextSend(noon, 1, &last); ok || !at.Equal(last.Add(5*time.Minute)) {
		t.Fatalf("gap not enforced: %v %v", ok, at)
	}
	night := time.Date(2026, 11, 13, 23, 0, 0, 0, loc)
	if ok, at, why := l.NextSend(night, 0, nil); ok || at.Hour() != 8 || at.Day() != 14 || why != "quiet hours" {
		t.Fatalf("quiet hours past midnight: %v %v %q", ok, at, why)
	}
	early := time.Date(2026, 11, 13, 5, 0, 0, 0, loc)
	if ok, at, _ := l.NextSend(early, 0, nil); ok || at.Hour() != 8 || at.Day() != 13 {
		t.Fatalf("quiet hours before the end: %v %v", ok, at)
	}
	l.QuietStartHour, l.QuietEndHour = 0, 0
	if ok, _, _ := l.NextSend(night, 0, nil); !ok {
		t.Fatal("no quiet hours configured")
	}
}

func TestOutreachLimitsValidate(t *testing.T) {
	bad := DefaultOutreachLimits("email")
	bad.DailyCap = 0
	if bad.Validate() == nil {
		t.Fatal("zero cap must be refused")
	}
}
