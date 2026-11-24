package assess

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// OwnerGradYear is the year the owner graduates (2028 B.Tech CSE, VIT Vellore). It is the one fact the graduation
// rules compare a posting against.
const OwnerGradYear = 2028

// ---- location and work authorisation ----------------------------------------------------------------------------

var (
	indiaPlace = regexp.MustCompile(`(?i)\b(india|bengaluru|bangalore|mumbai|navi mumbai|delhi|new delhi|ncr|gurgaon|gurugram|noida|greater noida|hyderabad|pune|chennai|kolkata|ahmedabad|vellore|jaipur|kochi|cochin|chandigarh|indore|coimbatore|thiruvananthapuram|trivandrum|nagpur|bhubaneswar|lucknow|vadodara|surat|mysuru|mysore|visakhapatnam|mohali|ghaziabad|faridabad|patna|bhopal|nashik)\b`)
	anywhere   = regexp.MustCompile(`(?i)\b(anywhere|worldwide|global|globally)\b`)
	remoteWord = regexp.MustCompile(`(?i)\b(remote|work from home|wfh|work-from-home)\b`)
	// remoteOpenText is wording in a description that makes a remote role open to the owner's country.
	remoteOpenText = regexp.MustCompile(`(?i)\bwork(?:ing)? from anywhere\b|\banywhere in the world\b|\bworldwide\b|\bglobally remote\b|\bremote[^.]{0,30}\bindia\b|\bindia[^.]{0,30}\bremote\b`)
	relocationText = regexp.MustCompile(`(?i)\brelocation (assistance|support|package|bonus|allowance)\b|\bwe (will )?(sponsor|provide) (your |a |the )?(work )?visa\b|\bvisa sponsorship (is )?(available|provided|offered)\b`)
	noSponsorText  = regexp.MustCompile(`(?i)\b(no|not|cannot|can't|unable to|do not|don't|does not|doesn't|will not|won't)\b[^.]{0,30}\b(visa sponsorship|sponsor (a |your |work )?visas?|sponsor)\b`)
)

// country is a non-India country or bloc that, written next to "only", "authorized to work in" or "must reside in",
// closes the role to someone without that right. Abbreviations are matched case-sensitively so "us" the pronoun is
// not "US" the country.
const country = `(?:the )?(?:U\.S\.A?\.?|USA?|U\.K\.|UK|EU|(?i:united states(?: of america)?|america|canada|united kingdom|great britain|england|european union|europe|germany|france|netherlands|ireland|australia|new zealand|singapore|uae|united arab emirates|japan|switzerland|spain|sweden|brazil|mexico))`

var blockers = []struct {
	why string
	re  *regexp.Regexp
}{
	{"The posting is open only to people in a country other than India", regexp.MustCompile(`\b` + country + `[- ]?(?i:only|based only)\b`)},
	{"The posting is open only to people in a country other than India", regexp.MustCompile(`(?i:\bonly (?:available |open )?(?:to|for) (?:candidates |applicants |people |residents )?(?:in|from|located in|based in) )` + country)},
	{"The posting requires authorisation to work in a country other than India", regexp.MustCompile(`(?i:\b(?:must|need to|required to|should)\s+(?:be\s+|have\s+)?(?:legally |currently |lawfully )?(?:authori[sz]ed|eligible|permitted|entitled|able|allowed|have the right)\s+(?:to\s+)?(?:work|live and work)\s+(?:in|within)\s+)` + country)},
	{"The posting requires authorisation to work in a country other than India", regexp.MustCompile(`(?i:\b(?:work authori[sz]ation|legal right to work|right to work)\s+(?:in|for)\s+)` + country)},
	{"The posting requires authorisation to work in a country other than India", regexp.MustCompile(`(?i:\bauthori[sz]ed to work (?:legally )?in )` + country)},
	{"The posting requires authorisation to work in a country other than India", regexp.MustCompile(`(?i:\bauthori[sz]ation to work (?:legally )?(?:in|within)\s+)` + country + `(?i:[^.]{0,40}\b(?:required|needed|necessary|mandatory)\b)`)},
	{"The posting requires citizenship of a country other than India", regexp.MustCompile(`\b(?:U\.S\.|US|USA)\s+(?i:citizens?(?:hip)?|persons?)\b|(?i:\bcitizens? of )` + country)},
	{"The posting requires a security clearance", regexp.MustCompile(`(?i)\b(?:must (?:have|hold|obtain)|requires?|required)\b[^.]{0,40}\b(?:security|secret|top secret|ts/sci) clearance\b|\bactive (?:secret|top secret|ts/sci)\b`)},
	{"The posting requires residence in a country other than India", regexp.MustCompile(`(?i:\bmust (?:be )?(?:located|based|reside|residing|living|resident)\s+(?:in|within|of)\s+)` + country)},
}

