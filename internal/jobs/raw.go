// Package jobs holds the canonical shape every opportunity source returns, so discovery, verification, matching,
// ranking and application preparation can be built and tested as separate stages.
package jobs

import "time"

// SourceKind says how close a record is to the employer. Only an employer posting can be verified; an aggregator
// record is a lead until the employer's own posting is fetched.
type SourceKind string

const (
	EmployerATS  SourceKind = "employer_ats"  // the employer's applicant-tracking board (Greenhouse, Lever, ...)
	EmployerSite SourceKind = "employer_site" // the employer's own careers page (e.g. schema.org JobPosting)
	Aggregator   SourceKind = "aggregator"    // Internshala, Unstop, InternDock, job alerts: third-party leads
)

// RawJob is one posting exactly as a source returned it. Unknown values stay empty or nil: nothing is guessed.
type RawJob struct {
	Source     string     // "greenhouse", "lever", "ashby", "smartrecruiters", "workday", "recruitee", "workable", "careers_jsonld", "internshala", "unstop", "interndock"
	SourceKind SourceKind //
	BoardToken string     // the board or company slug the job was read from ("" for aggregators)
	ExternalID string     // the source's own id for the posting

	EmployerName   string
	EmployerDomain string // e.g. "stripe.com"; "" when the source does not say

	Title          string
	URL            string   // the original posting URL as the source gives it (employer URL for ATS/site sources)
	ApplyURL       string   // "" when the source gives no separate apply link
	Locations      []string // verbatim location strings
	Remote         *bool    // nil: the source does not say
	EmploymentType string   // verbatim ("Intern", "Full-time", ...); "" unknown
	Department     string
	Description    string // plain text of the full description; "" when the source gives only a summary

	PostedAt     *time.Time
	UpdatedAt    *time.Time
	Deadline     *time.Time
	Compensation string // verbatim pay text ("₹ 30,000 /month", "$45/hr"); "" unknown

	FetchedAt time.Time
}

// ScanStatus is how a read of one board or page went. A failed or partial read never means "no openings".
type ScanStatus string

const (
	ScanOK      ScanStatus = "ok"
	ScanPartial ScanStatus = "partial" // some pages read, some failed: closures must not be inferred
	ScanFailed  ScanStatus = "failed"
)

// Scan is the result of reading one source target (an ATS board, a careers page, an aggregator search).
type Scan struct {
	Source     string
	Target     string // board token, URL or search term
	Status     ScanStatus
	Jobs       []RawJob
	Pages      int
	Error      string // "" when Status is ok
	StartedAt  time.Time
	FinishedAt time.Time
}

// Complete reports whether the scan read the whole target, the only case in which a missing job may be closed.
func (s Scan) Complete() bool { return s.Status == ScanOK }
