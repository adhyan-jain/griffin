package domain

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ContactState is where the owner is with one person. Griffin never sends a message: the owner does, and records
// that here ("sent_manually", "replied", "opted_out").
type ContactState string

const (
	ContactNotContacted ContactState = "not_contacted"
	ContactDrafted      ContactState = "drafted"
	ContactApproved     ContactState = "approved"
	ContactSentManually ContactState = "sent_manually"
	ContactReplied      ContactState = "replied"
	ContactOptedOut     ContactState = "opted_out"
)

// Valid reports whether s is a known contact state.
func (s ContactState) Valid() bool {
	switch s {
	case ContactNotContacted, ContactDrafted, ContactApproved, ContactSentManually, ContactReplied, ContactOptedOut:
		return true
	}
	return false
}

// AlumniOutreachApprovalAction is the approval type of an outreach draft. Approving it records that the owner
// accepts the draft; it never sends anything, and no executor handles it.
const AlumniOutreachApprovalAction = "alumni_outreach"

// OutreachChannels are the channels a draft may be written for.
var OutreachChannels = []string{"linkedin", "email", "other"}

// MaxOutreachDraftRunes is the longest outreach draft the owner can store.
const MaxOutreachDraftRunes = 2000

// RoleFamily groups job titles. It is inferred from the title's words, never read from a source.
const (
	RoleFamilyTech       = "tech"
	RoleFamilyOperations = "operations"
)

var (
	opsTitle  = regexp.MustCompile(`(?i)\b(operations|supply chain|logistics|procurement|strategy|bizops|business operations|program manager)\b`)
	techTitle = regexp.MustCompile(`(?i)\b(engineer|engineering|developer|sde|swe|software|backend|back-end|frontend|front-end|full[- ]?stack|devops|sre|architect|cto|machine learning|ml|ai|data scien\w*|programmer)\b`)
)

// RoleFamilyOf guesses the family of a job title from its words: "tech", "operations", or "" when the title says
// neither. Callers must mark the result as inferred.
func RoleFamilyOf(title string) string {
	switch {
	case opsTitle.MatchString(title):
		return RoleFamilyOperations
	case techTitle.MatchString(title):
		return RoleFamilyTech
	}
	return ""
}

// Person is a person record: a canonical entity of kind person with its outreach fields.
type Person struct {
	ID                       uuid.UUID          `json:"id"`
	Name                     string             `json:"name"`
	ProfileURL               *string            `json:"profile_url"`
	Company                  string             `json:"company"`
	Title                    string             `json:"title"`
	Club                     string             `json:"club,omitempty"`
	Country                  string             `json:"country,omitempty"`
	GraduationYear           *int               `json:"graduation_year"`
	GraduationYearEvidenceID *uuid.UUID         `json:"graduation_year_evidence_id"`
	GraduationYearSource     string             `json:"graduation_year_source,omitempty"` // tracker_batch, evidence or ""
	RoleFamily               string             `json:"role_family,omitempty"`
	RoleFamilyInferred       bool               `json:"role_family_inferred"`
	DoNotContact             bool               `json:"do_not_contact"`
	ContactState             ContactState       `json:"contact_state"`
	LastVerifiedAt           *time.Time         `json:"last_verified_at"`
	Campus                   string             `json:"campus"`
	VITAffiliation           string             `json:"vit_affiliation"`
	Branch                   string             `json:"branch,omitempty"`
	Degree                   string             `json:"degree,omitempty"`
	GraduationDate           *time.Time         `json:"graduation_date,omitempty"`
	GraduationEligibility    string             `json:"graduation_eligibility"`
	ACMAffiliation           string             `json:"acm_vit_affiliation"`
	ACMRole                  string             `json:"acm_role,omitempty"`
	ACMTenure                string             `json:"acm_tenure,omitempty"`
	ACMSource                string             `json:"acm_source,omitempty"`
	ACMEvidenceExcerpt       string             `json:"acm_evidence_excerpt,omitempty"`
	NormalizedTitle          string             `json:"normalized_title,omitempty"`
	ProfessionalFunction     string             `json:"professional_function,omitempty"`
	Seniority                string             `json:"seniority,omitempty"`
	ClassificationConfidence int                `json:"classification_confidence"`
	CurrentCompanyID         *uuid.UUID         `json:"current_company_id,omitempty"`
	ContactabilityStatus     string             `json:"contactability_status"`
	PreferredChannel         string             `json:"preferred_channel"`
	PublicContactRoute       string             `json:"public_contact_route,omitempty"`
	PublicBusinessEmail      string             `json:"public_business_email,omitempty"`
	PriorityTier             string             `json:"priority_tier"`
	PriorityScore            int                `json:"priority_score"`
	PriorityRationale        string             `json:"priority_rationale,omitempty"`
	Employments              []PersonEmployment `json:"employments,omitempty"`
	Metadata                 map[string]any     `json:"metadata"`
	CreatedAt                time.Time          `json:"created_at"`
	UpdatedAt                time.Time          `json:"updated_at"`
}

