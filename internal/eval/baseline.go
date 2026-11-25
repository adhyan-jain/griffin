package eval

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// The baseline is the opportunity pipeline as it was in d57ec39, frozen here so later changes to the live code can
// never move the "before" numbers. It is copied from internal/sources/sites.go and internal/domain/interndock.go at
// that commit:
//
//   - retrieval: a title regex for "intern", the owner-fit role regexes (fitRole, notMyJob) and the India or
//     worldwide-remote location rule, then a floor of Rs 10,000 a month on the stated stipend (InternDock exempt);
//   - ranking: the stipend tier (A at Rs 40,000 a month with a floor of Rs 15,000, B at Rs 15,000, C the rest), then the
//     stipend itself;
//   - eligibility: never read, every listing assumed open to the owner;
//   - dedupe: the database's unique key, the exact company and role text.
//
// The old pipeline read only the three aggregators (Internshala, Unstop, InternDock); employer boards were removed.
// BaselineSeesATS says whether the baseline is run over employer-board records anyway (the same filter applied to
// everything), which isolates the filter from the source coverage.

const (
	baselineMinStipend       = 10000 // sources.MinStipend at d57ec39
	baselineMinInternStipend = 40000 // domain.MinInternStipendINR
)

var (
	baselineInternship = regexp.MustCompile(`(?i)\b(intern|interns|internship|trainee)\b`)
	baselineFitRole    = regexp.MustCompile(`(?i)software|backend|back-end|full[- ]?stack|platform|infrastructure|distributed|machine learning|\bml\b|\bai\b|artificial intelligence|\bllm|generative|data scien|deep learning|computer vision|\bnlp\b|developer|\bsde\b|\bswe\b|engineer`)
	baselineNotMyJob   = regexp.MustCompile(`(?i)trainer|subject matter|\bsme\b|tutor|mentor|teach|front[- ]?end|angular|ui/ux|\bui\b|game|wordpress|shopify|test|\bqa\b|quality|support|sales|marketing|analyst|account|\bhr\b|recruit|design|mechanical|civil|electrical|hardware|embedded|manufactur|supply|operations|finance|legal|apprentice|founder'?s office|content|business`)
	baselineIndiaPlace = regexp.MustCompile(`(?i)\b(india|bengaluru|bangalore|mumbai|delhi|new delhi|ncr|gurgaon|gurugram|noida|hyderabad|pune|chennai|kolkata|ahmedabad|vellore|jaipur|kochi|chandigarh|indore|coimbatore)\b`)
	baselineAnywhere   = regexp.MustCompile(`(?i)\b(anywhere|worldwide|global|globally|apac|asia)\b`)
	baselineNumber     = regexp.MustCompile(`\d[\d,]*`)
)

func baselineInIndiaOrWorldwide(loc string) bool {
	if baselineIndiaPlace.MatchString(loc) {
		return true
	}
	return strings.Contains(strings.ToLower(loc), "remote") && baselineAnywhere.MatchString(loc)
}

func baselineMonthlyStipend(pay string) int {
	if !strings.Contains(strings.ToLower(pay), "/month") {
		return 0
	}
	best := 0
	for _, n := range baselineNumber.FindAllString(pay, -1) {
		if v, err := strconv.Atoi(strings.ReplaceAll(n, ",", "")); err == nil && v > best {
			best = v
		}
	}
	return best
}

func baselineLowestStipend(pay string) int {
	low := 0
	for _, n := range baselineNumber.FindAllString(pay, -1) {
		if v, err := strconv.Atoi(strings.ReplaceAll(n, ",", "")); err == nil && (low == 0 || v < low) {
			low = v
		}
	}
	return low
}

// baselineTier is the old ranking tier: "A", "B" or "C".
func baselineTier(pay string) string {
	switch s := baselineMonthlyStipend(pay); {
	case s >= baselineMinInternStipend && baselineLowestStipend(pay) >= 15000:
		return "A"
	case s >= 15000:
		return "B"
	default:
		return "C"
	}
}

// baselineFits is sources.Fits at d57ec39, with the reason when a posting is dropped.
func baselineFits(j jobs.RawJob) (bool, string) {
	location := strings.Join(j.Locations, "; ")
	switch {
	case !baselineInternship.MatchString(j.Title):
		return false, "the title does not contain intern, internship or trainee"
	case !baselineFitRole.MatchString(j.Title):
		return false, "the title matches none of the owner-fit role words (software, backend, ai, engineer, ...)"
	case baselineNotMyJob.MatchString(j.Title):
		return false, "the title matches an excluded word (" + baselineNotMyJob.FindString(j.Title) + ")"
	case !baselineInIndiaOrWorldwide(location):
		return false, "the location is not an Indian city or remote-worldwide"
	case j.Source != "interndock" && baselineMonthlyStipend(j.Compensation) < baselineMinStipend:
		return false, "no stated monthly stipend of Rs 10,000 or more (" + orNone(j.Compensation) + ")"
	}
	return true, "kept by the title, role, place and Rs 10,000 floor rules"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "no pay stated"
	}
	return s
}
