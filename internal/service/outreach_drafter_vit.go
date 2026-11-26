package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/griffin/core/internal/domain"
)

// Placeholders the owner must replace when the owner profile lacks the fact.
const (
	placeholderName    = "[your name]"
	placeholderSchool  = "[your college]"
	placeholderProject = "[one line about what you're building]"
)

const maxShortNoteRunes = 300

// GenerateVITAlumniDraft writes a short connection note and a longer message for an alumnus. Sender facts come only
// from the owner profile; anything it lacks becomes a visible placeholder and the draft is flagged NeedsUserReview.
// It states only what is stored about the person, pays no compliments and never asks for a referral or a job.
// Do-not-contact, opted-out and excluded-employer people are refused.
func GenerateVITAlumniDraft(p *domain.Person, sender domain.OwnerProfile) (*domain.DraftProposal, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: person is nil", domain.ErrValidation)
	}
	if p.DoNotContact || p.ContactState == domain.ContactOptedOut {
		return nil, fmt.Errorf("%w: cannot draft outreach for contact marked do-not-contact or opted-out", domain.ErrConflict)
	}
	if excluded, reason := domain.IsExcludedEmployer(p.Company); excluded {
		return nil, fmt.Errorf("%w: cannot draft outreach for excluded employer: %s", domain.ErrValidation, reason)
	}

	first := "there"
	if f := strings.Fields(p.Name); len(f) > 0 {
		first = firstNameCap(f[0])
	}
	name := strings.TrimSpace(sender.FullName)
	if name == "" {
		name = placeholderName
	}
	school := strings.TrimSpace(sender.School)
	if school == "" {
		school = placeholderSchool
	}
	project, senderHasACM := senderFacts(sender)
	if project == "" {
		project = placeholderProject
	} else {
		project = "I've been working on " + project + "."
	}

	var acmLine string
	if p.ACMAffiliation == "confirmed" && isURL(p.ACMSource) {
		acmLine = "I saw you were part of ACM-VIT"
		if senderHasACM {
			acmLine += ", where I'm involved now"
		}
		acmLine += "."
	}
	var roleLine string
	if p.Title != "" && p.Company != "" {
		roleLine = fmt.Sprintf("I see you're working at %s (%s).", p.Company, p.Title)
	}

	intro := fmt.Sprintf("I'm %s from %s.", name, school)
	ask := "Would you be open to a brief chat or a bit of advice?"
	// Optional clauses are dropped, least important first, until the note fits.
	optional := []string{acmLine, roleLine, project}
	var shortNote string
	for drop := 0; drop <= len(optional); drop++ {
		parts := []string{fmt.Sprintf("Hi %s,", first), intro}
		for i, o := range optional {
			// drop the project first, then the role, then the ACM line
			if o != "" && i < len(optional)-drop {
				parts = append(parts, o)
			}
		}
		parts = append(parts, ask)
		shortNote = strings.Join(parts, " ")
		if utf8.RuneCountInString(shortNote) <= maxShortNoteRunes {
			break
		}
	}

	var warm strings.Builder
	fmt.Fprintf(&warm, "Hi %s,\n\n%s", first, intro)
	for _, o := range []string{acmLine, roleLine, project} {
		if o != "" {
			warm.WriteString(" " + o)
		}
	}
	warm.WriteString("\n\nIf you have 10 minutes sometime, I'd appreciate any advice from your experience, in a short chat or just by message. No problem at all if you're busy.\n\n")
	fmt.Fprintf(&warm, "Thanks,\n%s", name)

	needsReview := false
	for _, ph := range []string{placeholderName, placeholderSchool, placeholderProject} {
		if strings.Contains(shortNote, ph) || strings.Contains(warm.String(), ph) {
			needsReview = true
		}
	}

	confirmedVellore := p.VITAffiliation == domain.AffiliationConfirmedVellore
	verifiedFacts := []string{}
	if confirmedVellore {
		fact := "VIT Vellore campus confirmed; graduation eligibility: " + p.GraduationEligibility
		if p.GraduationYear != nil && *p.GraduationYear > 0 {
			fact += fmt.Sprintf(" (graduation year %d)", *p.GraduationYear)
		}
		verifiedFacts = append(verifiedFacts, fact+".")
	}
	if p.ACMAffiliation == "confirmed" && isURL(p.ACMSource) {
		fact := "ACM-VIT affiliation confirmed (source: " + p.ACMSource + ")"
		if p.ACMRole != "" {
			fact += ", role: " + p.ACMRole
		}
		verifiedFacts = append(verifiedFacts, fact+".")
	}
	if p.Title != "" && p.Company != "" {
		label := "self-reported current role"
		for _, e := range p.Employments {
			if e.IsCurrent && e.EvidenceID != nil {
				label = "current role, evidence " + e.EvidenceID.String()
				break
			}
		}
		verifiedFacts = append(verifiedFacts, fmt.Sprintf("%s: %s at %s.", label, p.Title, p.Company))
	}

	selectionReason := fmt.Sprintf("%s priority.", p.PriorityTier)
	if confirmedVellore {
		selectionReason += " Confirmed VIT Vellore campus."
	}
	if p.GraduationEligibility != "" {
		selectionReason += " Graduation eligibility: " + p.GraduationEligibility + "."
	}
	if p.PriorityRationale != "" {
		selectionReason += " " + p.PriorityRationale
	}

	excludedFacts := []string{
		"Compensation figures are not mentioned.",
		"Only public professional routes are used.",
	}
	if p.GraduationDate == nil && p.GraduationYear != nil {
		excludedFacts = append(excludedFacts, "Exact graduation month and day are unknown (year only).")
	}

	contactHandle := ""
	if p.ProfileURL != nil {
		contactHandle = *p.ProfileURL
	} else if p.PublicBusinessEmail != "" {
		contactHandle = p.PublicBusinessEmail
	}

	return &domain.DraftProposal{
		ShortNote:       shortNote,
		WarmDraft:       warm.String(),
		SelectionReason: selectionReason,
		VerifiedFacts:   verifiedFacts,
		ExcludedFacts:   excludedFacts,
		PreferredRoute:  p.PreferredChannel,
		ContactHandle:   contactHandle,
		NeedsUserReview: needsReview,
	}, nil
}

// senderFacts returns the project name of the first profile fact tagged "project", and whether any profile fact
// mentions ACM. Nothing else about the sender is claimed.
func senderFacts(s domain.OwnerProfile) (project string, acm bool) {
	for _, f := range s.Facts {
		if strings.Contains(strings.ToLower(f.Project+" "+f.Text+" "+strings.Join(f.Tags, " ")), "acm") {
			acm = true
		}
		if project != "" || strings.TrimSpace(f.Project) == "" {
			continue
		}
		for _, t := range f.Tags {
			if strings.EqualFold(t, "project") {
				project = strings.TrimSpace(f.Project)
			}
		}
	}
	return project, acm
}

func isURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// firstNameCap cuts an absurdly long first name to 40 runes so the note fits.
func firstNameCap(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40])
	}
	return s
}
