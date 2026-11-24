package assess

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Conversion constants. They are fixed on purpose: a rate that moves would change old assessments without any new
// information. Change them deliberately and bump domain.AssessorVersion.
const (
	// UsdToInr is the fixed rupee value of one US dollar used for every foreign-currency pay figure. Not live.
	UsdToInr = 85.0
	// EurToInr and GbpToInr are the fixed rupee values of one euro and one pound, held next to the dollar rate for the
	// same reason: not live, so old assessments do not move without new information.
	EurToInr = 92.0
	GbpToInr = 108.0
	// HoursPerMonth turns an hourly rate into a monthly one: 40 hours a week for four weeks.
	HoursPerMonth = 160.0
	// DaysPerMonth turns a daily rate into a monthly one: 22 working days.
	DaysPerMonth = 22.0
)

// Comp is pay read from text.
type Comp struct {
	// MonthlyINR is the monthly pay in rupees: the low end of a range, so it is never an overstatement. 0 with
	// Unpaid set is stated unpaid work.
	MonthlyINR float64
	Unpaid     bool
	// Quote is the exact text it was read from.
	Quote string
	// Annual is true when the text states the figure per year (a salary, "6 LPA"): MonthlyINR is then only that figure
	// divided by 12, an employee-salary equivalent, and is never presented as an internship stipend.
	Annual bool
	// Range is the high end of a stated range in monthly rupees, 0 when the text gave one figure.
	High float64
}

var (
	amountRe = regexp.MustCompile(`(?i)(?:\b(?:rs\.?|inr|usd|eur|gbp)|₹|€|£|us\$|\$)\s*([\d][\d,]*(?:\.\d+)?)\s*(k|lakhs?|lacs?|lpa)?(?:\s*(?:-|–|—|to)\s*(?:(?:\b(?:rs\.?|inr|usd|eur|gbp)|₹|€|£|us\$|\$)\s*)?([\d][\d,]*(?:\.\d+)?)\s*(k|lakhs?|lacs?|lpa)?)?`)
	// amountFirstRe is "10,000 INR per month".
	amountFirstRe = regexp.MustCompile(`(?i)\b([\d][\d,]*(?:\.\d+)?)\s*(k)?\s*(?:inr|rupees|rs\.?|usd|dollars|eur|euros|gbp|pounds)\b`)
	// foreignDollarPrefix is a letter prefix that makes "$" another country's dollar (CA$, A$, S$, CAD $).
	foreignDollarPrefix = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:ca|au|nz|hk|sg|c|a|s|cad|aud|sgd|nzd|hkd)\s?$`)
	// dollarCountry is a place where a bare "$" is not the US dollar: Canada, Australia, Singapore, New Zealand, Hong Kong.
	dollarCountry = regexp.MustCompile(`(?i)\b(?:canada|canadian|toronto|vancouver|montreal|ottawa|calgary|waterloo|australia|australian|sydney|melbourne|brisbane|perth|singapore|new zealand|auckland|hong kong|mexico)\b|\b(?:cad|aud|sgd|nzd|hkd)\b`)
	periodRe      = regexp.MustCompile(`(?i)^[\s,()]*(?:/|per\s+|a\s+|each\s+|p\.\s*)?\s*(month|months|mo|monthly|year|years|yr|yearly|annum|annual|annually|week|weekly|wk|hour|hourly|hr|day|daily|lpa)\b`)
	payContext    = regexp.MustCompile(`(?i)\b(stipend|salary|compensation|pay|paid|rate|earn\w*|hourly|wage|remuneration|base|ctc)\b`)
	unpaidRe      = regexp.MustCompile(`(?i)\b(unpaid|non[- ]?paid|no stipend|without (?:a )?stipend|stipend:?\s*(?:nil|none|0)\b)`)
	negatedUnpaid = regexp.MustCompile(`(?i)\b(not|isn't|is not|never|no)\s+(?:an?\s+)?$`)
)

func parseNumber(s string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return v
}

func scale(v float64, unit string) float64 {
	switch strings.ToLower(unit) {
	case "k":
		return v * 1e3
	case "lakh", "lakhs", "lac", "lacs", "lpa":
		return v * 1e5
	}
	return v
}

