package ats

import (
	"regexp"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// Stage 1 of matching: from everything an employer board lists, keep what could be an internship or an early-career
// role, and nothing else. It is deliberately broad on technology (frontend, research, devtools and data stay) and
// narrow only in two ways: the posting must say it is for students or new graduates, and its department must not be
// plainly non-engineering (sales, legal, finance, HR, marketing). Whether the role suits the owner is decided later.
// Plain rules, no model: every verdict names the words that made it.

var (
	// titleSignals are early-career words in a title or an employment type. Word boundaries keep "Internal Tools
	// Engineer", "International Payments" and "Internet Systems" out: "intern" must stand alone or be intern(s|ship(s)).
	titleSignals = regexp.MustCompile(`(?i)\b(?:interns?|internships?|co-?ops?|apprentices?|apprenticeships?|trainees?|new[- ]?grads?|new graduates?|recent graduates?|graduates?|university (?:grad|graduate|hire|hires|program|programme|intern)s?|early[- ]career|entry[- ]level|campus|freshers?|werkstudent|working student|student (?:[a-z]+ )?(?:intern|engineer|developer|researcher|program|programme|position|role|trainee|assistant|worker|associate|fellow)s?|summer 20\d\d|(?:class of|graduating in|graduates? of) 20\d\d|20\d\d (?:grads?|graduates?|interns?|internships?|new grads?|summer))\b`)

	// descriptionSignals are the phrases a description uses when the role is for students or new graduates. A bare
	// "university" or "intern" is not enough: "degree from an accredited university" and "you will mentor interns" are
	// in full-time senior postings.
	descriptionSignals = regexp.MustCompile(`(?i)\b(?:(?:this|our|the) (?:summer |fall |winter |spring )?intern(?:ship)?|internship (?:program|programme|role|position|opportunity)s?|(?:summer|fall|winter|spring) 20\d\d (?:intern|internship|co-?op)|class of 20\d\d|20\d\d (?:grads?|graduates?|new grads?)|graduat(?:e|ing) in 20\d\d|new[- ]grads?|recent graduates?|currently (?:enrolled|pursuing)|enrolled in (?:a|an) (?:bachelor|undergraduate|master|university|degree|phd|college)|pursuing (?:a|an) (?:bachelor|undergraduate|master|degree|phd)|co-?op (?:program|programme|term|role|position)|university (?:grad|graduate|hire)s?)\b`)

	// hardExcluded departments and titles are never engineering, whatever else the title says ("Technical Recruiter").
	hardExcluded = regexp.MustCompile(`(?i)\b(?:recruit(?:er|ers|ing|ment)|talent acquisition|sourcer|legal|counsel|paralegal|attorney|payroll|accountant|human resources|hr|executive assistant|office manager|procurement)\b`)

	// softExcluded are non-engineering functions: excluded unless the title itself says engineering work.
	softExcluded = regexp.MustCompile(`(?i)\b(?:sales|account executive|account manager|business development|sdr|bdr|finance|financial|accounting|tax|audit|treasury|people (?:partner|operations|ops|team|and culture)|people|g&a|general (?:and|&) administrative|marketing|brand|public relations|communications|customer success|customer support|support specialist|content writer)\b`)

	// engineeringTitle is engineering work named in the title; it keeps a role whose department field reads like a
	// non-engineering one ("Sales Engineer Intern", "Data Engineer Intern, Marketing").
	engineeringTitle = regexp.MustCompile(`(?i)(?:engineer|developer|software|\bswe\b|\bsde\b|programmer|\bdata\b|machine learning|\bml\b|\bai\b|research|scientist|devops|\bsre\b|infrastructure|platform|security|front-?end|back-?end|full[- ]?stack|mobile|android|\bios\b|embedded|firmware|devtools|developer tools|compiler|systems|cloud|analytics|technolog|technical)`)
)

// EarlyCareerReason says why a posting could be an internship or early-career role: the words in its title or
// employment type, or the phrase in its description. "" means it is not one, or its department is not engineering.
func EarlyCareerReason(j jobs.RawJob) string {
	title := strings.TrimSpace(j.Title)
	if title == "" {
		return ""
	}
	if hardExcluded.MatchString(title) || hardExcluded.MatchString(j.Department) {
		return ""
	}
	if !engineeringTitle.MatchString(title) && (softExcluded.MatchString(title) || softExcluded.MatchString(j.Department)) {
		return ""
	}
	if m := titleSignals.FindString(title); m != "" {
		return "title: " + m
	}
	if m := titleSignals.FindString(j.EmploymentType); m != "" {
		return "employment type: " + m
	}
	if m := descriptionSignals.FindString(j.Description); m != "" {
		return "description: " + m
	}
	return ""
}

// IsEarlyCareer reports whether a posting could be an internship or early-career role.
func IsEarlyCareer(j jobs.RawJob) bool { return EarlyCareerReason(j) != "" }

// FilterEarlyCareer returns the postings that could be internships or early-career roles, in their order.
func FilterEarlyCareer(list []jobs.RawJob) []jobs.RawJob {
	var out []jobs.RawJob
	for _, j := range list {
		if IsEarlyCareer(j) {
			out = append(out, j)
		}
	}
	return out
}
