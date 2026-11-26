package domain

import (
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// Extended outreach target statuses for the VIT Alumni campaign lifecycle.
const (
	TargetStatusDiscovered      = "DISCOVERED"
	TargetStatusVerified        = "VERIFIED"
	TargetStatusQualified       = "QUALIFIED"
	TargetStatusPrioritised     = "PRIORITISED"
	TargetStatusDrafted         = "DRAFTED"
	TargetStatusNeedsUserReview = "NEEDS_USER_REVIEW"
	TargetStatusApproved        = "APPROVED"
	TargetStatusSentByUser      = "SENT_BY_USER"
	TargetStatusReplied         = "REPLIED"
	TargetStatusFollowUp        = "FOLLOW_UP"
	TargetStatusClosed          = "CLOSED"
	TargetStatusDoNotContact    = "DO_NOT_CONTACT"
	TargetStatusOptedOut        = "OPTED_OUT"
	TargetStatusInvalidContact  = "INVALID_CONTACT"
	TargetStatusDuplicate       = "DUPLICATE"
	TargetStatusNeedsResearch   = "NEEDS_RESEARCH"
)

// TargetExtendedStatusValid reports whether s is a valid extended target status.
func TargetExtendedStatusValid(s string) bool {
	switch s {
	case TargetStatusDiscovered, TargetStatusVerified, TargetStatusQualified, TargetStatusPrioritised,
		TargetStatusDrafted, TargetStatusNeedsUserReview, TargetStatusApproved, TargetStatusSentByUser,
		TargetStatusReplied, TargetStatusFollowUp, TargetStatusClosed, TargetStatusDoNotContact,
		TargetStatusOptedOut, TargetStatusInvalidContact, TargetStatusDuplicate, TargetStatusNeedsResearch:
		return true
	}
	return false
}

// VITCampaignStats counts the owner's own people records and campaign targets. Every field is a count of stored
// rows; nothing is estimated or hand-rated.
type VITCampaignStats struct {
	TotalPeople          int       `json:"total_people"`
	ConfirmedVellore     int       `json:"confirmed_vellore"`
	EligibleCohort       int       `json:"eligible_cohort"` // graduation_eligibility = eligible
	Borderline2022       int       `json:"borderline_2022"` // graduated 2022 without a month
	ConfirmedACM         int       `json:"confirmed_acm"`
	LikelyACM            int       `json:"likely_acm"`
	TechRoles            int       `json:"tech_roles"`
	OpsRoles             int       `json:"ops_roles"`
	WithPayEvidence      int       `json:"with_pay_evidence"` // company has an advertised or employee-reported pay observation
	PubliclyContactable  int       `json:"publicly_contactable"`
	Unverified           int       `json:"unverified"` // campus not confirmed as Vellore, or not eligible by graduation date
	DoNotContactCount    int       `json:"do_not_contact_count"`
	TierP1Count          int       `json:"tier_p1_count"`
	TierP2Count          int       `json:"tier_p2_count"`
	TierP3Count          int       `json:"tier_p3_count"`
	TierP4Count          int       `json:"tier_p4_count"`
	TierReviewCount      int       `json:"tier_review_count"`
	DraftsReady          int       `json:"drafts_ready"`
	NeedsUserReview      int       `json:"needs_user_review"`
	Approved             int       `json:"approved"`
	MessagesSentManually int       `json:"messages_sent_manually"`
	RepliesReceived      int       `json:"replies_received"`
	LastRefreshedAt      time.Time `json:"last_refreshed_at"`
}

// VITQueueBuild says what building the queue from the owner's records did.
type VITQueueBuild struct {
	Considered        int               `json:"considered"`
	Queued            int               `json:"queued"`
	NeedsUserReview   int               `json:"needs_user_review"` // queued with a placeholder the owner must fill
	SkippedDNC        int               `json:"skipped_do_not_contact"`
	SkippedExcluded   int               `json:"skipped_excluded_employer"`
	SkippedUnverified int               `json:"skipped_unverified"` // not confirmed Vellore and eligible
	SkippedNoProfile  int               `json:"skipped_no_profile_url"`
	Stats             *VITCampaignStats `json:"stats"`
}

// DraftProposal contains dual-variant personalised drafts and verifiable selection evidence.
type DraftProposal struct {
	ShortNote       string   `json:"short_note"`       // strictly <= 300 runes for LinkedIn connection note
	WarmDraft       string   `json:"warm_draft"`       // richer draft for InMail or direct email
	SelectionReason string   `json:"selection_reason"` // concise rationale for selecting this candidate
	VerifiedFacts   []string `json:"verified_facts"`   // facts backed by primary sources
	ExcludedFacts   []string `json:"excluded_facts"`   // facts withheld due to ambiguity
	PreferredRoute  string   `json:"preferred_route"`  // linkedin, email
	ContactHandle   string   `json:"contact_handle"`   // profile url or email
	// NeedsUserReview is true when the draft holds a placeholder (e.g. "[your name]") the owner must fill first.
	NeedsUserReview bool `json:"needs_user_review"`
}

// VITCampaignTarget represents an enriched outreach target in the campaign queue.
type VITCampaignTarget struct {
	ID                      uuid.UUID      `json:"id"`
	CampaignID              uuid.UUID      `json:"campaign_id"`
	PersonID                uuid.UUID      `json:"person_id"`
	Name                    string         `json:"name"`
	ProfileURL              string         `json:"profile_url"`
	Company                 string         `json:"company"`
	Title                   string         `json:"title"`
	Campus                  string         `json:"campus"`
	GraduationYear          *int           `json:"graduation_year,omitempty"`
	GraduationEligibility   string         `json:"graduation_eligibility"`
	ACMAffiliation          string         `json:"acm_vit_affiliation"`
	ACMRole                 string         `json:"acm_role,omitempty"`
	PriorityTier            string         `json:"priority_tier"`
	PriorityScore           int            `json:"priority_score"`
	PriorityRationale       string         `json:"priority_rationale,omitempty"`
	PayPotential            string         `json:"pay_potential"`
	FundingStage            string         `json:"funding_stage,omitempty"`
	Industry                string         `json:"industry,omitempty"`
	ExtendedStatus          string         `json:"extended_status"`
	ShortNote               string         `json:"short_note,omitempty"`
	AlternativeDraft        string         `json:"alternative_draft,omitempty"`
	SelectionReason         string         `json:"selection_reason,omitempty"`
	PersonalisationEvidence map[string]any `json:"personalisation_evidence,omitempty"`
	DoNotContact            bool           `json:"do_not_contact"`
	SentAt                  *time.Time     `json:"sent_at,omitempty"`
	RepliedAt               *time.Time     `json:"replied_at,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

// LinkedInSearchQuery contains pre-composed native Boolean search queries for manual sourcing.
type LinkedInSearchQuery struct {
	Category       string `json:"category"`
	Query          string `json:"query"`
	TargetAudience string `json:"target_audience"`
	URL            string `json:"url"`
	Instructions   string `json:"instructions"`
}

// BuildLinkedInURL generates a copy-ready LinkedIn people search URL.
func BuildLinkedInURL(keywords string) string {
	base := "https://www.linkedin.com/search/results/people/?"
	params := url.Values{}
	params.Set("keywords", keywords)
	return base + params.Encode()
}

// GenerateLinkedInQueries generates native search queries adhering to LinkedIn's operator syntax.
func GenerateLinkedInQueries() []LinkedInSearchQuery {
	queries := []struct {
		category string
		target   string
		keywords string
		instruct string
	}{
		{
			category: "P1: ACM-VIT High-Growth Startups & Founders",
			target:   "ACM VIT alumni who founded or lead engineering/ops at high-growth startups",
			keywords: `("ACM-VIT" OR "ACM VIT" OR "ACM Student Chapter") AND ("Vellore Institute of Technology") AND ("Founder" OR "Co-Founder" OR "CTO" OR "VP Engineering" OR "Staff" OR "Lead")`,
			instruct: "Paste into LinkedIn Search. Filter 'People' and review profile locations and graduation dates <= 2022.",
		},
		{
			category: "P1/P2: ACM-VIT Senior Software & Infrastructure Engineers",
			target:   "ACM VIT core members in backend, cloud, distributed systems, and ML engineering",
			keywords: `("ACM-VIT" OR "ACM VIT") AND ("Vellore Institute of Technology") AND ("Software Engineer" OR "SDE" OR "Backend" OR "Platform" OR "Infrastructure" OR "DevOps")`,
			instruct: "Targeting engineering leads and senior SWEs graduated on or before 2022.",
		},
		{
			category: "P3: VIT Vellore at B2B Procurement / Supply Chain Startups",
			target:   "VIT Vellore alumni working in ProcureYard archetype companies (procurement, vertical SaaS, logistics)",
			keywords: `("VIT" OR "Vellore Institute of Technology") AND ("Vellore") AND ("ProcureYard" OR "Zetwerk" OR "Infra.Market" OR "OfBusiness" OR "Bizongo" OR "Procol" OR "Moglix" OR "Porter")`,
			instruct: "Identifies alumni inside our high-conviction startup sectors.",
		},
		{
			category: "P3/P4: VIT Vellore Developer Infrastructure & FinTech",
			target:   "VIT Vellore alumni at high-paying devtools, infra, and fintech startups",
			keywords: `("Vellore Institute of Technology" OR "VIT Vellore") AND ("Hasura" OR "Appsmith" OR "SigNoz" OR "FamPay" OR "Razorpay" OR "Cashfree" OR "Chargebee")`,
			instruct: "Focus on engineers who joined during high-growth phases.",
		},
		{
			category: "P4: VIT Vellore Operations & Supply Chain Leaders",
			target:   "VIT Vellore alumni leading operations, procurement, program management, or strategy",
			keywords: `("Vellore Institute of Technology") AND ("Vellore") AND ("Operations Manager" OR "Supply Chain" OR "Procurement Manager" OR "Program Manager" OR "BizOps")`,
			instruct: "Target operations and business operations leaders in tech startups.",
		},
	}

	out := make([]LinkedInSearchQuery, 0, len(queries))
	for _, q := range queries {
		out = append(out, LinkedInSearchQuery{
			Category:       q.category,
			Query:          q.keywords,
			TargetAudience: q.target,
			URL:            BuildLinkedInURL(q.keywords),
			Instructions:   q.instruct,
		})
	}
	return out
}

// EnforceTargetDNC checks whether a target cannot be contacted.
func EnforceTargetDNC(target *VITCampaignTarget) error {
	if target == nil {
		return fmt.Errorf("%w: target is nil", ErrValidation)
	}
	if target.DoNotContact || target.ExtendedStatus == TargetStatusDoNotContact || target.ExtendedStatus == TargetStatusOptedOut {
		return fmt.Errorf("%w: contact is marked do-not-contact or opted-out", ErrConflict)
	}
	return nil
}

// ValidateTargetTransition validates status transitions under fail-closed DNC rules and the lifecycle state machine.
func ValidateTargetTransition(current, next string, isDNC bool) error {
	if isDNC && next != TargetStatusDoNotContact && next != TargetStatusOptedOut && next != TargetStatusClosed {
		return fmt.Errorf("%w: target is marked DNC and cannot transition to %s", ErrConflict, next)
	}
	if !TargetExtendedStatusValid(next) {
		return fmt.Errorf("%w: invalid target status %s", ErrValidation, next)
	}
	// Opting out is always allowed; nothing leaves do-not-contact except staying there.
	if next == TargetStatusDoNotContact || next == TargetStatusOptedOut {
		return nil
	}
	if current == TargetStatusDoNotContact || current == TargetStatusOptedOut {
		return fmt.Errorf("%w: target is %s and cannot move to %s", ErrConflict, current, next)
	}
	ok := false
	switch next {
	case TargetStatusApproved:
		ok = current == TargetStatusDrafted || current == TargetStatusNeedsUserReview
	case TargetStatusSentByUser:
		ok = current == TargetStatusApproved
	case TargetStatusReplied:
		ok = current == TargetStatusSentByUser || current == TargetStatusFollowUp
	case TargetStatusFollowUp:
		ok = current == TargetStatusSentByUser || current == TargetStatusReplied
	case TargetStatusClosed:
		ok = true
	default: // pipeline statuses before approval
		switch current {
		case TargetStatusApproved, TargetStatusSentByUser, TargetStatusReplied, TargetStatusFollowUp, TargetStatusClosed:
		default:
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%w: cannot move target from %q to %s", ErrConflict, current, next)
	}
	return nil
}