// PersonEmployment tracks one job held by a person.
type PersonEmployment struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"-"`
	PersonID     uuid.UUID  `json:"person_id"`
	CompanyID    *uuid.UUID `json:"company_id,omitempty"`
	EmployerName string     `json:"employer_name"`
	Title        string     `json:"title"`
	StartDate    *time.Time `json:"start_date,omitempty"`
	EndDate      *time.Time `json:"end_date,omitempty"`
	IsCurrent    bool       `json:"is_current"`
	Location     string     `json:"location,omitempty"`
	Function     string     `json:"function,omitempty"`
	EvidenceID   *uuid.UUID `json:"evidence_id,omitempty"`
	Confidence   int        `json:"confidence"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AlumniImportRow is one row of the Alumni tracker, read for import. Only what the row says is set.
type AlumniImportRow struct {
	Name, Company, Role, Club, ProfileURL string
	// EvidenceURL and Quote are a public page (not LinkedIn) about the person and the exact words on it that show
	// their campus, batch or ACM role. Griffin fetches the page and records the evidence only if the words are there.
	EvidenceURL, Quote string
	GraduationYear     int // 0 when the row shows none
	ContactState       ContactState
	DoNotContact       bool
}

// AlumniStatusState maps a tracker Status cell to a contact state and whether it means do-not-contact. Unknown or
// empty text is "not contacted" and not DNC.
func AlumniStatusState(status string) (ContactState, bool) {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(s, "do not contact"), strings.Contains(s, "do-not-contact"), s == "dnc", strings.HasPrefix(s, "dnc "),
		strings.Contains(s, "opted out"), strings.Contains(s, "opt-out"), strings.Contains(s, "opt out"),
		strings.Contains(s, "unsubscribe"), strings.Contains(s, "asked not to"):
		return ContactOptedOut, true
	case strings.HasPrefix(s, "replied"):
		return ContactReplied, false
	case strings.HasPrefix(s, "sent"), strings.HasPrefix(s, "requested"):
		return ContactSentManually, false
	}
	return ContactNotContacted, false
}

