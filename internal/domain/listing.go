package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MinInternStipendINR is the owner's bar for a top listing: about ₹40,000 a month.
const MinInternStipendINR = 40000

// Listing is one verified internship posting. It is created only by code that fetched the posting from the
// company's board (or InternDock) and saw the role there.
type Listing struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"-"`
	Source     string    `json:"source"`
	ExternalID string    `json:"external_id,omitempty"`
	Company    string    `json:"company"`
	Role       string    `json:"role"`
	URL        string    `json:"url"`
	Location   string    `json:"location"`
	Pay        string    `json:"pay"`
	// Tier ranks the listing by stipend: A pays MinInternStipendINR or more, B ₹15,000 or more, C the rest.
	Tier          string     `json:"tier"`
	PostedAt      *time.Time `json:"posted_at,omitempty"`
	Stage         string     `json:"stage"`
	StageNote     string     `json:"stage_note"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	TaskID        *uuid.UUID `json:"task_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Listing stages: the application pipeline. Code moves a listing forward; nothing skips a step.
const (
	StageVerified   = "verified"   // the posting was fetched and the role is on it
	StageDrafted    = "drafted"    // the form was filled and is waiting for the owner
	StageApproved   = "approved"   // the owner approved the filled form
	StageSubmitting = "submitting" // the approved form is being submitted
	StageConfirmed  = "confirmed"  // a confirmation page or email proves it went in
	StageNeedsYou   = "needs_you"  // a login, CAPTCHA or question only the owner can answer
	StageClosed     = "closed"     // the posting is gone, or the owner closed it
)

var stageMoves = map[string][]string{
	StageVerified:   {StageDrafted, StageNeedsYou, StageClosed},
	StageDrafted:    {StageApproved, StageVerified, StageNeedsYou, StageClosed},
	StageApproved:   {StageSubmitting, StageNeedsYou, StageClosed},
	StageSubmitting: {StageConfirmed, StageNeedsYou, StageClosed},
	StageNeedsYou:   {StageVerified, StageDrafted, StageApproved, StageConfirmed, StageClosed},
	StageConfirmed:  {},
	StageClosed:     {StageVerified},
}

// CanMove reports whether a listing may go from one stage to another.
func CanMove(from, to string) bool {
	for _, s := range stageMoves[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ListingFromPosting turns a fetched posting into a verified listing.
func ListingFromPosting(userID uuid.UUID, source, tier string, p Posting, at time.Time) *Listing {
	l := &Listing{UserID: userID, Source: source, Tier: tier, Company: strings.TrimSpace(p.Company), Role: strings.TrimSpace(p.Title), URL: strings.TrimSpace(p.Link),
		Location: p.Location, Pay: p.Pay, Stage: StageVerified, VerifiedAt: &at, LastCheckedAt: &at}
	if !p.Posted.IsZero() {
		posted := p.Posted
		l.PostedAt = &posted
	}
	return l
}

// VerifiedListMarker opens the Internships list once Griffin writes it from the verified listings. A list that
// carries it holds every listing Griffin knows is real, so anything missing from it was never verified.
const VerifiedListMarker = "<!-- verified listings: written by Griffin from Internshala, Unstop and InternDock; edits are replaced daily -->"

// listStatus is a listing's Status cell on the Internships list, in the words the list's readers understand.
func listStatus(stage string) string {
	switch stage {
	case StageVerified:
		return "not applied"
	case StageConfirmed:
		return "applied"
	case StageClosed:
		return ListingClosedStatus
	default:
		return stage
	}
}

// ListingsListText is the Internships list written from the verified listings, best tier first. Closed listings
// stay on it (marked closed) so a task for one is dropped rather than worked on.
func ListingsListText(listings []*Listing) string {
	cell := func(s string) string {
		return strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " "))
	}
	sorted := append([]*Listing(nil), listings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		oi, oj := sorted[i].Stage == StageClosed, sorted[j].Stage == StageClosed
		if oi != oj {
			return !oi
		}
		return sorted[i].Tier < sorted[j].Tier
	})
	var b strings.Builder
	b.WriteString(VerifiedListMarker + "\n# Internships tracker\n\n")
	b.WriteString("| Tier | Company | Role | Location | Stipend | Source | Link | Status |\n|---|---|---|---|---|---|---|---|\n")
	for _, l := range sorted {
		fmt.Fprintf(&b, "| Tier %s | %s | %s | %s | %s | %s | %s | %s |\n", cell(l.Tier), cell(l.Company), cell(l.Role), cell(l.Location), cell(l.Pay), cell(l.Source), cell(l.URL), listStatus(l.Stage))
	}
	return b.String()
}

// ListingIsUnverified reports whether the Internships list is Griffin's verified list and this company is not on
// it: an application for it would be for a listing nobody ever saw on a real posting.
func ListingIsUnverified(text, company string) bool {
	if !strings.Contains(text, VerifiedListMarker) || strings.TrimSpace(company) == "" {
		return false
	}
	return !strings.Contains(strings.ToLower(text), "| "+strings.ToLower(strings.TrimSpace(company))+" |")
}
