package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func cand(goal, answer string, ago time.Duration) ThreadCandidate {
	task := &domain.Task{ID: uuid.New(), Goal: goal, Title: goal, Status: domain.TaskStatusCompleted}
	return ThreadCandidate{Root: task, Latest: task, Answer: answer, Finished: t0.Add(-ago)}
}

func TestDecideThread(t *testing.T) {
	letter := cand("Draft a cover letter for the Acme backend internship",
		"Dear Acme team, I am excited to apply for the backend internship. My background in Go and distributed systems...", 4*time.Minute)

	cases := []struct {
		msg  string
		want ThreadAction
	}{
		// clearly a continuation
		{"make it shorter", ThreadContinue},
		{"also add my GitHub link", ThreadContinue},
		{"now send it", ThreadContinue},
		{"can you make the second paragraph friendlier", ThreadContinue},
		{"and then translate it to Hindi", ThreadContinue},
		{"change the tone to more formal", ThreadContinue},
		{"try again", ThreadContinue},
		{"add a line about my open source work to the letter", ThreadContinue},
		// clearly something new
		{"what's the weather in Pune tomorrow", ThreadNew},
		{"research the best laptops under 60000 and compare them", ThreadNew},
		{"who won the match yesterday", ThreadNew},
		{"find me three internships in Bengaluru", ThreadNew},
		{"start a new task: plan my weekend trip", ThreadNew},
		{"new question - how does leasing work", ThreadNew},
		{"thanks!", ThreadNew},
		{"", ThreadNew},
	}
	for _, c := range cases {
		got := DecideThread(c.msg, t0, []ThreadCandidate{letter})
		if got.Action != c.want {
			t.Errorf("%q: got %s (score %.2f, %v), want %s", c.msg, got.Action, got.Score, got.Reasons, c.want)
		}
	}
}

func TestSameTopicRaisesTheOddsAndFreshWordingLowersThem(t *testing.T) {
	laptops := cand("Compare laptops under 60000", "The Acer Swift Go 14 has the best screen and battery. The Lenovo IdeaPad is quieter.", 20*time.Minute)
	// no continuation wording, but plainly the same subject
	got := DecideThread("which of those laptops is the quietest", t0, []ThreadCandidate{laptops})
	if got.Action == ThreadNew || got.Candidate == nil {
		t.Errorf("a question about the earlier answer's subject should at least offer to continue: %+v", got)
	}
	// a fresh request that happens to share a word stays new
	got = DecideThread("research the best laptop bags for travel", t0, []ThreadCandidate{laptops})
	if got.Action == ThreadContinue {
		t.Errorf("a fresh research request must not be swallowed by a thread that shares one word: %+v", got)
	}
}

func TestOnlyRecentThreadsAreCandidatesAndTheBestOneWins(t *testing.T) {
	old := cand("Draft a cover letter", "Dear team...", 3*24*time.Hour)
	if got := DecideThread("make it shorter", t0, []ThreadCandidate{old}); got.Action != ThreadNew {
		t.Errorf("a thread older than a day is not continued by a bare \"it\": %+v", got)
	}
	letter := cand("Draft a cover letter for Acme", "Dear Acme team, I am excited to apply.", 90*time.Minute)
	laptops := cand("Compare laptops under 60000", "The Acer Swift Go 14 is the pick. The fan is quiet.", 3*time.Minute)
	got := DecideThread("which laptop has the quietest fan", t0, []ThreadCandidate{letter, laptops})
	if got.Candidate == nil || got.Candidate.Root != laptops.Root {
		t.Errorf("the thread about laptops should be chosen: %+v", got)
	}
	got = DecideThread("make the letter more formal", t0, []ThreadCandidate{letter, laptops})
	if got.Action != ThreadContinue || got.Candidate.Root != letter.Root {
		t.Errorf("naming the subject beats being most recent: %+v (%v)", got.Action, got.Reasons)
	}
	if len(got.Reasons) == 0 {
		t.Error("a decision must say why, so the owner can judge it")
	}
}

func TestADecisionNeverContinuesWithoutCandidatesOrWhenAsKedForNew(t *testing.T) {
	if got := DecideThread("also add more", t0, nil); got.Action != ThreadNew {
		t.Error("nothing to continue")
	}
	letter := cand("Draft a cover letter", "Dear team", time.Minute)
	if got := DecideThread("make it shorter but start a new task", t0, []ThreadCandidate{letter}); got.Action != ThreadNew {
		t.Error("an explicit request for something new always wins")
	}
	future := cand("x", "y", -time.Hour)
	if got := DecideThread("make it shorter", t0, []ThreadCandidate{future}); got.Action != ThreadNew {
		t.Error("a candidate that finished in the future is clock skew, not a thread")
	}
}
