package domain

import (
	"strings"
	"testing"
)

func TestParseProposalsReadsTheFencedBlockAndDropsJunk(t *testing.T) {
	text := "Here is what I found.\n\n```proposals\n[" +
		`{"title":"Prepare Tier A drafts","why":"6 are open","goal":"prepare the application drafts for all the A tier internships on my list"},` +
		`{"title":"","why":"no title","goal":"x"},` +
		`{"title":"No goal","why":"y","goal":""}` +
		"]\n```\n"
	got := ParseProposals(text)
	if len(got) != 1 || got[0].Title != "Prepare Tier A drafts" {
		t.Fatalf("want one proposal, got %+v", got)
	}
	for _, bad := range []string{"", "no block here", "```proposals\nnot json\n```"} {
		if ParseProposals(bad) != nil {
			t.Errorf("%q must produce no proposals", bad)
		}
	}
}

func TestParseProposalsIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("```proposals\n[")
	for i := 0; i < 20; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"title":"t","why":"w","goal":"g"}`)
	}
	b.WriteString("]\n```")
	if n := len(ParseProposals(b.String())); n != 8 {
		t.Fatalf("want 8, got %d", n)
	}
}

func TestInitiativeKindOf(t *testing.T) {
	cases := map[string]string{
		"take initiative on my internships and tell me what to do next":                      "internships",
		"which hackathons are easy wins that i should target":                                "hackathons",
		"some companies on my internship list are not applied to, what should I do about it": "internships",
		"expand my internship list":                                                          "internships",
		"prepare the application drafts for all the A tier internships on my list":           "",
		"summarise the internship tracker":                                                   "",
		"build me a hackathon website":                                                       "",
	}
	for in, want := range cases {
		if got := InitiativeKindOf(in); got != want {
			t.Errorf("%q: want %q, got %q", in, want, got)
		}
	}
}

func TestAnInitiativeTaskIsNeverTheApplicationBatchItProposes(t *testing.T) {
	title, goal := InitiativeTaskFor("internships")
	if !IsInitiativeTask(title) {
		t.Fatal("title must mark it as an initiative task")
	}
	if _, ok := ApplyBatchOf(title, goal, nil); ok {
		t.Fatal("the initiative task must not be split into applications itself")
	}
	if TrackerTypeOf(title, goal, nil) != "internships" {
		t.Fatal("it must be shown the internships list")
	}
	hTitle, hGoal := InitiativeTaskFor("hackathons")
	if TrackerTypeOf(hTitle, hGoal, nil) != "hackathons" {
		t.Fatal("the hackathon initiative must be shown the hackathons list")
	}
}

func TestRepoWorkIsRecognisedAndOnlyThePushStepWaits(t *testing.T) {
	req := "clean up the READMEs of my repos"
	if !RepoWorkKindOf(req) {
		t.Fatal("a README cleanup request is repo work")
	}
	for _, not := range []string{"summarise the internship tracker", "write a poem", "what is a readme"} {
		if RepoWorkKindOf(not) {
			t.Errorf("%q must not be repo work", not)
		}
	}
	title, goal := RepoWorkTaskFor(req)
	task := &Task{ManagerDomain: "engineering", Title: title, Goal: goal}
	if !IsRepoWork(task) || IsPushStep(task) {
		t.Fatal("the cleanup is repo work and is not a push step")
	}
	if ToolModeFor(task) != ToolAct {
		t.Fatal("repo work runs in the act tier on the owner's standing permission")
	}
	push := &Task{ManagerDomain: "engineering", Title: "Push README cleanup", Goal: "In " + RepoWorkDir + "/Archon: git push -u origin griffin/readme and open a pull request"}
	if !IsRepoWork(push) || !IsPushStep(push) {
		t.Fatal("the proposed push is repo work and is a push step")
	}
	snapshot := &Task{ManagerDomain: "engineering", Title: "REPO HYGIENE (monthly)", Goal: "Merged tracker snapshot", InputPayload: []byte(`{"kind":"tracker_snapshot"}`)}
	if IsRepoWork(snapshot) {
		t.Fatal("a stored tracker snapshot must not turn into repo work")
	}
	if !strings.Contains(RepoWorkRules(), "NEVER run `git push`") {
		t.Fatal("the rules must forbid pushing")
	}
}
