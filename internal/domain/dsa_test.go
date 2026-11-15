package domain

import (
	"testing"
	"time"
)

func d(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }

func list(n int) []*DsaItem {
	var out []*DsaItem
	for i := 0; i < n; i++ {
		out = append(out, &DsaItem{Slug: string(rune('a' + i)), Title: string(rune('A' + i)), Ord: i, State: DsaQueued})
	}
	return out
}

func TestPlanPicksTwoInOrderAndIsIdempotent(t *testing.T) {
	items := list(5)
	got := PlanDsaDay(items, d(1), 2)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
	again := PlanDsaDay(items, d(1), 2)
	if len(again) != 2 || items[2].State != DsaQueued || items[0].Attempts != 1 {
		t.Fatalf("second call the same day must change nothing: %v attempts=%d", again, items[0].Attempts)
	}
}

func TestMissedProblemRestsAWeekThenComesBackFirst(t *testing.T) {
	items := list(20)
	PlanDsaDay(items, d(1), 2)        // a, b handed out on the 1st
	items[1].State = DsaSolved        // b solved, a missed
	got := PlanDsaDay(items, d(2), 2) // next day: a is failed, not due until the 8th
	if got[0] != "c" || got[1] != "d" || items[0].State != DsaFailed {
		t.Fatalf("a must rest: got %v state=%s", got, items[0].State)
	}
	if !items[0].DueOn.Equal(d(8)) {
		t.Fatalf("due %v, want the 8th (a week after it was handed out)", items[0].DueOn)
	}
	for day := 3; day <= 7; day++ { // nothing on days 3..7 may bring a back
		for _, s := range PlanDsaDay(items, d(day), 2) {
			if s == "a" {
				t.Fatalf("a came back early on day %d", day)
			}
		}
	}
	got = PlanDsaDay(items, d(8), 2)
	if got[0] != "a" && got[1] != "a" {
		t.Fatalf("a must come back on the 8th: %v", got)
	}
	if items[0].Attempts != 2 {
		t.Fatalf("attempts %d, want 2", items[0].Attempts)
	}
}

func TestSolvedNeverReturnsAndListRunsOut(t *testing.T) {
	items := list(2)
	PlanDsaDay(items, d(1), 2)
	items[0].State, items[1].State = DsaSolved, DsaSolved
	if got := PlanDsaDay(items, d(30), 2); len(got) != 0 {
		t.Fatalf("solved problems must not return: %v", got)
	}
}

func TestNagQuietUntilEveningAndWhenDone(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	items := list(2)
	PlanDsaDay(items, d(1), 2)
	morning := time.Date(2026, 11, 1, 9, 0, 0, 0, loc)
	if _, ok := DsaNag(items, morning, loc, 18); ok {
		t.Fatal("must not nag in the morning")
	}
	evening := time.Date(2026, 11, 1, 19, 0, 0, 0, loc)
	msg, ok := DsaNag(items, evening, loc, 18)
	if !ok || msg == "" {
		t.Fatal("must nag in the evening")
	}
	late := time.Date(2026, 11, 1, 22, 30, 0, 0, loc)
	if m, _ := DsaNag(items, late, loc, 18); m == msg {
		t.Fatal("late nag must be firmer")
	}
	items[0].State, items[1].State = DsaSolved, DsaSolved
	if _, ok := DsaNag(items, late, loc, 18); ok {
		t.Fatal("must not nag when everything is solved")
	}
}

func TestSeedHasUniqueSlugs(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range DsaSeed {
		if s.Slug == "" || s.Title == "" || seen[s.Slug] {
			t.Fatalf("bad or duplicate seed entry %q", s.Slug)
		}
		seen[s.Slug] = true
	}
}

func TestSolvingEarlyDoesNotHandOutMore(t *testing.T) {
	items := list(6)
	PlanDsaDay(items, d(1), 2)
	items[0].State = DsaSolved
	got := PlanDsaDay(items, d(1), 2)
	if len(got) != 2 || items[2].State != DsaQueued {
		t.Fatalf("a solved problem still fills its slot: %v, c=%s", got, items[2].State)
	}
}
