package domain

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// SourceKindLinkedInSelfReported marks a claim whose only source is a LinkedIn page: what the person wrote about
// themselves. It never confirms a campus or an ACM role.
const (
	SourceKindLinkedInSelfReported = "linkedin_self_reported"
	SourceKindIndependent          = "independent"
)

// CampusClaim, GraduationClaim, ACMClaim and ContactRouteClaim are the owner's evidence-backed claims about a person.
type CampusClaim struct {
	Value     string `json:"value"`
	SourceURL string `json:"source_url"`
	Excerpt   string `json:"excerpt"`
}

type GraduationClaim struct {
	Year      int    `json:"year"`
	Date      string `json:"date"`
	SourceURL string `json:"source_url"`
	Excerpt   string `json:"excerpt"`
}

type ACMClaim struct {
	Status    string `json:"status"`
	Role      string `json:"role"`
	Tenure    string `json:"tenure"`
	SourceURL string `json:"source_url"`
	Excerpt   string `json:"excerpt"`
}

type ContactRouteClaim struct {
	Value     string `json:"value"`
	SourceURL string `json:"source_url"`
}

// PersonVerification is the body of PATCH /alumni/{id}/verification. Every part is optional; each given part must
// carry its evidence.
type PersonVerification struct {
	Campus       *CampusClaim       `json:"campus"`
	Graduation   *GraduationClaim   `json:"graduation"`
	ACM          *ACMClaim          `json:"acm"`
	ContactRoute *ContactRouteClaim `json:"contact_route"`
}

// VerifiedFields is a validated verification, ready to store. A nil pointer means "leave the column as it is".
type VerifiedFields struct {
	Campus, VITAffiliation      *string
	GraduationYear              *int
	GraduationDate              *time.Time
	ACMAffiliation              *string
	ACMRole, ACMTenure          *string
	ACMSource, ACMExcerpt       *string
	ContactRoute, BusinessEmail *string
	Provenance                  map[string]any
}

// SourceKindOf says whether a source URL is LinkedIn (self-reported) or independent.
func SourceKindOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return SourceKindIndependent
	}
	h := strings.ToLower(u.Hostname())
	if h == "linkedin.com" || strings.HasSuffix(h, ".linkedin.com") {
		return SourceKindLinkedInSelfReported
	}
	return SourceKindIndependent
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func needEvidence(claim, src, excerpt string) error {
	if !validHTTPURL(src) {
		return fmt.Errorf("%w: %s needs a source_url that is an http(s) URL", ErrValidation, claim)
	}
	if strings.TrimSpace(excerpt) == "" {
		return fmt.Errorf("%w: %s needs a non-empty excerpt", ErrValidation, claim)
	}
	return nil
}

func vptr[T any](v T) *T { return &v }

// Resolve checks every given claim and decides what is stored. Only a non-LinkedIn source confirms a campus or an
// ACM role: a LinkedIn source leaves the affiliation unconfirmed and an ACM claim at most "likely".
func (v PersonVerification) Resolve(now time.Time) (*VerifiedFields, error) {
	if v.Campus == nil && v.Graduation == nil && v.ACM == nil && v.ContactRoute == nil {
		return nil, fmt.Errorf("%w: give campus, graduation, acm or contact_route", ErrValidation)
	}
	out := &VerifiedFields{Provenance: map[string]any{}}
	stamp := func(src, excerpt string) map[string]any {
		m := map[string]any{"source_url": strings.TrimSpace(src), "source_kind": SourceKindOf(src), "verified_by": "owner", "verified_at": now.UTC().Format(time.RFC3339)}
		if excerpt != "" {
			m["excerpt"] = strings.TrimSpace(excerpt)
		}
		return m
	}
	if c := v.Campus; c != nil {
		if err := needEvidence("campus", c.SourceURL, c.Excerpt); err != nil {
			return nil, err
		}
		val := strings.ToLower(strings.TrimSpace(c.Value))
		aff := AffiliationUnconfirmed
		independent := SourceKindOf(c.SourceURL) == SourceKindIndependent
		switch val {
		case CampusVellore:
			if independent {
				aff = AffiliationConfirmedVellore
			}
		case CampusChennai, CampusAP, CampusBhopal, "other":
			if independent {
				aff = AffiliationOtherCampus
			}
		default:
			return nil, fmt.Errorf("%w: campus value must be vellore, chennai, ap, bhopal or other", ErrValidation)
		}
		out.Campus, out.VITAffiliation = &val, &aff
		out.Provenance["campus"] = stamp(c.SourceURL, c.Excerpt)
	}
	if g := v.Graduation; g != nil {
		if err := needEvidence("graduation", g.SourceURL, g.Excerpt); err != nil {
			return nil, err
		}
		if g.Year < 1950 || g.Year > 2100 {
			return nil, fmt.Errorf("%w: graduation year must be between 1950 and 2100", ErrValidation)
		}
		out.GraduationYear = &g.Year
		if strings.TrimSpace(g.Date) != "" {
			d, err := time.Parse("2006-01-02", strings.TrimSpace(g.Date))
			if err != nil || d.Year() != g.Year {
				return nil, fmt.Errorf("%w: graduation date must be YYYY-MM-DD within the graduation year", ErrValidation)
			}
			out.GraduationDate = &d
		}
		out.Provenance["graduation"] = stamp(g.SourceURL, g.Excerpt)
	}
	if a := v.ACM; a != nil {
		if err := needEvidence("acm", a.SourceURL, a.Excerpt); err != nil {
			return nil, err
		}
		status := strings.ToLower(strings.TrimSpace(a.Status))
		switch status {
		case "confirmed", "likely":
			src := a.SourceURL
			if SourceKindOf(src) != SourceKindIndependent {
				src = "" // self-report is not confirmation
			}
			status = ResolveACMAffiliation(status, src, a.Excerpt)
		case "not_confirmed":
		default:
			return nil, fmt.Errorf("%w: acm status must be confirmed, likely or not_confirmed", ErrValidation)
		}
		out.ACMAffiliation = &status
		out.ACMRole, out.ACMTenure = vptr(strings.TrimSpace(a.Role)), vptr(strings.TrimSpace(a.Tenure))
		out.ACMSource, out.ACMExcerpt = vptr(strings.TrimSpace(a.SourceURL)), vptr(strings.TrimSpace(a.Excerpt))
		out.Provenance["acm"] = stamp(a.SourceURL, a.Excerpt)
	}
	if r := v.ContactRoute; r != nil {
		if !validHTTPURL(r.SourceURL) {
			return nil, fmt.Errorf("%w: contact_route needs a source_url that is an http(s) URL", ErrValidation)
		}
		val := strings.TrimSpace(r.Value)
		switch {
		case validHTTPURL(val):
			out.ContactRoute = &val
		default:
			addr, err := mail.ParseAddress(val)
			if err != nil || addr.Address != val {
				return nil, fmt.Errorf("%w: contact_route value must be an http(s) URL or an email address", ErrValidation)
			}
			out.BusinessEmail = &val
		}
		out.Provenance["contact_route"] = stamp(r.SourceURL, "")
	}
	return out, nil
}
