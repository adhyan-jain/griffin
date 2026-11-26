package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/prep"
)

// OwnerProfileWithLibrary reads the structured owner profile and, when there is none (or it has no name), the
// profile document in the Library, so the owner never enters the same facts twice.
type OwnerProfileWithLibrary struct {
	Structured OwnerProfileSource
	Library    func(ctx context.Context, userID uuid.UUID) string
}

// Latest implements OwnerProfileSource. It returns (nil, nil) when neither holds a profile.
func (s OwnerProfileWithLibrary) Latest(ctx context.Context, userID uuid.UUID) (*domain.OwnerProfile, error) {
	if s.Structured != nil {
		p, err := s.Structured.Latest(ctx, userID)
		if err != nil {
			return nil, err
		}
		if p != nil && strings.TrimSpace(p.FullName) != "" {
			return p, nil
		}
	}
	if s.Library == nil {
		return nil, nil
	}
	return OwnerProfileFromText(s.Library(ctx, userID)), nil
}

var (
	schoolWord = regexp.MustCompile(`(?i)\b(institute|university|college|school|vit|iit|nit|bits)\b`)
	acmMention = regexp.MustCompile(`\bACM\b`)
	bulletFact = regexp.MustCompile(`^\s*[-*]\s*\[([^\]]+)\]\s*(.+?)\s*$`)
)

// OwnerProfileFromText turns the Library profile document into an OwnerProfile, using only what it states: the
// name, the school named in the Education line, and its bracketed facts. A fact with a project name is tagged
// "project" (the drafter names the first one); ACM facts are kept but never named as the project. It returns nil
// when the text holds no profile.
func OwnerProfileFromText(text string) *domain.OwnerProfile {
	pp := prep.ParseProfile(text)
	if pp.Empty() {
		return nil
	}
	op := &domain.OwnerProfile{FullName: pp.Name, School: schoolOf(pp.Education), GradYear: pp.GradYear}
	seen := map[string]bool{}
	for _, f := range pp.Facts {
		seen[f.ID] = true
		if f.Project != "" && !acmMention.MatchString(f.Project+" "+f.Text) {
			f.Tags = []string{"project"}
		}
		f.Source = "library:profile"
		op.Facts = append(op.Facts, f)
	}
	// "- [acm] ACM - VIT Student Chapter (Senior Core Member)" has no "project: text" shape; keep it as a fact so
	// the drafter knows the owner is in ACM.
	for _, line := range strings.Split(text, "\n") {
		if m := bulletFact.FindStringSubmatch(line); m != nil && !seen[m[1]] && acmMention.MatchString(m[2]) {
			seen[m[1]] = true
			op.Facts = append(op.Facts, domain.ProfileFact{ID: m[1], Text: m[2], Source: "library:profile"})
		}
	}
	return op
}

// schoolOf picks the part of an Education line that names the school ("B.Tech CSE, Vellore Institute of
// Technology (VIT)" -> "Vellore Institute of Technology (VIT)"), or the whole line when no part does.
func schoolOf(education string) string {
	parts := strings.Split(education, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(parts[i]); schoolWord.MatchString(p) {
			return p
		}
	}
	return strings.TrimSpace(education)
}