// AlumniImportRows reads the Alumni tracker's table. A row with no LinkedIn profile link is left out: the profile
// link is what identifies a person, so a row without one cannot be imported without guessing.
func AlumniImportRows(text string) []AlumniImportRow {
	tbl, ok := tableIn(text)
	if !ok {
		return nil
	}
	lines := strings.Split(tbl, "\n")
	header := splitRow(lines[0])
	col := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	nameC, batchC, companyC, roleC, clubC, linkC, statusC := col("Name"), col("Batch"), col("Company"), col("Role"), col("Club"), col("LinkedIn"), col("Status")
	evidenceC, quoteC := col("Evidence"), col("Quote")
	var out []AlumniImportRow
	for _, l := range lines[2:] {
		cells := splitRow(l)
		get := func(i int) string {
			if i >= 0 && i < len(cells) {
				return strings.TrimSpace(cells[i])
			}
			return ""
		}
		link, ok := ProfileURL(get(linkC))
		name := strings.Join(strings.Fields(get(nameC)), " ")
		if !ok || name == "" {
			continue
		}
		year := 0
		if y, err := strconv.Atoi(get(batchC)); err == nil && y >= 1950 && y <= 2100 {
			year = y
		}
		club := get(clubC)
		if strings.EqualFold(club, "none") || club == "-" {
			club = ""
		}
		state, dnc := AlumniStatusState(get(statusC))
		out = append(out, AlumniImportRow{Name: name, Company: get(companyC), Role: get(roleC), Club: club, ProfileURL: link, GraduationYear: year, ContactState: state, DoNotContact: dnc,
			EvidenceURL: get(evidenceC), Quote: strings.Trim(get(quoteC), `"“”'`)})
	}
	return out
}

// DefaultGraduationCutoff is the graduation-year cutoff when the config sets none.
const DefaultGraduationCutoff = 2022

// DefaultCutoffDate is the standard 4-year graduation cutoff (26 November 2022).
var DefaultCutoffDate = time.Date(2022, 11, 26, 23, 59, 59, 0, time.UTC)

// GraduationCutoffDate is the cutoff for a configured cutoff year: 26 November of that year, end of day UTC. For
// 2022 it is DefaultCutoffDate. A year of zero or less gives the default.
func GraduationCutoffDate(year int) time.Time {
	if year <= 0 {
		return DefaultCutoffDate
	}
	return time.Date(year, 11, 26, 23, 59, 59, 0, time.UTC)
}

// Graduation eligibility constants.
const (
	CohortEligible       = "eligible"
	CohortBorderline2022 = "borderline_2022"
	CohortIneligible     = "ineligible"
	CohortUnknown        = "unknown"
)

// EvaluateGraduationEligibility computes candidate eligibility under the 4-year cutoff. It is the only graduation
// rule: an exact date on or before the cutoff is eligible; a year alone is eligible before the cutoff's year,
// borderline in it (the date is unknown) and ineligible after it; no year is unknown.
func EvaluateGraduationEligibility(year *int, exactDate *time.Time, cutoff time.Time) string {
	if exactDate != nil && !exactDate.IsZero() {
		if exactDate.Before(cutoff) || exactDate.Equal(cutoff) {
			return CohortEligible
		}
		return CohortIneligible
	}
	if year == nil || *year <= 0 {
		return CohortUnknown
	}
	cutoffYear := cutoff.Year()
	if *year < cutoffYear {
		return CohortEligible
	}
	if *year == cutoffYear {
		return CohortBorderline2022
	}
	return CohortIneligible
}

var (
	founderPattern = regexp.MustCompile(`(?i)\b(co-founder|cofounder|founder|founding engineer|founding member)\b`)
	execPattern    = regexp.MustCompile(`(?i)\b(cto|coo|chief technology|chief operating|vice president|vp|head of)\b`)
	managerPattern = regexp.MustCompile(`(?i)\b(engineering manager|operations manager|lead|tech lead|principal|staff)\b`)
	seniorPattern  = regexp.MustCompile(`(?i)\b(senior|sr\.?|lead)\b`)
	entryPattern   = regexp.MustCompile(`(?i)\b(intern|associate|junior|trainee)\b`)
)

// SeniorityOf infers the seniority level of a professional from their title.
func SeniorityOf(title string) string {
	t := strings.TrimSpace(title)
	switch {
	case founderPattern.MatchString(t):
		return "founder"
	case execPattern.MatchString(t):
		return "executive"
	case managerPattern.MatchString(t):
		return "lead"
	case seniorPattern.MatchString(t):
		return "senior"
	case entryPattern.MatchString(t):
		return "entry"
	case t != "":
		return "mid"
	}
	return "unknown"
}

