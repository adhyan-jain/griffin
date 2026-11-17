package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvaluateOpportunity_WalkAwayRedFlags(t *testing.T) {
	tests := []struct {
		name          string
		opp           FreelanceOpportunity
		wantRule      string
		wantDisqualif bool
	}{
		{
			name: "deferred funding payment rejection",
			opp: FreelanceOpportunity{
				ID:          uuid.New(),
				Title:       "LLM Engineer for Stealth Startup",
				Description: "We will pay once we raise funding round from investors.",
				Channel:     FreelanceHNMonthly,
			},
			wantRule:      RedFlagDeferredFunding,
			wantDisqualif: true,
		},
		{
			name: "rxgpt style deferred promise",
			opp: FreelanceOpportunity{
				ID:          uuid.New(),
				Title:       "Agent Developer",
				Description: "Work on our AI tool, deferred compensation until funded next quarter.",
				Channel:     FreelanceContractorPost,
			},
			wantRule:      RedFlagDeferredFunding,
			wantDisqualif: true,
		},
		{
			name: "unpaid test work > 2h",
			opp: FreelanceOpportunity{
				ID:              uuid.New(),
				Title:           "Backend AI Integrator",
				Description:     "Complete our take-home assignment 8 hours before interview.",
				TestRequirement: "take-home assignment 8 hours",
				Channel:         FreelanceJobBoard,
			},
			wantRule:      RedFlagUnpaidTestExceeds2h,
			wantDisqualif: true,
		},
		{
			name: "equity or exposure in place of pay",
			opp: FreelanceOpportunity{
				ID:          uuid.New(),
				Title:       "Prompt Engineer",
				Description: "Great work for exposure and portfolio exposure, equity only.",
				Channel:     FreelanceHNMonthly,
			},
			wantRule:      RedFlagEquityOnly,
			wantDisqualif: true,
		},
		{
			name: "upfront fee or equipment purchase",
			opp: FreelanceOpportunity{
				ID:                 uuid.New(),
				Title:              "AI Contractor",
				Description:        "Pay an onboarding fee to access our API client environment.",
				RequiresUpfrontFee: true,
				Channel:            FreelanceAgencySubcontract,
			},
			wantRule:      RedFlagUpfrontFee,
			wantDisqualif: true,
		},
		{
			name: "undefined scope without written deliverables",
			opp: FreelanceOpportunity{
				ID:           uuid.New(),
				Title:        "General AI Helper",
				Description:  "We will figure out as we go, open ended tasks with no scope.",
				ScopeDefined: false,
				Channel:      FreelanceFreelancePlatform,
			},
			wantRule:      RedFlagUnwrittenScope,
			wantDisqualif: true,
		},
		{
			name: "credential access request",
			opp: FreelanceOpportunity{
				ID:             uuid.New(),
				Title:          "Automation Assistant",
				Description:    "Send us your password and github password for our workspace.",
				RequiresLogins: true,
				Channel:        FreelanceContractorPost,
			},
			wantRule:      RedFlagCredentialAccess,
			wantDisqualif: true,
		},
		{
			name: "off-platform escrow bypass pressure",
			opp: FreelanceOpportunity{
				ID:             uuid.New(),
				Title:          "Fast Upwork Contract",
				Description:    "Bypass the escrow and let us pay outside upwork before contract.",
				BypassesEscrow: true,
				Channel:        FreelanceFreelancePlatform,
			},
			wantRule:      RedFlagOffPlatformEscrowBypass,
			wantDisqualif: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eval := EvaluateOpportunity(tc.opp)
			if eval.Disqualified != tc.wantDisqualif {
				t.Fatalf("expected Disqualified=%v, got %v", tc.wantDisqualif, eval.Disqualified)
			}
			if eval.Recommendation != "DISQUALIFIED" {
				t.Fatalf("expected Recommendation=DISQUALIFIED, got %s", eval.Recommendation)
			}
			found := false
			for _, v := range eval.Violations {
				if v.Rule == tc.wantRule {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected violation rule %q in %+v", tc.wantRule, eval.Violations)
			}
		})
	}
}