// LocationResult is the place and work-authorisation reading of a posting.
type LocationResult struct {
	Fit      string
	Quote    string
	Blockers []domain.Reason
}

// AssessLocation reads where a role is and whether the owner may work there. India and worldwide-remote are fine; a
// posting that is open only to another country ("US only", "must be authorized to work in the US") is a blocker,
// quoted. Where the text does not settle it the fit is unknown.
func AssessLocation(location, description string) LocationResult {
	loc, text := norm(location), norm(description)
	var res LocationResult
	seen := map[string]bool{}
	for _, b := range blockers {
		if seen[b.why] { // several wordings of one restriction are one finding
			continue
		}
		for _, src := range []string{text, loc} {
			if q, ok := snip(b.re, src, 20); ok {
				res.Blockers = append(res.Blockers, domain.Reason{Reason: b.why, Quote: q})
				seen[b.why] = true
				break
			}
		}
	}

	switch {
	case loc == "":
		res.Fit = domain.LocationUnknown
	case indiaPlace.MatchString(loc):
		res.Fit, res.Quote = domain.LocationIndia, loc
	case remoteWord.MatchString(loc) && anywhere.MatchString(loc):
		res.Fit, res.Quote = domain.LocationRemoteOK, loc
	case remoteWord.MatchString(loc):
		// A remote role names no country on its own: it is open to the owner only if the text says so, and closed to
		// the owner when the label names another country.
		switch {
		case namesOtherCountry(loc):
			res.Fit, res.Quote = domain.LocationOutside, loc
		default:
			if q, ok := snip(remoteOpenText, text, 20); ok {
				res.Fit, res.Quote = domain.LocationRemoteOK, q
			} else {
				res.Fit = domain.LocationUnknown
			}
		}
	case namesNoPlace(loc):
		// "Hybrid", "Multiple locations", "APAC": a label that names no country says nothing about whether the owner
		// can work there, so it needs evidence from elsewhere.
		res.Fit = domain.LocationUnknown
	default:
		if q, ok := snip(relocationText, text, 20); ok && !noSponsorText.MatchString(text) {
			res.Fit, res.Quote = domain.LocationRelocation, q
		} else {
			res.Fit, res.Quote = domain.LocationOutside, loc
		}
	}

	// Onsite abroad with the posting saying it will not sponsor: the owner cannot take it, and the words say so.
	if res.Fit == domain.LocationOutside && len(res.Blockers) == 0 {
		if q, ok := snip(noSponsorText, text, 20); ok {
			res.Blockers = append(res.Blockers, domain.Reason{Reason: "The role is outside India and the posting says it does not sponsor", Quote: q})
		}
	}
	return res
}

// placeless is wording that stands in for a location without naming one: a work mode, a count of offices, or a region
// too wide to say whether the owner may work there (APAC, Asia).
var placeless = regexp.MustCompile(`(?i)\b(?:hybrid|on-?site|in[- ]office|office|offices|multiple|various|several|many|flexible|locations?|tbd|tba|n/a|apac|asia|asia[- ]pacific|and|or)\b`)

// namesNoPlace reports whether loc is only placeless wording, so it names no country at all.
func namesNoPlace(loc string) bool {
	return strings.Trim(placeless.ReplaceAllString(loc, ""), " ,;/()-–|·") == ""
}

