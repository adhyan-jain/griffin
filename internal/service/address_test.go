package service

import "testing"

func TestStripLeadingName(t *testing.T) {
	cases := []struct{ in, name, want string }{
		{"JJ wassup", "JJ", "wassup"},
		{"hey JJ wassup", "JJ", "wassup"},
		{"Hey Jarvis, plan my week", "Jarvis", "plan my week"},
		{"jarvis: what's the time", "Jarvis", "what's the time"},
		{"JJ", "JJ", ""},
		{"ok jj!", "JJ", ""},
		{"Jarvisly speaking", "Jarvis", "Jarvisly speaking"},
		{"tell Jarvis to wait", "Jarvis", "tell Jarvis to wait"},
		{"Mr Stark, call mom", "Mr Stark", "call mom"},
		{"O'Neil, hello", "O'Neil", "hello"},
		{"hello there", "JJ", "hello there"},
		{"  JJ   wassup  ", "JJ", "wassup"},
		{"anything", "", "anything"},
		{"a.b wassup", "a.b", "wassup"}, // regex characters in a name must not act as a pattern
		{"axb wassup", "a.b", "axb wassup"},
	}
	for _, c := range cases {
		if got := StripLeadingName(c.in, c.name); got != c.want {
			t.Errorf("StripLeadingName(%q, %q) = %q, want %q", c.in, c.name, got, c.want)
		}
	}
}
