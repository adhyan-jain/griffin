package domain

import (
	"testing"
	"time"
)

func ist(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, ISTZone)
}

const ankit = "Give me a notification to remind me that I need to talk to Ankit mama tomorrow at 12 in the noon"

func TestResolveWhen(t *testing.T) {
	night := ist(2026, 11, 28, 1, 19) // Sat, the owner is still up
	day := ist(2026, 11, 28, 14, 0)
	cases := []struct {
		name string
		now  time.Time
		text string
		want time.Time
		ok   bool
	}{
		{"ankit at night means this morning", night, ankit, ist(2026, 11, 28, 12, 0), true},
		{"ankit in the afternoon means next day", day, ankit, ist(2026, 11, 29, 12, 0), true},
		{"at 9 at 20:00 is 21:00", ist(2026, 11, 28, 20, 0), "remind me to call mom at 9", ist(2026, 11, 28, 21, 0), true},
		{"in 2 hours", day, "remind me in 2 hours", ist(2026, 11, 28, 16, 0), true},
		{"in an hour", day, "in an hour", ist(2026, 11, 28, 15, 0), true},
		{"in half an hour", day, "in half an hour", ist(2026, 11, 28, 14, 30), true},
		{"in 10 minutes", day, "in 10 minutes", ist(2026, 11, 28, 14, 10), true},
		{"tomorrow without a time", day, "remind me tomorrow", time.Time{}, false},
		{"weekday", day, "on friday at 5pm", ist(2026, 12, 4, 17, 0), true},
		{"same weekday is next week", day, "on saturday at 5pm", ist(2026, 12, 5, 17, 0), true},
		{"date dd mon", day, "28th november at 6 pm", ist(2026, 11, 28, 18, 0), true},
		{"date mon dd", day, "nov 30 at 17:30", ist(2026, 11, 30, 17, 30), true},
		{"date slash", day, "on 2/12 at 8", ist(2026, 12, 2, 8, 0), true},
		{"tonight at 11 after midnight", ist(2026, 11, 28, 1, 0), "tonight at 11", ist(2026, 11, 28, 23, 0), true},
		{"tonight alone", day, "tonight", ist(2026, 11, 28, 21, 0), true},
		{"tomorrow morning", day, "tomorrow morning", ist(2026, 11, 29, 9, 0), true},
		{"tomorrow evening", day, "tomorrow evening", ist(2026, 11, 29, 19, 0), true},
		{"day after tomorrow noon", day, "day after tomorrow at noon", ist(2026, 11, 30, 12, 0), true},
		{"tomorrow at 5:30 pm", day, "tomorrow 5:30 pm", ist(2026, 11, 29, 17, 30), true},
		{"midnight", day, "tomorrow at midnight", ist(2026, 11, 29, 0, 0), true},
		{"at 7 after 2pm is 7 pm", day, "at 7", ist(2026, 11, 28, 19, 0), true},
		{"at 7 at 8 pm is next morning", ist(2026, 11, 28, 21, 0), "at 7", ist(2026, 11, 29, 7, 0), true},
		{"today in the past", day, "today at 9 am", time.Time{}, false},
		{"no time at all", day, "call mom", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := ResolveWhen(c.now, c.text)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("%s: got %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestParseReminder(t *testing.T) {
	cases := []struct {
		text, what string
		ok         bool
	}{
		{ankit, "talk to Ankit mama", true},
		{"remind me to call mom at 9", "call mom", true},
		{"Remind me that I have to pay rent tomorrow at 10 am.", "pay rent", true},
		{"set a reminder to drink water in 2 hours", "drink water", true},
		{"reminder: submit the form on friday at 5pm", "submit the form", true},
		{"ping me at 5 to call mom", "call mom", true},
		{"notify me about the meeting tomorrow at 3 pm", "the meeting", true},
		{"how do reminders work?", "", false},
		{"how do I set a reminder for tomorrow", "", false},
		{"call mom tomorrow at 9", "", false},
	}
	for _, c := range cases {
		what, ok := ParseReminder(c.text)
		if ok != c.ok || what != c.what {
			t.Errorf("%q: got %q %v, want %q %v", c.text, what, ok, c.what, c.ok)
		}
	}
}

func TestDescribeWhen(t *testing.T) {
	if got := DescribeWhen(ist(2026, 11, 28, 1, 19), ist(2026, 11, 28, 12, 0)); got != "today at 12:00 pm" {
		t.Error(got)
	}
	if got := DescribeWhen(ist(2026, 11, 28, 14, 0), ist(2026, 11, 29, 12, 0)); got != "tomorrow at 12:00 pm" {
		t.Error(got)
	}
	if got := DescribeWhen(ist(2026, 11, 28, 14, 0), ist(2026, 12, 4, 17, 0)); got != "Fri 4 Dec at 5:00 pm" {
		t.Error(got)
	}
}
