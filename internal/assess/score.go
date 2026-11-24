package assess

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// Component names, in the order they are scored. Their maxima sum to 100.
const (
	CompDepth        = "depth"
	CompEmployer     = "employer"
	CompCompensation = "compensation"
	CompRelevance    = "relevance"
	CompMentorship   = "mentorship"
	CompPPO          = "ppo"
	CompFeasibility  = "feasibility"
	CompCap          = "cap" // a note, not a component: why the class was held down (Max 0)
)

// Labels are the owner-facing names of the components.
var Labels = map[string]string{
	CompDepth: "Engineering depth", CompEmployer: "Employer", CompCompensation: "Pay", CompRelevance: "Fit with your skills",
	CompMentorship: "Mentorship", CompPPO: "Full-time offer", CompFeasibility: "Feasibility", CompCap: "Class limit",
}

// strongEngineeringEmployers is a hand-curated, conservative list of employers with a well-known public engineering
// reputation (products and infrastructure that are engineered, not outsourced). It is a deterministic signal, not a
// guess about any one posting: a company on it scores high on employer quality, a company off it is simply unknown
// (not bad). Entries are normalized employer names (domain.NormalizeEmployer). Extend it by hand; add nothing on a hunch.
var strongEngineeringEmployers = map[string]bool{
	// Big tech and infrastructure
	"google": true, "alphabet": true, "microsoft": true, "amazon": true, "amazon web services": true, "aws": true, "meta": true, "meta platforms": true,
	"apple": true, "netflix": true, "nvidia": true, "intel": true, "ibm": true, "oracle": true, "adobe": true, "salesforce": true, "cisco": true,
	"openai": true, "anthropic": true, "deepmind": true, "google deepmind": true, "mistral ai": true, "hugging face": true, "cohere": true,
	// Developer-tool, data and cloud companies
	"stripe": true, "cloudflare": true, "github": true, "gitlab": true, "jetbrains": true, "hashicorp": true, "datadog": true, "snowflake": true,
	"databricks": true, "mongodb": true, "confluent": true, "elastic": true, "vercel": true, "supabase": true, "figma": true, "notion": true,
	"linear": true, "atlassian": true, "palantir": true, "uber": true, "airbnb": true, "lyft": true, "dropbox": true, "slack": true, "twilio": true,
	"shopify": true, "spotify": true, "block": true, "coinbase": true, "robinhood": true, "plaid": true, "scale ai": true, "replit": true,
	// Quant and systems
	"jane street": true, "citadel": true, "citadel securities": true, "two sigma": true, "hudson river trading": true, "de shaw": true, "d e shaw": true,
	// India
	"razorpay": true, "zerodha": true, "phonepe": true, "cred": true, "swiggy": true, "zomato": true, "flipkart": true, "meesho": true, "groww": true,
	"juspay": true, "postman": true, "browserstack": true, "freshworks": true, "zoho": true, "thoughtworks": true, "sarvam ai": true, "sarvam": true,
	"atlan": true, "hasura": true, "sprinklr": true, "uber india": true, "google india": true, "microsoft india": true, "amazon development centre india": true,
	"ola": true, "ola krutrim": true, "krutrim": true, "dream11": true, "slice": true, "navi": true, "paytm": true, "ather energy": true,
}

var foundationName = regexp.MustCompile(`(?i)\b(foundation|trust|ngo|society|charit\w*|non-?profit|association)\b`)

// round1 rounds to one decimal.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// component builds one scored component.
func component(name string, max, points float64, reason, quote string, unknown bool) domain.ValueComponent {
	return domain.ValueComponent{Name: name, Max: max, Points: round1(points), Reason: reason, Quote: quote, Unknown: unknown}
}

// Thresholds on unknown weight and caps. Documented here because they decide classes:
//   - A listing where more than unknownCapWeight of the 100 points were scored at the neutral midpoint (not read from
//     the posting) is at most "exploratory": there is not enough evidence to call it good.
//   - A role outside India with no relocation or visa support is at most "exploratory" (feasibility).
//   - A non-technical role scores at most nonTechCap and is "low".
//   - An ineligible listing's class is "low" (the score is kept so the reasons can be read).
const (
	unknownCapWeight = 60.0
	nonTechCap       = 25
)