// ClassifyRole performs semantic classification of a candidate's title into professional function, role family,
// seniority, and confidence.
func ClassifyRole(title string) (normTitle, function, roleFamily, seniority string, conf int) {
	normTitle = strings.Join(strings.Fields(title), " ")
	seniority = SeniorityOf(normTitle)
	family := RoleFamilyOf(normTitle)
	switch family {
	case RoleFamilyTech:
		function = "technology"
		roleFamily = RoleFamilyTech
		conf = 85
	case RoleFamilyOperations:
		function = "operations"
		roleFamily = RoleFamilyOperations
		conf = 85
	default:
		function = "other"
		roleFamily = ""
		conf = 40
	}
	if seniority == "founder" || seniority == "executive" {
		conf += 10
	}
	if conf > 100 {
		conf = 100
	}
	return normTitle, function, roleFamily, seniority, conf
}

// Priority tier constants.
const (
	PriorityP1     = "P1"
	PriorityP2     = "P2"
	PriorityP3     = "P3"
	PriorityP4     = "P4"
	PriorityReview = "REVIEW"
)

// EvaluatePriorityTier calculates a candidate's priority tier, numerical score, and rationale.
// It filters out IT services body shops (TCS, Infosys, etc.) and mega-corps (Amazon, Microsoft, etc.) where referrals do not provide startup hiring leverage.
func EvaluatePriorityTier(acmStatus, compPayPotential, employer string, isConfirmedVellore, isEligible, isBorderline bool) (tier string, score int, rationale string) {
	if excluded, reason := IsExcludedEmployer(employer); excluded {
		return PriorityReview, 10, "Excluded employer: " + reason
	}

	isACM := acmStatus == "confirmed"
	isHighComp := strings.ToUpper(compPayPotential) == "HIGH"

	// If campus or graduation is uncertain, route to REVIEW
	if !isConfirmedVellore || isBorderline || (!isEligible && !isBorderline) {
		return PriorityReview, 30, "Requires manual review: ambiguous campus or borderline 2022 graduation cutoff."
	}

	if isACM && isHighComp {
		return PriorityP1, 95, "P1: Confirmed ACM VIT alumnus at high-potential startup / high-compensation employer."
	}
	if isACM {
		return PriorityP2, 80, "P2: Confirmed ACM VIT alumnus at promising product company / startup."
	}
	if isHighComp {
		return PriorityP3, 70, "P3: Confirmed VIT Vellore alumnus at high-potential startup / high-compensation employer."
	}
	return PriorityP4, 55, "P4: Confirmed VIT Vellore alumnus at product company."
}

// ---- company observations ----

// Company observation properties: each is its own kind of fact and is never merged into another.
const (
	PropFunding             = "funding"
	PropRevenue             = "revenue"
	PropProfitability       = "profitability"
	PropAdvertisedPay       = "advertised_pay"
	PropEmployeeReportedPay = "employee_reported_pay"
	PropInternshipStipend   = "internship_stipend"
	PropHiringSignal        = "hiring_signal"
	PropProduct             = "product"
	PropMarket              = "market"
)

// CompanyProperties lists the properties an observation may have.
var CompanyProperties = []string{PropFunding, PropRevenue, PropProfitability, PropAdvertisedPay, PropEmployeeReportedPay,
	PropInternshipStipend, PropHiringSignal, PropProduct, PropMarket}

// IsCompanyProperty reports whether p is a known observation property.
func IsCompanyProperty(p string) bool {
	for _, k := range CompanyProperties {
		if k == p {
			return true
		}
	}
	return false
}

// Who a claim comes from.
const (
	OriginCompanyReported = "company_reported"
	OriginIndependent     = "independent"
	OriginUnknown         = "unknown"
)

