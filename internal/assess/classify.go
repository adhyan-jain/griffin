package assess

import (
	"regexp"

	"github.com/griffin/core/internal/domain"
)

// snip returns the words around the first match of re in text (about pad bytes each side), or "" and false.
func snip(re *regexp.Regexp, text string, pad int) (string, bool) {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	return around(text, loc[0], loc[1], pad), true
}

var (
	titleIntern   = regexp.MustCompile(`(?i)\b(intern|interns|internship|internships|trainee|apprentice|apprenticeship|co-?op|working student|werkstudent)\b`)
	titleFullTime = regexp.MustCompile(`(?i)\b(senior|sr\.?|staff|principal|lead|manager|director|head of|architect|vp|new[- ]grad|graduate (engineer|developer|program)|full[- ]time)\b`)
	descFullTime  = regexp.MustCompile(`(?i)\bfull[- ]time (position|role|employment|job|opportunity|employee)\b|\bemployment type:? full[- ]time|\bpermanent (position|role|employee)\b|\b\d+\+? years? of (professional |relevant |industry |work |hands-on )?experience\b`)
	descIntern    = regexp.MustCompile(`(?i)\b(this is an?|the) (paid |unpaid |summer |winter |remote )?internship\b|\binternship (program|programme|opportunity|duration|period)\b|\b\d+[- ]months? internship\b|\bsummer internship\b`)
)

// ClassifyEmployment says whether a posting is an internship or a full-time job, from its title and then its text.
// It is unknown when neither says. The second result is the words that decided it.
func ClassifyEmployment(title, description string) (string, string) {
	if m, ok := firstMatch(titleIntern, title); ok {
		return domain.EmploymentInternship, m
	}
	if m, ok := firstMatch(titleFullTime, title); ok {
		return domain.EmploymentFullTime, m
	}
	text := norm(description)
	if q, ok := snip(descFullTime, text, 30); ok {
		return domain.EmploymentFullTime, q
	}
	if q, ok := snip(descIntern, text, 30); ok {
		return domain.EmploymentInternship, q
	}
	return domain.EmploymentUnknown, ""
}

type roleRule struct {
	class string
	re    *regexp.Regexp
}

// Title rules, most specific first. A frontend, research or devtools role is a role, not a reason to drop a listing.
var roleRules = []roleRule{
	{domain.RoleSecurity, regexp.MustCompile(`(?i)\b(security|cyber\w*|infosec|appsec|pen-?test\w*|vulnerabilit\w*|cryptograph\w*)\b`)},
	{domain.RoleData, regexp.MustCompile(`(?i)\bdata (scientist|engineer\w*|analy\w+|science|pipelines?|platform)\b|\banalytics\b|\bbusiness intelligence\b|\bbi (developer|engineer|analyst)\b|\betl\b`)},
	{domain.RoleResearch, regexp.MustCompile(`(?i)\b(research\w*|r&d|applied scientist|scientist|fellow)\b`)},
	{domain.RoleAIML, regexp.MustCompile(`(?i)\b(ai|a\.i\.|ml|machine learning|deep learning|llms?|nlp|gen ?ai|generative|computer vision|reinforcement learning|neural|mlops|ai/ml|rag|prompt engineer\w*)\b`)},
	{domain.RoleInfra, regexp.MustCompile(`(?i)\b(devops|dev ops|sre|site reliability|infrastructure|platform engineer\w*|cloud|kubernetes|systems? (engineer\w*|administrat\w+)|network engineer\w*|reliability|observability|release engineer\w*|build engineer\w*)\b`)},
	{domain.RoleDevtools, regexp.MustCompile(`(?i)\b(developer (tools?|experience|productivity|platform|infrastructure)|devtools?|dev tools?|tooling|compilers?|sdks?|build systems?)\b`)},
	{domain.RoleMobile, regexp.MustCompile(`(?i)\b(android|ios|flutter|react native|mobile|swift|kotlin)\b`)},
	{domain.RoleFullstack, regexp.MustCompile(`(?i)\b(full[- ]?stack|mern|mean stack)\b`)},
	{domain.RoleFrontend, regexp.MustCompile(`(?i)\b(front[- ]?end|ui (developer|engineer)|react(\.js)?|angular|vue(\.js)?|javascript|css|html|next\.?js|web designer)\b`)},
	{domain.RoleBackend, regexp.MustCompile(`(?i)\b(back[- ]?end|server[- ]?side|apis?|distributed|golang|java|node(\.js)?|django|spring|databases?|microservices?)\b`)},
}

