package domain

import (
	"time"

	"github.com/google/uuid"
)

// Verification says how far a listing has been checked against the employer. Only an employer's own posting,
// fetched by code, makes a listing employer_verified; an aggregator listing is a lead however good it looks.
const (
	VerificationLead     = "lead"
	VerificationEmployer = "employer_verified"
)

// Source kinds, as stored on a listing (they mirror jobs.SourceKind).
const (
	SourceKindEmployerATS  = "employer_ats"
	SourceKindEmployerSite = "employer_site"
	SourceKindAggregator   = "aggregator"
)

// Company statuses. A company first seen on an aggregator is only a candidate; it becomes active when its own
// board or site yields a posting. An ignored company stays ignored.
const (
	CompanyCandidate = "candidate"
	CompanyActive    = "active"
	CompanyIgnored   = "ignored"
)

// Company is an employer, once, however many sources name it.
type Company struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"-"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalized_name"`
	Domain         *string   `json:"domain,omitempty"`
	Kind           string    `json:"kind"`
	DiscoveredVia  string    `json:"discovered_via"`
	EvidenceURL    *string   `json:"evidence_url,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ATS board statuses.
const (
	BoardActive  = "active"
	BoardBroken  = "broken"
	BoardRetired = "retired"
)

// BoardBrokenAfter is how many failed scans in a row mark a board broken. One bad night is not a dead board.
const BoardBrokenAfter = 5

// ATSBoard is one employer board Griffin reads (a Greenhouse token, a Lever slug, a careers page URL).
type ATSBoard struct {
	ID                  uuid.UUID  `json:"id"`
	UserID              uuid.UUID  `json:"-"`
	CompanyID           *uuid.UUID `json:"company_id,omitempty"`
	Provider            string     `json:"provider"`
	TokenOrURL          string     `json:"token_or_url"`
	Status              string     `json:"status"`
	LastScanAt          *time.Time `json:"last_scan_at,omitempty"`
	LastOKAt            *time.Time `json:"last_ok_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

// ATSProviders are the board providers the schema allows.
var ATSProviders = map[string]bool{
	"greenhouse": true, "lever": true, "lever_eu": true, "ashby": true, "smartrecruiters": true,
	"workday": true, "recruitee": true, "workable": true, "careers_jsonld": true,
}

// SourceScan is one recorded scan of one source target: the scan history and the source health.
type SourceScan struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"-"`
	Source     string    `json:"source"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	JobsSeen   int       `json:"jobs_seen"`
	Pages      int       `json:"pages"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// SourceHealth is the latest scan of one source target, with how many scans in a row have failed.
type SourceHealth struct {
	Source        string     `json:"source"`
	Target        string     `json:"target"`
	LastStatus    string     `json:"last_status"`
	LastError     string     `json:"last_error,omitempty"`
	LastScanAt    time.Time  `json:"last_scan_at"`
	LastOKAt      *time.Time `json:"last_ok_at,omitempty"`
	JobsSeen      int        `json:"jobs_seen"`
	Pages         int        `json:"pages"`
	FailedSinceOK int        `json:"failed_since_ok"`
	Scans         int        `json:"scans"`
}

// JobSnapshot is one capture of what a listing said when something about it changed.
type JobSnapshot struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"-"`
	ListingID        uuid.UUID  `json:"listing_id"`
	CapturedAt       time.Time  `json:"captured_at"`
	DescriptionHash  *string    `json:"description_hash,omitempty"`
	CompensationText *string    `json:"compensation_text,omitempty"`
	Deadline         *time.Time `json:"deadline,omitempty"`
	Title            string     `json:"title"`
	Location         string     `json:"location"`
}

// ListingEvidence is a claim about a listing with the exact words and page that support it.
type ListingEvidence struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"-"`
	ListingID  uuid.UUID `json:"listing_id"`
	Claim      string    `json:"claim"`
	Value      string    `json:"value"`
	Quote      string    `json:"quote"`
	SourceURL  string    `json:"source_url"`
	CapturedAt time.Time `json:"captured_at"`
}

// SourceRank orders source kinds by closeness to the employer: when two sources carry one opportunity, the higher
// rank is the primary record and supplies its URL.
func SourceRank(kind string) int {
	switch kind {
	case SourceKindEmployerATS:
		return 3
	case SourceKindEmployerSite:
		return 2
	default:
		return 1
	}
}

// IsEmployerKind reports whether a source kind is the employer's own (ATS or site).
func IsEmployerKind(kind string) bool {
	return kind == SourceKindEmployerATS || kind == SourceKindEmployerSite
}