func TestEvaluateOpportunity_GreenFlags(t *testing.T) {
	opp := FreelanceOpportunity{
		ID:                  uuid.New(),
		Title:               "RAG Pipeline Engineer",
		ClientName:          "TechFounder Inc",
		Channel:             FreelanceHNMonthly,
		Description:         "Looking for an engineer to build RAG retrieval pipeline. 50% deposit upfront, clear deliverables with definition of done, sole decision maker is hiring founder.",
		ScopeDefined:        true,
		SingleDecisionMaker: true,
		Deposit50Percent:    true,
	}

	eval := EvaluateOpportunity(opp)
	if eval.Disqualified {
		t.Fatalf("expected qualified opportunity, but got disqualified: %+v", eval.Violations)
	}
	if eval.Recommendation != "RECOMMENDED" {
		t.Fatalf("expected RECOMMENDED, got %s", eval.Recommendation)
	}
	if len(eval.GreenFlags) != 3 {
		t.Fatalf("expected 3 green flags, got %d: %+v", len(eval.GreenFlags), eval.GreenFlags)
	}
	if eval.ProofLinkSuggested.RepoName != "OS-Tutor-RAG" {
		t.Fatalf("expected proof link OS-Tutor-RAG, got %s", eval.ProofLinkSuggested.RepoName)
	}
}

func TestSelectProofLink(t *testing.T) {
	cases := []struct {
		channel  FreelanceChannel
		topic    string
		wantRepo string
	}{
		{
			channel:  FreelanceLocalBusiness,
			topic:    "Mom dealer supplier logistics",
			wantRepo: "AquaDrop",
		},
		{
			channel:  FreelanceHNMonthly,
			topic:    "Building semantic search and retrieval rag pipelines",
			wantRepo: "OS-Tutor-RAG",
		},
		{
			channel:  FreelanceCommunity,
			topic:    "Long context character narrative entity tracking",
			wantRepo: "StoryTrace",
		},
		{
			channel:  FreelanceJobBoard,
			topic:    "Interactive coding tutor environment",
			wantRepo: "lab-tutor",
		},
		{
			channel:  FreelanceAgencySubcontract,
			topic:    "Autonomous multi-agent execution worker engine",
			wantRepo: "Proteus",
		},
	}

	for _, c := range cases {
		t.Run(c.wantRepo, func(t *testing.T) {
			link := SelectProofLink(c.channel, c.topic)
			if link.RepoName != c.wantRepo {
				t.Fatalf("expected repo %q, got %q", c.wantRepo, link.RepoName)
			}
			if !strings.HasPrefix(link.URL, "https://github.com/AdhyanJain/") {
				t.Fatalf("expected github link for AdhyanJain, got %s", link.URL)
			}
		})
	}
}

func TestWeeklyScorecard(t *testing.T) {
	sc := WeeklyScorecard{
		WeekStarting:          time.Now().UTC(),
		TargetedPitchesSent:   15,
		CommunityAnswersGiven: 10,
		GitHubIssuesFixed:     0,
	}

	if sc.PitchesRemaining() != 5 {
		t.Errorf("expected 5 pitches remaining, got %d", sc.PitchesRemaining())
	}
	if sc.AnswersRemaining() != 4 {
		t.Errorf("expected 4 answers remaining, got %d", sc.AnswersRemaining())
	}
	if sc.IssuesRemaining() != 1 {
		t.Errorf("expected 1 issue remaining, got %d", sc.IssuesRemaining())
	}
	if sc.IsGoalMet() {
		t.Errorf("expected goals not met")
	}

	summary := sc.ProgressSummary()
	if !strings.Contains(summary, "Pitches: 15/20") || !strings.Contains(summary, "Community Answers: 10/14") || !strings.Contains(summary, "GitHub Issues: 0/1") {
		t.Errorf("unexpected summary text: %s", summary)
	}

	sc.TargetedPitchesSent = 20
	sc.CommunityAnswersGiven = 14
	sc.GitHubIssuesFixed = 1
	if !sc.IsGoalMet() {
		t.Errorf("expected goals met when counts reach targets")
	}
}