var (
	// A title that is plainly not an engineering job. "data entry" must be read before the data rule.
	nonTechTitle = regexp.MustCompile(`(?i)\b(data entry|marketing|sales|business development|bd|hr|human resources|recruit\w*|talent acquisition|content (writ\w*|creat\w*)|copywrit\w*|social media|graphic design\w*|video edit\w*|finance|accounting|legal|operations|customer (support|success)|telecall\w*|seo|public relations|event\w*|influencer|community manager|digital marketing|management trainee|hr executive|business analyst)\b`)
	// "software", "developer" and friends say it is an engineering job even when the kind is not named.
	genericStrong = regexp.MustCompile(`(?i)\b(software|developer|sde|swe|programm\w+|coding|web)\b`)
	genericWeak   = regexp.MustCompile(`(?i)\b(engineer\w*|technolog\w*|tech|technical)\b`)
)

// What a description says about the kind of work when the title does not. A class needs two hits to win.
var descRoleRules = []roleRule{
	{domain.RoleAIML, regexp.MustCompile(`(?i)\b(machine learning|deep learning|llms?|rag|pytorch|tensorflow|transformers?|nlp|fine-?tun\w+|embeddings?|generative ai|inference|ai agents?|model training)\b`)},
	{domain.RoleBackend, regexp.MustCompile(`(?i)\b(back-?end|microservices?|rest(ful)? apis?|grpc|postgres(ql)?|mysql|mongodb|redis|kafka|distributed systems?|golang|node\.?js|django|flask|spring boot|sql)\b`)},
	{domain.RoleFrontend, regexp.MustCompile(`(?i)\b(react|next\.?js|vue|angular|css|html|tailwind|front-?end|ui components?)\b`)},
	{domain.RoleInfra, regexp.MustCompile(`(?i)\b(kubernetes|terraform|ci/cd|devops|infrastructure|observability|site reliability)\b`)},
}

func countMatches(re *regexp.Regexp, text string) int { return len(re.FindAllStringIndex(text, -1)) }

// ClassifyRole classifies a posting's technical role from its title and, when the title is generic, its description.
// The second result is the words that decided it. A role is unknown only when neither the title nor the text says.
func ClassifyRole(title, description string) (string, string) {
	title = norm(title)
	if m, ok := firstMatch(nonTechTitle, title); ok && !genericStrong.MatchString(title) {
		// "Sales Engineer" is not an engineering role either; "Marketing Technology" is not a technical one.
		return domain.RoleNonTech, m
	}
	for _, r := range roleRules {
		if m, ok := firstMatch(r.re, title); ok {
			return r.class, m
		}
	}
	generic := ""
	if m, ok := firstMatch(genericStrong, title); ok {
		generic = m
	} else if m, ok := firstMatch(genericWeak, title); ok {
		generic = m
	}
	if m, ok := firstMatch(nonTechTitle, title); ok && generic == "" {
		return domain.RoleNonTech, m
	}
	text := norm(description)
	best, bestN, bestQuote := "", 1, ""
	hits := map[string]int{}
	for _, r := range descRoleRules {
		n := countMatches(r.re, text)
		hits[r.class] = n
		if n > bestN {
			best, bestN = r.class, n
			bestQuote, _ = snip(r.re, text, 30)
		}
	}
	if hits[domain.RoleBackend] >= 2 && hits[domain.RoleFrontend] >= 2 {
		q, _ := snip(descRoleRules[1].re, text, 30)
		return domain.RoleFullstack, q
	}
	if best != "" {
		return best, bestQuote
	}
	if generic != "" {
		return domain.RoleOtherTech, generic
	}
	return domain.RoleUnknown, ""
}

// roleIsEngineering reports whether a class is an engineering role at all.
func roleIsEngineering(class string) bool {
	return class != domain.RoleNonTech && class != domain.RoleUnknown
}