// Score computes the career-value score of one listing: engineering depth and ownership 25, employer quality 20,
// compensation 15, skill and project relevance 15, mentorship and learning 10, PPO and future 10, practical
// feasibility 5. A component the posting does not inform is scored at the midpoint of its range and flagged unknown;
// it is never filled in by guessing, and the class is capped when too much of the score is unknown.
func Score(in Input, f Findings, eligibility string) (int, []domain.ValueComponent, string) {
	var comps []domain.ValueComponent

	// 1. Engineering depth and ownership (25).
	switch {
	case f.Role == domain.RoleNonTech:
		comps = append(comps, component(CompDepth, 25, 0, "The role is not an engineering role", f.RoleQuote, false))
	case f.Depth.Level == DepthDeep:
		comps = append(comps, component(CompDepth, 25, 25, f.Depth.Reason, f.Depth.Quote, false))
	case f.Depth.Level == DepthModerate:
		comps = append(comps, component(CompDepth, 25, 16, f.Depth.Reason, f.Depth.Quote, false))
	case f.Depth.Level == DepthShallow:
		comps = append(comps, component(CompDepth, 25, 5, f.Depth.Reason, f.Depth.Quote, false))
	default:
		comps = append(comps, component(CompDepth, 25, 12.5, f.Depth.Reason, "", true))
	}

	// 2. Employer quality and engineering reputation (20): deterministic signals only.
	comps = append(comps, employerComponent(in, f))

	// 3. Compensation (15). It cannot dominate: it is the same size as a quarter of the engineering signals.
	comps = append(comps, compensationComponent(f))

	// 4. Skill and project relevance (15): the role's kind and the owner's skills the posting asks for.
	comps = append(comps, relevanceComponent(f))

	// 5. Mentorship and learning (10). Absence in the text is not evidence of none, so it stays unknown.
	if f.Mentorship.Level == "present" {
		comps = append(comps, component(CompMentorship, 10, 10, f.Mentorship.Reason, f.Mentorship.Quote, false))
	} else {
		comps = append(comps, component(CompMentorship, 10, 5, "The posting does not say whether the intern is mentored", "", true))
	}

	// 6. PPO and future (10).
	switch f.PPO.Level {
	case "yes":
		comps = append(comps, component(CompPPO, 10, 10, f.PPO.Reason, f.PPO.Quote, false))
	case "conditional":
		comps = append(comps, component(CompPPO, 10, 7, f.PPO.Reason, f.PPO.Quote, false))
	case "no":
		comps = append(comps, component(CompPPO, 10, 0, f.PPO.Reason, f.PPO.Quote, false))
	default:
		comps = append(comps, component(CompPPO, 10, 5, "The posting does not say whether a full-time offer can follow", "", true))
	}

	// 7. Practical feasibility (5): can the owner actually take it.
	comps = append(comps, feasibilityComponent(f, eligibility))

	var total, unknownWeight float64
	for _, c := range comps {
		total += c.Points
		if c.Unknown {
			unknownWeight += c.Max
		}
	}
	score := int(math.Round(total))
	class := domain.ValueClassFor(score)

	downTo := func(limit string) {
		order := []string{domain.ValueLow, domain.ValueExploratory, domain.ValueGood, domain.ValueStrong, domain.ValueExceptional}
		idx := func(c string) int {
			for i, o := range order {
				if o == c {
					return i
				}
			}
			return 0
		}
		if idx(class) > idx(limit) {
			class = limit
		}
	}
	if unknownWeight > unknownCapWeight {
		downTo(domain.ValueExploratory)
		comps = append(comps, component(CompCap, 0, 0, fmt.Sprintf("%.0f of the 100 points could not be read from the listing, so it is held at exploratory until more is known", unknownWeight), "", true))
	}
	if f.Loc.Fit == domain.LocationOutside {
		downTo(domain.ValueExploratory)
		comps = append(comps, component(CompCap, 0, 0, "The role is outside India and the posting does not offer relocation or visa support", f.Loc.Quote, false))
	}
	if f.Role == domain.RoleNonTech {
		if score > nonTechCap {
			score = nonTechCap
		}
		class = domain.ValueLow
		comps = append(comps, component(CompCap, 0, 0, "Not an engineering role: capped at low", f.RoleQuote, false))
	}
	if eligibility == domain.EligibilityIneligible {
		class = domain.ValueLow
		comps = append(comps, component(CompCap, 0, 0, "The owner is not eligible: the class is low whatever the role is worth", "", false))
	}
	// A listing is ranked by its score, so a cap that lowers the class must lower the score too: otherwise an ineligible
	// role, or one abroad with no visa support, keeps a high score and sits above roles the owner can take. The
	// breakdown still shows every component as read.
	if ceiling := classCeiling(class); score > ceiling {
		score = ceiling
	}
	return score, comps, class
}

// classCeiling is the highest score a class holds: one below the next class's lowest score (domain.ValueClassFor).
func classCeiling(class string) int {
	switch class {
	case domain.ValueExceptional:
		return 100
	case domain.ValueStrong:
		return 79
	case domain.ValueGood:
		return 64
	case domain.ValueExploratory:
		return 49
	}
	return 34
}

func employerComponent(in Input, f Findings) domain.ValueComponent {
	key := domain.NormalizeEmployer(in.Company)
	verified := in.Verification == domain.VerificationEmployer && domain.IsEmployerKind(in.SourceKind)
	switch {
	case strongEngineeringEmployers[key]:
		why := "On Griffin's curated list of employers with a strong public engineering reputation"
		if verified {
			why += "; read from the employer's own board"
		}
		return component(CompEmployer, 20, 20, why, "", false)
	case in.CompanyKind == "foundation" || foundationName.MatchString(in.Company):
		return component(CompEmployer, 20, 4, "A foundation or non-profit: little engineering reputation to go on", in.Company, false)
	case in.CompanyKind == "lab":
		return component(CompEmployer, 20, 16, "A research lab", "", false)
	case verified:
		return component(CompEmployer, 20, 12, "Read from the employer's own board; its engineering reputation is not known to Griffin", "", true)
	}
	return component(CompEmployer, 20, 10, "A third-party listing of an employer whose engineering reputation is not known to Griffin", "", true)
}

