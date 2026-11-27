package domain

import (
	"regexp"
	"strconv"
	"strings"
)

// Quotes shorter than this prove too little to check; longer than MaxEvidenceQuote are not a quote.
const (
	MinEvidenceQuote = 20
	MaxEvidenceQuote = 300
)

// EvidenceCheckedBy is the verified_by of a claim Griffin recorded after finding the quote on the page itself.
const EvidenceCheckedBy = "griffin:quote-check"

var (
	acmInQuote = regexp.MustCompile(`\bACM\b`)
	spaceRun   = regexp.MustCompile(`\s+`)
	quoteFold  = strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'", "–", "-", "—", "-", " ", " ")
)

// normaliseForQuote folds case, curly quotes, dashes and runs of white space, so a quote copied from a page matches
// the page's text however the page spaced it.
func normaliseForQuote(s string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(strings.ToLower(quoteFold.Replace(s)), " "))
}

// QuoteOnPage reports whether quote appears, word for word, in the page's text.
func QuoteOnPage(quote, pageText string) bool {
	q := normaliseForQuote(quote)
	if len([]rune(q)) < MinEvidenceQuote || len([]rune(q)) > MaxEvidenceQuote {
		return false
	}
	return strings.Contains(normaliseForQuote(pageText), q)
}

// EvidenceVerification turns a row's evidence into the claims it proves, once the page's text is known. It proves
// nothing (ok is false) unless the evidence page is a public http(s) page that is not LinkedIn and the quote is on
// it word for word. Then the quote proves only what it says: the Vellore campus when it names it, the row's batch
// when it contains that year next to a VIT Vellore mention, and ACM VIT when it names ACM next to VIT.
func EvidenceVerification(row AlumniImportRow, pageText string) (*PersonVerification, bool) {
	src := strings.TrimSpace(row.EvidenceURL)
	if !validHTTPURL(src) || SourceKindOf(src) != SourceKindIndependent || !QuoteOnPage(row.Quote, pageText) {
		return nil, false
	}
	quote := strings.TrimSpace(row.Quote)
	v := &PersonVerification{VerifiedBy: EvidenceCheckedBy}
	campus, _ := ClassifyVITCampus(quote)
	if campus != CampusUnknown {
		v.Campus = &CampusClaim{Value: campus, SourceURL: src, Excerpt: quote}
	}
	if campus == CampusVellore && row.GraduationYear > 0 && regexp.MustCompile(`\b`+strconv.Itoa(row.GraduationYear)+`\b`).MatchString(quote) {
		v.Graduation = &GraduationClaim{Year: row.GraduationYear, SourceURL: src, Excerpt: quote}
	}
	if acmInQuote.MatchString(quote) && (vitName.MatchString(strings.ToLower(quote)) || vitFullName.MatchString(strings.ToLower(quote))) {
		v.ACM = &ACMClaim{Status: "confirmed", SourceURL: src, Excerpt: quote}
	}
	if v.Campus == nil && v.Graduation == nil && v.ACM == nil {
		return nil, false
	}
	return v, true
}