// rateOf is the rupee value of one unit of the currency a figure's lead ("$", "€", "EUR", "₹") names.
func rateOf(lead string) float64 {
	switch {
	case strings.Contains(lead, "$") || strings.Contains(lead, "usd") || strings.Contains(lead, "dollar"):
		return UsdToInr
	case strings.Contains(lead, "€") || strings.Contains(lead, "eur"):
		return EurToInr
	case strings.Contains(lead, "£") || strings.Contains(lead, "gbp") || strings.Contains(lead, "pound"):
		return GbpToInr
	}
	return 1
}

// ambiguousDollar reports whether the figure at text[start:end], led by lead, is a bare "$" that may not be the US
// dollar: a CA$/A$/S$ prefix, or a place near the figure or in the listing's location that uses another dollar. An
// explicit "US$" or "USD" is never ambiguous.
func ambiguousDollar(text string, start, end int, lead, location string) bool {
	if !strings.Contains(lead, "$") || strings.Contains(lead, "us") {
		return false
	}
	if foreignDollarPrefix.MatchString(text[max(0, start-5):start]) {
		return true
	}
	return dollarCountry.MatchString(location) || dollarCountry.MatchString(text[max(0, start-60):min(len(text), end+60)])
}

// monthly converts an amount per period into monthly rupees (amount is already in rupees). ok is false for a period it
// cannot place.
func monthly(amount float64, period string) (float64, bool) {
	switch strings.ToLower(period) {
	case "month", "months", "mo", "monthly":
		return amount, true
	case "year", "years", "yr", "yearly", "annum", "annual", "annually", "lpa":
		return amount / 12, true
	case "week", "weekly", "wk":
		return amount * 52 / 12, true
	case "hour", "hourly", "hr":
		return amount * HoursPerMonth, true
	case "day", "daily":
		return amount * DaysPerMonth, true
	}
	return 0, false
}

