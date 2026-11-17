package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// FreelanceChannel represents the acquisition channel for client and contract leads.
type FreelanceChannel string

const (
	FreelanceHNMonthly         FreelanceChannel = "hn_monthly"
	FreelanceProductHunt       FreelanceChannel = "product_hunt"
	FreelanceCommunity         FreelanceChannel = "community"
	FreelanceContractorPost    FreelanceChannel = "contractor_post"
	FreelanceJobBoard          FreelanceChannel = "job_board"
	FreelanceAgencySubcontract FreelanceChannel = "agency_subcontract"
	FreelanceGitHubIssues      FreelanceChannel = "github_issues"
	FreelanceFreelancePlatform FreelanceChannel = "freelance_platform"
	FreelanceLocalBusiness     FreelanceChannel = "local_business"
)

// AllFreelanceChannels lists all valid acquisition channels.
var AllFreelanceChannels = []FreelanceChannel{
	FreelanceHNMonthly,
	FreelanceProductHunt,
	FreelanceCommunity,
	FreelanceContractorPost,
	FreelanceJobBoard,
	FreelanceAgencySubcontract,
	FreelanceGitHubIssues,
	FreelanceFreelancePlatform,
	FreelanceLocalBusiness,
}

// OutreachProofLink holds genuine repository proof references to attach to technical pitches.
type OutreachProofLink struct {
	RepoName    string `json:"repo_name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	DomainTag   string `json:"domain_tag"`
}

// Authentic portfolio proof links for the owner.
var (
	ProofLinkStoryTrace = OutreachProofLink{
		RepoName:    "StoryTrace",
		URL:         "https://github.com/AdhyanJain/StoryTrace",
		Description: "Entity tracking, narrative consistency, and long-context agent memory systems.",
		DomainTag:   "memory_and_agents",
	}
	ProofLinkOSTutorRAG = OutreachProofLink{
		RepoName:    "OS-Tutor-RAG",
		URL:         "https://github.com/AdhyanJain/OS-Tutor-RAG",
		Description: "Retrieval-augmented technical question-answering systems.",
		DomainTag:   "rag_and_search",
	}
	ProofLinkLabTutor = OutreachProofLink{
		RepoName:    "lab-tutor",
		URL:         "https://github.com/AdhyanJain/lab-tutor",
		Description: "Interactive coding tutor and pedagogical agent environments.",
		DomainTag:   "education_and_agents",
	}
	ProofLinkProteus = OutreachProofLink{
		RepoName:    "Proteus",
		URL:         "https://github.com/AdhyanJain/Proteus",
		Description: "Autonomous multi-agent execution framework.",
		DomainTag:   "multi_agent",
	}
	ProofLinkAquaDrop = OutreachProofLink{
		RepoName:    "AquaDrop",
		URL:         "https://github.com/AdhyanJain/AquaDrop",
		Description: "Supply chain, inventory tracking, and dealer management for local businesses.",
		DomainTag:   "local_business",
	}
)

// SelectProofLink chooses the most authoritative proof link for a given acquisition channel and topic.
func SelectProofLink(channel FreelanceChannel, topic string) OutreachProofLink {
	lowerTopic := strings.ToLower(topic)
	if channel == FreelanceLocalBusiness || strings.Contains(lowerTopic, "supplier") || strings.Contains(lowerTopic, "dealer") || strings.Contains(lowerTopic, "local") {
		return ProofLinkAquaDrop
	}
	if strings.Contains(lowerTopic, "rag") || strings.Contains(lowerTopic, "retrieval") || strings.Contains(lowerTopic, "search") || strings.Contains(lowerTopic, "embeddings") {
		return ProofLinkOSTutorRAG
	}
	if strings.Contains(lowerTopic, "memory") || strings.Contains(lowerTopic, "entity") || strings.Contains(lowerTopic, "narrative") || strings.Contains(lowerTopic, "long context") {
		return ProofLinkStoryTrace
	}
	if strings.Contains(lowerTopic, "tutor") || strings.Contains(lowerTopic, "education") || strings.Contains(lowerTopic, "learning") {
		return ProofLinkLabTutor
	}
	// Default to Proteus or StoryTrace for general autonomous agent / LLM engineering
	if strings.Contains(lowerTopic, "agent") || channel == FreelanceCommunity || channel == FreelanceAgencySubcontract {
		return ProofLinkProteus
	}
	return ProofLinkStoryTrace
}

// Walk-Away Red Flag identifiers.
const (
	RedFlagDeferredFunding         = "deferred_funding_payment"
	RedFlagUnpaidTestExceeds2h     = "unpaid_test_exceeds_2h"
	RedFlagEquityOnly              = "equity_or_exposure_only"
	RedFlagUpfrontFee              = "upfront_fee_or_equipment"
	RedFlagUnwrittenScope          = "absence_of_written_scope"
	RedFlagCredentialAccess        = "personal_credential_request"
	RedFlagOffPlatformEscrowBypass = "off_platform_escrow_bypass"
)

// Green Flag identifiers.
const (
	GreenFlagDeposit50Percent    = "50_percent_upfront_deposit"
	GreenFlagCrispScope          = "crisp_definition_of_done"
	GreenFlagSingleDecisionMaker = "single_named_decision_maker"
)

// Regex heuristics for red-flag detection in freeform opportunity text.
var (
	reDeferredFunding = regexp.MustCompile(`(?i)(pay\s+(once|when|after)\s+we\s+raise|after\s+(the\s+)?(seed|series\s+[a-z]|funding)\s+round|upon\s+raising\s+funding|rxgpt|deferred\s+compensation\s+until\s+funded)`)
	reEquityExposure  = regexp.MustCompile(`(?i)(equity\s+only|in\s+exchange\s+for\s+equity|work\s+for\s+exposure|portfolio\s+exposure|unpaid\s+internship|rev-?share\s+only\b|no\s+cash\s+compensation)`)
	reUpfrontFee      = regexp.MustCompile(`(?i)(pay\s+(an?\s+)?(onboarding|training|registration)\s+fee|buy\s+(your\s+own\s+)?equipment\s+from\s+us|deposit\s+for\s+software|purchase\s+tools\s+upfront)`)
	reCredentials     = regexp.MustCompile(`(?i)(send\s+(us\s+)?(your\s+)?(password|login\s+credentials|google\s+login|github\s+password)|share\s+your\s+personal\s+account\s+login)`)
	reEscrowBypass    = regexp.MustCompile(`(?i)(bypass\s+(the\s+)?escrow|pay\s+outside\s+(upwork|contra)\s+before|off-?platform\s+before\s+(contract|initial\s+payment)|no\s+escrow)`)
	reUnpaidTestLong  = regexp.MustCompile(`(?i)(unpaid\s+test\s+(task|project|work)|take-?home\s+(test|assignment)\s+([3-9]|\d{2,})\s*h(ou)?rs?|unpaid\s+trial\s+(week|period|days?))`)
)

// FreelanceOpportunity captures one prospective client lead or contract post.
type FreelanceOpportunity struct {
	ID                  uuid.UUID        `json:"id"`
	Title               string           `json:"title"`
	ClientName          string           `json:"client_name"`
	Channel             FreelanceChannel `json:"channel"`
	URL                 string           `json:"url"`
	Description         string           `json:"description"`
	OfferedRate         string           `json:"offered_rate,omitempty"`
	PaymentTerms        string           `json:"payment_terms,omitempty"`
	TestRequirement     string           `json:"test_requirement,omitempty"`
	ScopeDefined        bool             `json:"scope_defined"`
	SingleDecisionMaker bool             `json:"single_decision_maker"`
	Deposit50Percent    bool             `json:"deposit_50_percent"`
	RequiresLogins      bool             `json:"requires_logins"`
	RequiresUpfrontFee  bool             `json:"requires_upfront_fee"`
	BypassesEscrow      bool             `json:"bypasses_escrow"`
	CreatedAt           time.Time        `json:"created_at"`
}

// WalkAwayViolation details one triggered red flag.
type WalkAwayViolation struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// GreenFlagMatch details one positive contract criterion met.
type GreenFlagMatch struct {
	Flag   string `json:"flag"`
	Detail string `json:"detail"`
}

// OpportunityEvaluation summarizes the automated vetting outcome.
type OpportunityEvaluation struct {
	OpportunityID      uuid.UUID           `json:"opportunity_id"`
	Disqualified       bool                `json:"disqualified"`
	Violations         []WalkAwayViolation `json:"violations"`
	GreenFlags         []GreenFlagMatch    `json:"green_flags"`
	Recommendation     string              `json:"recommendation"` // DISQUALIFIED, RECOMMENDED, QUALIFIED_PROCEED_WITH_CAUTION
	ProofLinkSuggested OutreachProofLink   `json:"proof_link_suggested"`
	EvaluatedAt        time.Time           `json:"evaluated_at"`
}

// EvaluateOpportunity performs rigorous anti-scam and qualification checks on a contract lead.
func EvaluateOpportunity(opp FreelanceOpportunity) OpportunityEvaluation {
	eval := OpportunityEvaluation{
		OpportunityID: opp.ID,
		Disqualified:  false,
		Violations:    make([]WalkAwayViolation, 0),
		GreenFlags:    make([]GreenFlagMatch, 0),
		EvaluatedAt:   time.Now().UTC(),
	}

	text := strings.Join([]string{opp.Title, opp.Description, opp.PaymentTerms, opp.TestRequirement}, " ")

	// 1. Check Deferred Funding Pay
	if reDeferredFunding.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagDeferredFunding,
			Reason: "Client offers payment contingent on future fundraising or seed round (violates RxGPT safeguard).",
		})
	}

	// 2. Check Unpaid Test Work > 2h
	if reUnpaidTestLong.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagUnpaidTestExceeds2h,
			Reason: "Unpaid test assignment or trial exceeds 2 hours without compensation.",
		})
	}

	// 3. Check Equity or Exposure Only
	if reEquityExposure.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagEquityOnly,
			Reason: "Contract substitutes equity, revenue share, or exposure for guaranteed monetary compensation.",
		})
	}

	// 4. Check Upfront Fees
	if opp.RequiresUpfrontFee || reUpfrontFee.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagUpfrontFee,
			Reason: "Lead requires upfront fee, onboarding charge, or mandatory equipment purchase.",
		})
	}

	// 5. Check Absence of Written Scope
	if !opp.ScopeDefined && (strings.Contains(strings.ToLower(text), "figure out as we go") || strings.Contains(strings.ToLower(text), "open ended tasks with no scope")) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagUnwrittenScope,
			Reason: "Scope is completely undefined with no written deliverables or acceptance criteria.",
		})
	}

	// 6. Check Personal Credential Access
	if opp.RequiresLogins || reCredentials.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagCredentialAccess,
			Reason: "Lead asks for personal account passwords, logins, or developer profiles.",
		})
	}

	// 7. Check Platform Escrow Bypass
	if opp.BypassesEscrow || reEscrowBypass.MatchString(text) {
		eval.Violations = append(eval.Violations, WalkAwayViolation{
			Rule:   RedFlagOffPlatformEscrowBypass,
			Reason: "Pressure to bypass platform escrow or work off-platform prior to initial payment funded.",
		})
	}

	// Evaluate Disqualification
	if len(eval.Violations) > 0 {
		eval.Disqualified = true
		eval.Recommendation = "DISQUALIFIED"
	}

	// Evaluate Green Flags
	if opp.Deposit50Percent || strings.Contains(strings.ToLower(text), "50% upfront") || strings.Contains(strings.ToLower(text), "50% deposit") {
		eval.GreenFlags = append(eval.GreenFlags, GreenFlagMatch{
			Flag:   GreenFlagDeposit50Percent,
			Detail: "50% upfront deposit guaranteed before commencing work.",
		})
	}

	if opp.ScopeDefined || strings.Contains(strings.ToLower(text), "definition of done") || strings.Contains(strings.ToLower(text), "clear deliverables") {
		eval.GreenFlags = append(eval.GreenFlags, GreenFlagMatch{
			Flag:   GreenFlagCrispScope,
			Detail: "Clear, crisp definition of done with documented milestones.",
		})
	}

	if opp.SingleDecisionMaker || strings.Contains(strings.ToLower(text), "sole decision maker") || strings.Contains(strings.ToLower(text), "hiring founder") {
		eval.GreenFlags = append(eval.GreenFlags, GreenFlagMatch{
			Flag:   GreenFlagSingleDecisionMaker,
			Detail: "Single named decision-maker who approves deliverables and authorizes payment.",
		})
	}

	// Final Recommendation
	if !eval.Disqualified {
		if len(eval.GreenFlags) >= 2 {
			eval.Recommendation = "RECOMMENDED"
		} else {
			eval.Recommendation = "QUALIFIED_PROCEED_WITH_CAUTION"
		}
	}

	// Attach Suggested Proof Link
	eval.ProofLinkSuggested = SelectProofLink(opp.Channel, text)

	return eval
}

// WeeklyScorecard monitors adherence to the 20/2/1 weekly freelance client acquisition discipline.
type WeeklyScorecard struct {
	WeekStarting          time.Time `json:"week_starting"`
	TargetedPitchesSent   int       `json:"targeted_pitches_sent"`   // Goal: 20
	CommunityAnswersGiven int       `json:"community_answers_given"` // Goal: 14 (2 per day)
	GitHubIssuesFixed     int       `json:"github_issues_fixed"`     // Goal: 1
}

const (
	WeeklyPitchGoal  = 20
	WeeklyAnswerGoal = 14
	WeeklyIssueGoal  = 1
)

// PitchesRemaining returns how many targeted pitches remain for the week.
func (s WeeklyScorecard) PitchesRemaining() int {
	if s.TargetedPitchesSent >= WeeklyPitchGoal {
		return 0
	}
	return WeeklyPitchGoal - s.TargetedPitchesSent
}

// AnswersRemaining returns how many public community answers remain for the week.
func (s WeeklyScorecard) AnswersRemaining() int {
	if s.CommunityAnswersGiven >= WeeklyAnswerGoal {
		return 0
	}
	return WeeklyAnswerGoal - s.CommunityAnswersGiven
}

// IssuesRemaining returns how many open source issues remain to be fixed this week.
func (s WeeklyScorecard) IssuesRemaining() int {
	if s.GitHubIssuesFixed >= WeeklyIssueGoal {
		return 0
	}
	return WeeklyIssueGoal - s.GitHubIssuesFixed
}

// IsGoalMet reports whether all 3 weekly goals have been fulfilled.
func (s WeeklyScorecard) IsGoalMet() bool {
	return s.PitchesRemaining() == 0 && s.AnswersRemaining() == 0 && s.IssuesRemaining() == 0
}

// ProgressSummary generates a concise text scorecard for briefings and notifications.
func (s WeeklyScorecard) ProgressSummary() string {
	return fmt.Sprintf("Weekly Freelance Scorecard: Pitches: %d/%d | Community Answers: %d/%d | GitHub Issues: %d/%d",
		s.TargetedPitchesSent, WeeklyPitchGoal,
		s.CommunityAnswersGiven, WeeklyAnswerGoal,
		s.GitHubIssuesFixed, WeeklyIssueGoal,
	)
}
