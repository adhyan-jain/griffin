// Package prep prepares one application package from a posting and the owner's profile. Code decides what it can: the
// resume, the matching projects, the questions the posting asks, the documents it requires and what is missing. A
// model (Gemini Flash, at most two calls) is asked only to write the cover letter and the "why join" paragraph, and
// whatever it writes is kept only if every number and name in it is in the owner's profile or the posting. Nothing
// here invents a qualification: a gap is listed for the owner, never filled in.
package prep

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// Profile is the owner's profile read from its text: the identity lines and the verified facts. Both forms Griffin
// stores are read (the Library document and the structured profile's context block), and a fact present in both
// counts once.
type Profile struct {
	Name      string
	Email     string
	GitHub    string
	LinkedIn  string
	Portfolio string
	Education string
	GradYear  string
	CGPA      string
	Facts     []domain.ProfileFact
	// Lines are the profile's other "- label: value" lines, kept whole: an answer such as availability is taken
	// from one of them or from nowhere.
	Lines []string
	// Raw is the profile text as read.
	Raw string
}

var (
	labelLine = regexp.MustCompile(`^\s*[-*]\s*([A-Za-z][A-Za-z /]{1,40}?)\s*:\s*(.+?)\s*$`)
	factLine  = regexp.MustCompile(`^\s*[-*]\s*\[([^\]]+)\]\s*([^:]{1,80}?)\s*:\s*(.+?)\s*$`)
)

// ParseProfile reads the owner's profile text. A phone number is never read: nothing here may carry one.
func ParseProfile(text string) Profile {
	p := Profile{Raw: strings.TrimSpace(text)}
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if m := factLine.FindStringSubmatch(line); m != nil {
			id := strings.TrimSpace(m[1])
			if seen[id] {
				continue
			}
			seen[id] = true
			p.Facts = append(p.Facts, domain.ProfileFact{ID: id, Project: strings.TrimSpace(m[2]), Text: strings.TrimSpace(m[3])})
			continue
		}
		m := labelLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label, value := strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
		switch {
		case label == "full name" || label == "name":
			p.Name = value
		case label == "email":
			p.Email = value
		case label == "github":
			p.GitHub = value
		case label == "linkedin":
			p.LinkedIn = value
		case label == "portfolio" || label == "website":
			p.Portfolio = value
		case label == "education":
			p.Education = value
		case strings.Contains(label, "graduation"):
			p.GradYear = value
		case label == "cgpa":
			p.CGPA = value
		case label == "phone" || label == "mobile":
			// never read
		default:
			p.Lines = append(p.Lines, strings.TrimSpace(m[1])+": "+value)
		}
	}
	return p
}

// Hash identifies the profile's content, so a package made from an older profile can be told from a current one.
func (p Profile) Hash() string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(p.Raw), " ")))
	return hex.EncodeToString(sum[:])[:32]
}

// Empty reports whether the profile holds nothing a package could be grounded in.
func (p Profile) Empty() bool { return len(p.Facts) == 0 && p.Name == "" && p.Education == "" }

// line returns the first "label: value" line of the profile (an identity line or another) whose label matches re.
func (p Profile) line(re *regexp.Regexp) (string, bool) {
	for _, l := range p.Lines {
		if i := strings.Index(l, ":"); i > 0 && re.MatchString(l[:i]) {
			return strings.TrimSpace(l[i+1:]), true
		}
	}
	return "", false
}