func compensationComponent(f Findings) domain.ValueComponent {
	c := f.Comp
	switch {
	case c == nil:
		return component(CompCompensation, 15, 7.5, "The listing does not state pay", "", true)
	case c.Unpaid && f.Role == domain.RoleResearch:
		return component(CompCompensation, 15, 7.5, "Unpaid, but research roles are often unpaid: scored neutral, not low", c.Quote, false)
	case c.Unpaid:
		return component(CompCompensation, 15, 0, "Unpaid", c.Quote, false)
	}
	if c.Annual {
		return component(CompCompensation, 15, 7.5, fmt.Sprintf("The listing states a yearly figure (about ₹%s a month if divided by 12): an employee salary, not an internship stipend, so scored neutral", thousands(c.MonthlyINR)), c.Quote, true)
	}
	var pts float64
	switch m := c.MonthlyINR; {
	case m >= 60000:
		pts = 15
	case m >= 40000:
		pts = 13
	case m >= 25000:
		pts = 10
	case m >= 15000:
		pts = 8.5
	case m >= 8000:
		pts = 5
	default:
		pts = 3
	}
	return component(CompCompensation, 15, pts, fmt.Sprintf("Pays about ₹%s a month (the low end of what the listing states)", thousands(c.MonthlyINR)), c.Quote, false)
}

// thousands writes 45000 as "45,000" (Indian grouping is not needed for a short reason).
func thousands(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	return strings.Join(out, ",")
}

func relevanceComponent(f Findings) domain.ValueComponent {
	var roleAffinity float64
	var roleWhy string
	switch f.Role {
	case domain.RoleBackend, domain.RoleAIML, domain.RoleInfra, domain.RoleResearch, domain.RoleFullstack, domain.RoleDevtools:
		roleAffinity, roleWhy = 6, "a "+strings.ReplaceAll(f.Role, "_", "/")+" role, which is the owner's core"
	case domain.RoleFrontend, domain.RoleData, domain.RoleMobile, domain.RoleSecurity:
		roleAffinity, roleWhy = 4, "a "+f.Role+" role, close to the owner's work"
	case domain.RoleOtherTech:
		roleAffinity, roleWhy = 3, "a technical role of no stated kind"
	case domain.RoleNonTech:
		return component(CompRelevance, 15, 0, "Not an engineering role", f.RoleQuote, false)
	default:
		roleAffinity, roleWhy = 3, "a role of unknown kind"
	}
	if f.TextLen < 100 {
		return component(CompRelevance, 15, roleAffinity+4.5, "Too little text to match skills; "+roleWhy, f.RoleQuote, true)
	}
	var skillPts float64
	switch n := len(f.Skills); {
	case n == 0:
		skillPts = 1
	case n == 1:
		skillPts = 4
	case n == 2:
		skillPts = 6
	case n == 3:
		skillPts = 8
	default:
		skillPts = 9
	}
	names := make([]string, 0, len(f.Skills))
	for _, s := range f.Skills {
		names = append(names, s.Skill)
	}
	why := roleWhy
	quote := ""
	if len(names) > 0 {
		why = fmt.Sprintf("Asks for %d of the owner's skills (%s); %s", len(names), strings.Join(names[:min(4, len(names))], ", "), roleWhy)
		quote = f.Skills[0].Quote
	} else {
		why = "Asks for none of the owner's listed skills; " + roleWhy
	}
	return component(CompRelevance, 15, roleAffinity+skillPts, why, quote, false)
}

func feasibilityComponent(f Findings, eligibility string) domain.ValueComponent {
	var pts float64
	var why []string
	unknown := false
	switch eligibility {
	case domain.EligibilityConfirmed:
		pts += 3
		why = append(why, "eligibility confirmed")
	case domain.EligibilityLikely:
		pts += 2.5
		why = append(why, "no restriction found")
	case domain.EligibilityIneligible:
		why = append(why, "not eligible")
	default:
		pts += 1.5
		unknown = true
		why = append(why, "eligibility unknown")
	}
	switch f.Loc.Fit {
	case domain.LocationIndia, domain.LocationRemoteOK:
		pts += 2
		why = append(why, "place works ("+f.Loc.Fit+")")
	case domain.LocationRelocation:
		pts += 1
		why = append(why, "needs relocation")
	case domain.LocationOutside:
		why = append(why, "outside India")
	default:
		pts += 1
		unknown = true
		why = append(why, "place unknown")
	}
	if f.Employment == domain.EmploymentFullTime {
		pts = math.Max(0, pts-2)
		why = append(why, "a full-time job, not an internship")
	}
	if pts > 5 {
		pts = 5
	}
	return component(CompFeasibility, 5, pts, strings.ToUpper(why[0][:1])+why[0][1:]+strings.Join(prefixAll(why[1:]), ""), "", unknown)
}

func prefixAll(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = "; " + p
	}
	return out
}
