package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AssessorVersion changes whenever the assessment rules change, so listings are re-assessed by the new rules (the
// verified model conclusions kept with each assessment are reused, so a rule change costs no model call).
const AssessorVersion = 1

// Employment classes.
const (
	EmploymentInternship = "internship"
	EmploymentFullTime   = "full_time"
	EmploymentUnknown    = "unknown"
)

// Role classes. Nothing is dropped for its class: only non_tech is scored down, and only by the score.
const (
	RoleBackend   = "backend"
	RoleFrontend  = "frontend"
	RoleFullstack = "fullstack"
	RoleAIML      = "ai_ml"
	RoleResearch  = "research"
	RoleInfra     = "infra"
	RoleDevtools  = "devtools"
	RoleData      = "data"
	RoleMobile    = "mobile"
	RoleSecurity  = "security"
	RoleOtherTech = "other_tech"
	RoleNonTech   = "non_tech"
	RoleUnknown   = "unknown"
)

// RoleClasses is every role class the schema allows.
var RoleClasses = map[string]bool{
	RoleBackend: true, RoleFrontend: true, RoleFullstack: true, RoleAIML: true, RoleResearch: true, RoleInfra: true,
	RoleDevtools: true, RoleData: true, RoleMobile: true, RoleSecurity: true, RoleOtherTech: true, RoleNonTech: true, RoleUnknown: true,
}

// Eligibility says whether the owner may apply. unknown is a real answer: the posting did not say, and the listing
// goes to the review queue instead of being dropped or assumed.
const (
	EligibilityConfirmed  = "confirmed"
	EligibilityLikely     = "likely"
	EligibilityUnknown    = "unknown"
	EligibilityIneligible = "ineligible"
)

// Location fits.
const (
	LocationIndia      = "india"
	LocationRemoteOK   = "remote_ok"
	LocationRelocation = "relocation"
	LocationOutside    = "outside"
	LocationUnknown    = "unknown"
)

// Graduation-year fits.
const (
	GradFit      = "fit"
	GradMismatch = "mismatch"
	GradUnknown  = "unknown"
)

// Value classes, best first.
const (
	ValueExceptional = "exceptional"
	ValueStrong      = "strong"
	ValueGood        = "good"
	ValueExploratory = "exploratory"
	ValueLow         = "low"
)

// Reason is one finding with the exact words of the posting that support it.
type Reason struct {
	Reason string `json:"reason"`
	Quote  string `json:"quote"`
}

// SkillMatch is one of the owner's skills the posting asks for, with the words that show it.
type SkillMatch struct {
	Skill string `json:"skill"`
	Quote string `json:"quote"`
}

// ValueComponent is one part of the career-value score. Unknown means the posting did not give what the component
// needs, so it was scored at the neutral midpoint and flagged; it is never filled in by guessing.
type ValueComponent struct {
	Name    string  `json:"name"`
	Max     float64 `json:"max"`
	Points  float64 `json:"points"`
	Reason  string  `json:"reason"`
	Quote   string  `json:"quote,omitempty"`
	Unknown bool    `json:"unknown"`
}

// Assessment is what Griffin concluded about one listing from its text.
type Assessment struct {
	ListingID uuid.UUID `json:"-"`
	UserID    uuid.UUID `json:"-"`
	// DescriptionHash is the listing's description hash at assessment time; AssessmentKey is that hash, or for a lead
	// without a description a hash of the fields it does have; InputHash adds the facts the score reads.
	DescriptionHash *string `json:"description_hash,omitempty"`
	AssessmentKey   string  `json:"-"`
	InputHash       string  `json:"-"`
	Version         int     `json:"-"`

	EmploymentClass    string   `json:"employment_class"`
	RoleClass          string   `json:"role_class"`
	Eligibility        string   `json:"eligibility"`
	EligibilityReasons []Reason `json:"eligibility_reasons"`
	LocationFit        string   `json:"location_fit"`
	GradYearFit        string   `json:"grad_year_fit"`
	// CompensationMonthlyINR is set only when the text states pay (0 for text that says unpaid); nil is unknown.
	CompensationMonthlyINR *float64         `json:"compensation_monthly_inr"`
	CompensationQuote      string           `json:"compensation_quote,omitempty"`
	Duration               *string          `json:"duration"`
	SkillsMatched          []SkillMatch     `json:"skills_matched"`
	ValueScore             int              `json:"value_score"`
	ValueBreakdown         []ValueComponent `json:"value_breakdown"`
	ValueClass             string           `json:"value_class"`
	AssessedAt             time.Time        `json:"assessed_at"`
	ModelUsed              *string          `json:"model_used,omitempty"`

	// ModelKey is the key a model call was attempted at; ModelOutput the conclusions from it that passed
	// verification. Both are internal.
	ModelKey    *string         `json:"-"`
	ModelOutput json.RawMessage `json:"-"`
}

// AssessmentKey is the key an assessment (and its model call) is cached under: the description hash, or for a lead
// without a description a hash of the fields it does have.
func AssessmentKey(l *Listing) string {
	if l.DescriptionHash != nil && *l.DescriptionHash != "" {
		return *l.DescriptionHash
	}
	return "meta:" + hashParts(l.Role, l.Company, l.Location, l.Pay)
}

// AssessmentInputHash covers everything the deterministic stages and the score read besides the description.
func AssessmentInputHash(l *Listing, companyKind string) string {
	return hashParts(AssessmentKey(l), l.Role, l.Company, l.Location, l.Pay, l.SourceKind, l.Verification, companyKind)
}

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strings.Join(strings.Fields(p), " ")))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// ValueClassFor maps a score to its class. Caps for thin evidence are applied by the assessor, not here.
func ValueClassFor(score int) string {
	switch {
	case score >= 80:
		return ValueExceptional
	case score >= 65:
		return ValueStrong
	case score >= 50:
		return ValueGood
	case score >= 35:
		return ValueExploratory
	}
	return ValueLow
}

// AssessmentSubject is one open listing and what the routine needs to decide whether it must be assessed: the
// employer's stored kind and the keys of its current assessment (empty when it has none).
type AssessmentSubject struct {
	Listing     *Listing
	CompanyKind string
	Assessed    bool
	Key         string
	InputHash   string
	Version     int
}

// Stale reports whether the listing must be (re-)assessed: it has no assessment, its description or the facts the
// score reads changed, or the rules have changed since.
func (s AssessmentSubject) Stale() bool {
	return !s.Assessed || s.Key != AssessmentKey(s.Listing) || s.InputHash != AssessmentInputHash(s.Listing, s.CompanyKind) || s.Version != AssessorVersion
}

// AssessedFilter narrows the listings read with their assessments. Empty lists mean no filter. Eligibility
// "unknown" also matches a listing that has not been assessed yet: it is waiting for review.
type AssessedFilter struct {
	Stages      []string
	Classes     []string
	Eligibility []string
	Limit       int
}