var otherCountry = regexp.MustCompile(`\b(?:U\.S\.A?\.?|USA?|U\.K\.|UK|EU)\b|(?i:\b(?:united states|america|canada|united kingdom|england|europe|european union|germany|france|netherlands|ireland|australia|new zealand|singapore|uae|japan|switzerland|spain|sweden|brazil|mexico|north america|emea|latam)\b)`)

func namesOtherCountry(loc string) bool { return otherCountry.MatchString(loc) }

// ---- graduation year and degree -----------------------------------------------------------------------------------

const (
	// yearList is one year or several joined by a separator; a season or month before the later year is part of the
	// list ("Fall 2027 or Spring 2028", "December 2027 or spring/summer 2028"), not the end of it.
	yearList = `20[2-3]\d(?:\s*(?:,|/|-|–|to|and|&|or)\s*(?:and\s+)?(?:(?:spring|summer|fall|autumn|winter|early|late|mid|january|february|march|april|may|june|july|august|september|october|november|december)[\s/,-]*)*20[2-3]\d)*`
	gradWord = `(?:graduat\w*|batch(?:es)?|pass(?:ing)?[- ]?outs?|class of|cohorts?|expected (?:to )?(?:graduate|complete)|degree completion|year of (?:graduation|passing))`
)

var (
	yearsAfterWord  = regexp.MustCompile(`(?i)\b` + gradWord + `\b[^.]{0,60}?\b(` + yearList + `)\b(\s*(?:or|and)\s+(?:later|onwards?|above|beyond|after|earlier|before))?`)
	yearsBeforeWord = regexp.MustCompile(`(?i)\b(` + yearList + `)\s*(?:\(\w+\)\s*)?(?:graduat\w*|batch(?:es)?|pass(?:ing)?[- ]?outs?|grads?|alumni|cohort)\b`)
	yearToken       = regexp.MustCompile(`20[2-3]\d`)
	anyYear         = regexp.MustCompile(`(?i)\b(?:any|all|every)\s+(?:graduation |passing[- ]out |pass[- ]out )?(?:year|batch)s?\b|\birrespective of (?:your )?(?:graduation|passing|batch)|\bregardless of (?:your )?(?:graduation|batch)`)
	prefinalYear    = regexp.MustCompile(`(?i)\bpre-?final[- ]year\b|\bpenultimate[- ]year\b|\b(?:third|3rd)[- ]year\b|\brising junior\b`)
	finalYear       = regexp.MustCompile(`(?i)\bfinal[- ]year\b|\brising senior\b|\bgraduating this year\b`)

	degreeOnly = regexp.MustCompile(`(?i)\b(?:pursuing|enrolled in|currently (?:in|pursuing)|students? (?:of|in)|candidates? (?:with|holding))\s+(?:an?\s+)?(?:master'?s|masters|m\.?tech|m\.?s|mba|ph\.?d|m\.?sc|postgraduate|post-graduate)\b|\b(?:ph\.?d|doctoral|master'?s|mba)\s+(?:students?|candidates?)\s+only\b`)
	// bachelorHit is a bachelor's-level degree named in the text. "BE" and "BS" are matched in capitals only (a lower-case
	// "be" is the verb, which made every posting look like it welcomed bachelors).
	bachelorHit = regexp.MustCompile(`(?i:\b(?:bachelor\w*|b\.?tech|b\.?sc|undergrad\w*|bca)\b)|\bB\.E\b\.?|\bB\.S\b\.?|\b(?:BE|BS)\b`)
)

// GradResult is the graduation-year and degree reading of a posting.
type GradResult struct {
	Fit      string
	Reason   *domain.Reason // why it fits (quoted), when it does
	Blockers []domain.Reason
	// Soft is a restriction the text hints at without settling (final-year students), for the reviewer.
	Soft *domain.Reason
}

