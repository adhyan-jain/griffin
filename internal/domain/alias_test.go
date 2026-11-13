package domain

import "testing"

func TestApplyAliases(t *testing.T) {
	a := map[string]string{"mummy": "mom", "bhai": "anish aggarwal", "bade bhai": "rohan", "mom": "mummy"}
	for in, want := range map[string]string{
		"call mummy":                 "call mom",
		"Call MUMMY now":             "Call mom now",
		"whatsapp bade bhai hello":   "whatsapp rohan hello",
		"message my bhai":            "message my anish aggarwal",
		"mummys recipe":              "mummys recipe",
		"what did bhai's friend say": "what did bhai's friend say",
		"mom and mummy":              "mummy and mom",
		"no alias here":              "no alias here",
		"mummy, call bhai.":          "mom, call anish aggarwal.",
	} {
		if got := ApplyAliases(in, a); got != want {
			t.Errorf("%q => %q, want %q", in, got, want)
		}
	}
	if ApplyAliases("hi", nil) != "hi" {
		t.Error("no aliases must leave text alone")
	}
}

func TestNormalizeAlias(t *testing.T) {
	if got, err := NormalizeAlias("  Mummy  Ji ", 40); err != nil || got != "mummy ji" {
		t.Errorf("got %q %v", got, err)
	}
	for _, bad := range []string{"", "   ", "a;drop", "<b>"} {
		if _, err := NormalizeAlias(bad, 40); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
