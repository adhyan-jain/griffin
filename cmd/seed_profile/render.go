package main

import (
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// profileText is what application tasks are shown about the owner: identity and contact (never the phone number),
// education, and the verified facts verbatim.
func profileText(p *domain.OwnerProfile) string {
	var b strings.Builder
	b.WriteString("# Owner profile\n\n")
	line := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "- %s: %s\n", label, v)
		}
	}
	line("Full name", p.FullName)
	line("Email", p.Email)
	line("GitHub", p.GitHub)
	line("LinkedIn", p.LinkedIn)
	line("Education", strings.TrimSpace(p.Degree+", "+p.School))
	line("Expected graduation", p.GradYear)
	line("CGPA", p.CGPA)
	b.WriteString("\n## Verified facts\n\n")
	for _, f := range p.Facts {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", f.ID, f.Project, f.Text)
	}
	return b.String()
}