// CompanyObservation is one dated, sourced value of one property of a company. A new value adds a row; nothing is
// overwritten, so the history stays.
type CompanyObservation struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"-"`
	CompanyID   uuid.UUID  `json:"company_id"`
	Property    string     `json:"property"`
	Value       string     `json:"value"`
	EvidenceID  *uuid.UUID `json:"evidence_id"`
	SourceURL   string     `json:"source_url"`
	ClaimOrigin string     `json:"claim_origin"`
	ObservedAt  time.Time  `json:"observed_at"`
	AgeDays     int        `json:"age_days"`
}

// ---- campus, ACM affiliation and LinkedIn URLs ----

// VIT campus values and affiliations as stored on a person.
const (
	CampusVellore = "vellore"
	CampusChennai = "chennai"
	CampusAP      = "ap"
	CampusBhopal  = "bhopal"
	CampusUnknown = "unknown"

	AffiliationConfirmedVellore = "confirmed_vellore"
	AffiliationUnconfirmed      = "unconfirmed"
	AffiliationOtherCampus      = "other_campus"
)

var (
	campusChennai = regexp.MustCompile(`\bchennai\b`)
	campusAP      = regexp.MustCompile(`\b(vit[- ]ap|amaravati|andhra)\b`)
	campusBhopal  = regexp.MustCompile(`\bbhopal\b`)
	campusVellore = regexp.MustCompile(`\bvellore\b`)
	vitName       = regexp.MustCompile(`\bvit\b`)
	vitFullName   = regexp.MustCompile(`vellore institute of technology`)
)

// ClassifyVITCampus reads the campus out of a school or affiliation text. It returns the campus and the VIT
// affiliation. A campus is named only when the text names it: bare "VIT" or "Vellore Institute of Technology"
// (the university's name, not the campus) is unknown/unconfirmed, because it cannot tell the campuses apart.
func ClassifyVITCampus(text string) (campus, vitAffiliation string) {
	t := strings.ToLower(text)
	switch {
	case campusChennai.MatchString(t):
		return CampusChennai, AffiliationOtherCampus
	case campusAP.MatchString(t):
		return CampusAP, AffiliationOtherCampus
	case campusBhopal.MatchString(t):
		return CampusBhopal, AffiliationOtherCampus
	}
	// The university's own name contains "Vellore"; take it out so only a named campus counts.
	rest := vitFullName.ReplaceAllString(t, " ")
	if campusVellore.MatchString(rest) && (vitName.MatchString(t) || vitFullName.MatchString(t)) {
		return CampusVellore, AffiliationConfirmedVellore
	}
	return CampusUnknown, AffiliationUnconfirmed
}

// ResolveACMAffiliation decides the stored ACM VIT affiliation. "confirmed" needs a claim of confirmed, a source
// that is an http(s) URL and a non-empty evidence excerpt; a confirmed claim missing either is "likely", and so is
// a club text that merely mentions ACM. Anything else is "unknown".
func ResolveACMAffiliation(claimed, source, excerpt string) string {
	c := strings.ToLower(strings.TrimSpace(claimed))
	if c == "confirmed" {
		u, err := url.Parse(strings.TrimSpace(source))
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && strings.TrimSpace(excerpt) != "" {
			return "confirmed"
		}
		return "likely"
	}
	if c == "likely" || strings.Contains(c, "acm") {
		return "likely"
	}
	return "unknown"
}

// CanonicalLinkedInURL reduces a LinkedIn profile link to one form, https://linkedin.com/in/<slug>: lower case,
// https, no "www.", no query, fragment, trailing slash or extra path. It returns "" for anything that is not a
// profile link. Two links to one profile are equal after it; two people are never merged by name.
func CanonicalLinkedInURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "linkedin.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) < 2 || strings.ToLower(parts[0]) != "in" || parts[1] == "" {
		return ""
	}
	return "https://linkedin.com/in/" + strings.ToLower(parts[1])
}
