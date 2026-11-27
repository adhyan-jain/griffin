package domain

import (
	"testing"
	"time"
)

// The owner's own ways of asking for a reminder, said at 03:15 at night.
func TestReminderPhrasings(t *testing.T) {
	now := time.Date(2026, 11, 28, 3, 15, 0, 0, ISTZone)
	cases := []struct{ text, what, at string }{
		{"send a notification to me tomorrow noon to call ankit mama", "call ankit mama", "Sat 28 Nov 12:00"},
		{"send me a notification tomorrow at noon to call ankit mama", "call ankit mama", "Sat 28 Nov 12:00"},
		{"remind me to call mitul at 6pm", "call mitul", "Sat 28 Nov 18:00"},
		{"notify me in 10 minutes to check the oven", "check the oven", "Sat 28 Nov 03:25"},
		{"give me a notification at 9 pm to study DSA", "study DSA", "Sat 28 Nov 21:00"},
		{"alert me tomorrow morning to apply to Zepto", "apply to Zepto", "Sat 28 Nov 09:00"},
		{"remind me on 1 dec at 10am to pay rent", "pay rent", "Tue 1 Dec 10:00"},
	}
	for _, c := range cases {
		what, ok := ParseReminder(c.text)
		at, ok2 := ResolveWhen(now, c.text)
		if !ok || !ok2 || what != c.what || at.Format("Mon 2 Jan 15:04") != c.at {
			t.Errorf("%q: got what=%q (%v) at=%s (%v); want %q at %s", c.text, what, ok, at.Format("Mon 2 Jan 15:04"), ok2, c.what, c.at)
		}
	}
	if _, ok := ParseReminder("Call mitul and tell him I'll be late"); ok {
		t.Error("a call request is not a reminder")
	}
}
