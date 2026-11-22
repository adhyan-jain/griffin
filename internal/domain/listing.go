package domain

import (
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