// expandYears reads a list such as "2026/2027", "2026 to 2028" or "2027" into years, honouring "between ... and",
// "or later" and "or earlier".
func expandYears(list, tail string, between bool) []int {
	var years []int
	for _, y := range yearToken.FindAllString(list, -1) {
		n, _ := strconv.Atoi(y)
		years = append(years, n)
	}
	if parts := yearToken.Split(list, -1); len(years) == 2 && years[0] < years[1] && len(parts) == 3 {
		sep := strings.ToLower(parts[1])
		if between || strings.Contains(sep, "to") || strings.ContainsAny(sep, "-–") {
			years = years[:0]
			first, _ := strconv.Atoi(yearToken.FindAllString(list, -1)[0])
			last, _ := strconv.Atoi(yearToken.FindAllString(list, -1)[1])
			for y := first; y <= last; y++ {
				years = append(years, y)
			}
		}
	}
	tail = strings.ToLower(tail)
	if len(years) == 0 {
		return years
	}
	switch {
	case strings.Contains(tail, "later") || strings.Contains(tail, "onward") || strings.Contains(tail, "above") || strings.Contains(tail, "beyond") || strings.Contains(tail, "after"):
		last := years[len(years)-1]
		for y := last + 1; y <= last+6; y++ {
			years = append(years, y)
		}
	case strings.Contains(tail, "earlier") || strings.Contains(tail, "before"):
		first := years[0]
		for y := first - 6; y < first; y++ {
			years = append(years, y)
		}
	}
	return years
}

// AssessGrad reads which graduation years and degrees a posting is open to. A posting naming graduation years that
// do not include the owner's (2028) is a blocker, quoted; one that includes it fits; one that names none is unknown.
func AssessGrad(description string) GradResult {
	text := norm(description)
	res := GradResult{Fit: domain.GradUnknown}
	type found struct {
		years []int
		quote string
	}
	var sets []found
	for _, m := range yearsAfterWord.FindAllStringSubmatchIndex(text, -1) {
		list := text[m[2]:m[3]]
		tail := ""
		if m[4] >= 0 {
			tail = text[m[4]:m[5]]
		}
		between := strings.Contains(strings.ToLower(text[m[0]:m[2]]), "between")
		sets = append(sets, found{expandYears(list, tail, between), around(text, m[0], m[1], 10)})
	}
	for _, m := range yearsBeforeWord.FindAllStringSubmatchIndex(text, -1) {
		sets = append(sets, found{expandYears(text[m[2]:m[3]], "", false), around(text, m[0], m[1], 10)})
	}
	var firstMismatch *found
	for i := range sets {
		s := sets[i]
		if len(s.years) == 0 {
			continue
		}
		hit := false
		for _, y := range s.years {
			if y == OwnerGradYear {
				hit = true
			}
		}
		if hit {
			res.Fit = domain.GradFit
			res.Reason = &domain.Reason{Reason: fmt.Sprintf("The posting is open to the %d graduating year", OwnerGradYear), Quote: s.quote}
			break
		}
		if firstMismatch == nil {
			firstMismatch = &s
		}
	}
	if res.Fit != domain.GradFit {
		if q, ok := snip(prefinalYear, text, 20); ok {
			res.Fit = domain.GradFit
			res.Reason = &domain.Reason{Reason: "The posting is open to pre-final (third) year students, which the owner is", Quote: q}
		} else if q, ok := snip(anyYear, text, 20); ok && firstMismatch == nil {
			res.Fit = domain.GradFit
			res.Reason = &domain.Reason{Reason: "The posting is open to any graduation year", Quote: q}
		} else if firstMismatch != nil {
			res.Fit = domain.GradMismatch
			res.Blockers = append(res.Blockers, domain.Reason{Reason: fmt.Sprintf("The posting is for other graduating years; the owner graduates in %d", OwnerGradYear), Quote: firstMismatch.quote})
		}
	}
	if res.Fit != domain.GradFit {
		if q, ok := snip(finalYear, text, 20); ok {
			res.Soft = &domain.Reason{Reason: "The posting mentions final-year students, which may not include the owner", Quote: q}
		}
	}
	if q, ok := snip(degreeOnly, text, 20); ok && !bachelorHit.MatchString(text) {
		res.Blockers = append(res.Blockers, domain.Reason{Reason: "The posting is for postgraduate students; the owner is an undergraduate", Quote: q})
		if res.Fit == domain.GradUnknown {
			res.Fit = domain.GradMismatch
		}
	}
	return res
}