func isAnnual(period string) bool {
	switch strings.ToLower(period) {
	case "year", "years", "yr", "yearly", "annum", "annual", "annually", "lpa":
		return true
	}
	return false
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// amountIn finds the first stated pay with a period in text. needContext asks for a pay word near the figure, which
// a free-text description needs ("raised $50 million" is not pay) and a pay field does not.
func amountIn(text string, needContext bool, location string) (*Comp, bool) {
	for _, m := range amountRe.FindAllStringSubmatchIndex(text, -1) {
		lead := strings.ToLower(text[m[0]:m[2]])
		rate := rateOf(lead)
		if ambiguousDollar(text, m[0], m[1], lead, location) {
			continue
		}
		unit1 := ""
		if m[5] >= 0 {
			unit1 = text[m[4]:m[5]]
		}
		low := scale(parseNumber(text[m[2]:m[3]]), unit1)
		high, unit2 := 0.0, ""
		if m[6] >= 0 {
			if m[9] >= 0 {
				unit2 = text[m[8]:m[9]]
			}
			if unit2 == "" {
				unit2 = unit1
			}
			high = scale(parseNumber(text[m[6]:m[7]]), unit2)
		}
		period := ""
		if strings.EqualFold(unit1, "lpa") || strings.EqualFold(unit2, "lpa") {
			period = "lpa"
		} else if pm := periodRe.FindStringSubmatch(text[m[1]:min(len(text), m[1]+30)]); pm != nil {
			period = pm[1]
		}
		if period == "" {
			continue
		}
		if needContext {
			from := max(0, m[0]-80)
			if !payContext.MatchString(text[from:min(len(text), m[1]+30)]) {
				continue
			}
		}
		mo, ok := monthly(low*rate, period)
		if !ok || mo <= 0 {
			continue
		}
		c := &Comp{MonthlyINR: round2(mo), Quote: around(text, m[0], m[1], 12), Annual: isAnnual(period)}
		if high > low {
			if h, ok := monthly(high*rate, period); ok {
				c.High = round2(h)
			}
		}
		return c, true
	}
	if !needContext {
		// "10,000 INR per month"
		for _, m := range amountFirstRe.FindAllStringSubmatchIndex(text, -1) {
			unit := ""
			if m[4] >= 0 {
				unit = text[m[4]:m[5]]
			}
			cur := strings.ToLower(text[max(0, m[1]-6):m[1]])
			rate := rateOf(cur)
			if rate == UsdToInr && dollarCountry.MatchString(location) {
				continue
			}
			pm := periodRe.FindStringSubmatch(text[m[1]:min(len(text), m[1]+30)])
			if pm == nil {
				continue
			}
			if mo, ok := monthly(scale(parseNumber(text[m[2]:m[3]]), unit)*rate, pm[1]); ok && mo > 0 {
				return &Comp{MonthlyINR: round2(mo), Quote: around(text, m[0], m[1], 12), Annual: isAnnual(pm[1])}, true
			}
		}
	}
	return nil, false
}

func unpaidIn(text string) (*Comp, bool) {
	for _, m := range unpaidRe.FindAllStringIndex(text, -1) {
		lead := text[max(0, m[0]-14):m[0]]
		if negatedUnpaid.MatchString(lead) {
			continue
		}
		return &Comp{Unpaid: true, Quote: around(text, m[0], m[1], 12)}, true
	}
	return nil, false
}

// ParseCompensation reads the pay a posting states: the pay field first, then the title ("(Unpaid)"), then the
// description. A figure is converted to monthly rupees at the fixed rates above and, for a range, is its low end.
// Pay the text does not state, or states without a period ("a stipend of 10,000"), is nil: unknown, never guessed.
func ParseCompensation(pay, title, description string) *Comp {
	return ParseCompensationAt(pay, title, description, "")
}

// ParseCompensationAt is ParseCompensation with the listing's location: a bare "$" in a listing placed in Canada,
// Australia, Singapore and the like may be that country's dollar, so it is unknown rather than read as US dollars.
// A figure stated per year is marked Annual: it is an employee salary, and its monthly value is only that figure
// divided by twelve. Euro and pound figures are converted at the fixed rates above.
func ParseCompensationAt(pay, title, description, location string) *Comp {
	pay, title, desc := norm(pay), norm(title), norm(description)
	loc := norm(location)
	if c, ok := amountIn(pay, false, loc); ok {
		return c
	}
	if c, ok := unpaidIn(pay); ok {
		return c
	}
	if c, ok := unpaidIn(title); ok {
		return c
	}
	if c, ok := amountIn(title, true, loc); ok {
		return c
	}
	if c, ok := amountIn(desc, true, loc); ok {
		return c
	}
	if c, ok := unpaidIn(desc); ok {
		return c
	}
	return nil
}

var (
	durationRe      = regexp.MustCompile(`(?i)\b(\d{1,2})(?:\s*(?:-|–|to)\s*(\d{1,2}))?[\s-]*(months?|weeks?)\b`)
	durationContext = regexp.MustCompile(`(?i)\b(intern\w*|duration|period|term|program(?:me)?|commitment|contract|tenure|long|full-time for)\b`)
	durationNot     = regexp.MustCompile(`(?i)^\s*(?:of\b|ago\b|notice|prior|old\b|experience|before|after)`)
)

// ParseDuration reads how long an internship lasts ("6 months", "8-12 weeks") from the text, with the words it came
// from, or "" when the text does not say.
func ParseDuration(title, description string) (string, string) {
	text := norm(title + " . " + description)
	for _, m := range durationRe.FindAllStringSubmatchIndex(text, -1) {
		if durationNot.MatchString(text[m[1]:min(len(text), m[1]+20)]) {
			continue
		}
		lead := strings.ToLower(text[max(0, m[0]-12):m[0]])
		if strings.HasSuffix(strings.TrimSpace(lead), "within") || strings.HasSuffix(strings.TrimSpace(lead), "every") || strings.HasSuffix(strings.TrimSpace(lead), "last") || strings.HasSuffix(strings.TrimSpace(lead), "past") || strings.HasSuffix(strings.TrimSpace(lead), "per") || strings.HasSuffix(strings.TrimSpace(lead), "after") {
			continue
		}
		if !durationContext.MatchString(text[max(0, m[0]-60):min(len(text), m[1]+60)]) {
			continue
		}
		n := text[m[2]:m[3]]
		if m[4] >= 0 {
			n += "-" + text[m[4]:m[5]]
		}
		unit := strings.ToLower(text[m[6]:m[7]])
		if !strings.HasSuffix(unit, "s") {
			unit += "s"
		}
		return fmt.Sprintf("%s %s", n, unit), around(text, m[0], m[1], 12)
	}
	return "", ""
}
