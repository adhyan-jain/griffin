package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ProfileFact is one verified, citable claim about the owner (a resume bullet, a project detail).
// Generated text may select and order facts but must not invent claims, so every bullet traces to
// a fact ID and a source document.
type ProfileFact struct {
	ID      string   `json:"id"`
	Project string   `json:"project"` // project or role the fact belongs to
	Text    string   `json:"text"`    // verbatim, owner-verified wording
	Source  string   `json:"source"`  // where it was verified, e.g. "resume:AdhyanJain_Resume.pdf"
	Tags    []string `json:"tags,omitempty"`
}

// OwnerProfile is the canonical, versioned owner profile. It is loaded from the database; no
// personal facts live in source code. Work authorisation, salary and similar answers are
// deliberately absent: they are never inferred and always go to the owner for review.
type OwnerProfile struct {
	Version    int           `json:"version"`
	FullName   string        `json:"full_name"`
	Email      string        `json:"email"`
	Phone      string        `json:"phone"`
	GitHub     string        `json:"github"`
	LinkedIn   string        `json:"linkedin"`
	Portfolio  string        `json:"portfolio"`
	School     string        `json:"school"`
	Degree     string        `json:"degree"`
	GradYear   string        `json:"grad_year"`
	CGPA       string        `json:"cgpa"`
	Country    string        `json:"country"`
	ResumeFile string        `json:"resume_file"` // file name of the canonical resume artifact
	Facts      []ProfileFact `json:"facts"`
}

// FirstName and LastName are derived from FullName, never stored separately.
func (p OwnerProfile) FirstName() string {
	parts := strings.Fields(p.FullName)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (p OwnerProfile) LastName() string {
	parts := strings.Fields(p.FullName)
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[1:], " ")
}

var reWord = regexp.MustCompile(`[a-z0-9+#.]{2,}`)

// RelevantFacts returns up to max verified facts ranked by overlap between the query (role title,
// description) and each fact's tags, project and text. Facts are returned verbatim and unchanged.
// Facts with no overlap are omitted, so an unsupported role yields no claims rather than filler.
func (p OwnerProfile) RelevantFacts(query string, max int) []ProfileFact {
	q := map[string]bool{}
	for _, w := range reWord.FindAllString(strings.ToLower(query), -1) {
		q[w] = true
	}
	type scored struct {
		f     ProfileFact
		score int
		idx   int
	}
	var out []scored
	for i, f := range p.Facts {
		s := 0
		for _, t := range f.Tags {
			if q[strings.ToLower(t)] {
				s += 3
			}
		}
		for _, w := range reWord.FindAllString(strings.ToLower(f.Project+" "+f.Text), -1) {
			if q[w] {
				s++
			}
		}
		if s > 0 {
			out = append(out, scored{f, s, i})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].idx < out[j].idx
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	facts := make([]ProfileFact, len(out))
	for i, s := range out {
		facts[i] = s.f
	}
	return facts
}

// ContextBlock renders the profile for an LLM prompt: only stored fields and verbatim facts.
func (p OwnerProfile) ContextBlock() string {
	var b strings.Builder
	b.WriteString("Candidate Background (verified owner profile; use only these facts, add no other claims):\n")
	line := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			b.WriteString("- " + label + ": " + v + "\n")
		}
	}
	line("Full Name", p.FullName)
	line("Education", strings.TrimSpace(p.Degree+", "+p.School))
	line("Expected graduation", p.GradYear)
	line("CGPA", p.CGPA)
	for _, f := range p.Facts {
		b.WriteString("- [" + f.ID + "] " + f.Project + ": " + f.Text + "\n")
	}
	return b.String()
}

// Validate rejects profiles that could not safely ground generated text: identity fields missing,
// or facts without an ID, verbatim text and a verification source.
func (p OwnerProfile) Validate() error {
	for name, v := range map[string]string{"full_name": p.FullName, "email": p.Email, "school": p.School, "degree": p.Degree, "grad_year": p.GradYear} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("owner profile: %s is required", name)
		}
	}
	seen := map[string]bool{}
	for i, f := range p.Facts {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Text) == "" || strings.TrimSpace(f.Source) == "" {
			return fmt.Errorf("owner profile: fact %d needs id, text and source", i)
		}
		if seen[f.ID] {
			return fmt.Errorf("owner profile: duplicate fact id %q", f.ID)
		}
		seen[f.ID] = true
	}
	return nil
}
