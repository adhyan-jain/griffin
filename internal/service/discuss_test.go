package service

import "testing"

func TestDiscussFirstAndGoAhead(t *testing.T) {
	for _, s := range []string{"let's discuss the internship filter first", "I want to tweak the plan before you run it", "plan it first please", "don't run anything yet"} {
		if !DiscussFirst(s) {
			t.Errorf("should discuss: %q", s)
		}
	}
	for _, s := range []string{"find internships in Bangalore", "what is a plan", "send the mail"} {
		if DiscussFirst(s) {
			t.Errorf("should not discuss: %q", s)
		}
	}
	for _, s := range []string{"go ahead", "Ok, go ahead!", "run it", "looks good, go", "yes proceed"} {
		if !GoAhead(s) {
			t.Errorf("should be a go-ahead: %q", s)
		}
	}
	for _, s := range []string{"go ahead and add a salary filter", "do it later", "I will go ahead tomorrow"} {
		if GoAhead(s) {
			t.Errorf("should not be a go-ahead: %q", s)
		}
	}
}

func TestDiscussedObjectiveCollectsOnlyTheOwnersTurns(t *testing.T) {
	roles := []string{"user", "user" /*discuss*/, "assistant", "user", "assistant"}
	texts := []string{"hello", "let's discuss an internship filter first" + DiscussHint, "Plan: ...", "only paid, remote", "Updated plan"}
	got := DiscussedObjective(roles, texts)
	want := "let's discuss an internship filter first\nonly paid, remote"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if DiscussedObjective([]string{"user"}, []string{"find internships"}) != "" {
		t.Fatal("no discussion means nothing to submit")
	}
}
