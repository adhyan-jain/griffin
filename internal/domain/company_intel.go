package domain

import (
	"regexp"
	"strconv"
	"strings"
)

// Company pay potential levels.
const (
	PayPotentialHigh       = "HIGH"
	PayPotentialMedium     = "MEDIUM"
	PayPotentialLowUnknown = "LOW/UNKNOWN"
)

// Company business, funding, and compensation strength ratings.
const (
	StrengthHigh       = "HIGH"
	StrengthMedium     = "MEDIUM"
	StrengthLowUnknown = "LOW/UNKNOWN"
)

// Target sectors corresponding to the ProcureYard archetype and high-conviction startups.
const (
	SectorProcurementSupplyChain = "Procurement & Supply Chain"
	SectorVerticalSaaS           = "Vertical SaaS & Industrial Tech"
	SectorDevInfra               = "Developer Infrastructure & Tools"
	SectorFinTech                = "FinTech & Payments"
	SectorEnterprise             = "Enterprise Product"
)

var (
	procureYardPattern = regexp.MustCompile(`(?i)\b(procurement|b2b commerce|raw materials|supply chain|logistics|warehousing|freight|industrial|construction tech|mro|manufacturing)\b`)
	devInfraPattern    = regexp.MustCompile(`(?i)\b(developer tools|infrastructure|database|observability|api platform|devops|cloud native|distributed)\b`)
	fintechPattern     = regexp.MustCompile(`(?i)\b(fintech|payments|neobank|banking infrastructure|billing|payroll|lending)\b`)

	// "28 LPA", "28 lakh per annum": INR lakhs per year.
	lpaPayPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:lpa\b|(?:lakhs?|lacs?)\s*(?:per\s*annum|p\.?\s?a\b|/\s*(?:yr|year|annum)|per\s*(?:yr|year)|annually))`)
	// "₹40k/month", "Rs 1,50,000 per month", "INR 30,00,000 per annum": an INR amount and the period it is paid for.
	inrPayPattern = regexp.MustCompile(`(?i)(?:₹|\brs\.?|\binr)\s*([\d,]+(?:\.\d+)?)\s*(k\b|lakhs?\b|lacs?\b|cr\b|crores?\b)?\s*(?:(?:/|\bper\b|\ba\b)\s*)?(months?\b|mo\b|mth\b|pm\b|yrs?\b|years?\b|annum\b|pa\b)?`)
	// "$150k", "$150,000", "$8k/month", "$60/hour": a USD amount and the period it is paid for.
	usdPayPattern = regexp.MustCompile(`(?i)\$\s*([\d,]+(?:\.\d+)?)\s*(k\b)?\s*(?:(?:/|\bper\b|\ba\b)\s*)?(hours?\b|hr\b|weeks?\b|days?\b|months?\b|mo\b|yrs?\b|years?\b|annum\b)?`)
)

// IsProcureYardArchetype checks if a company's sector or description matches the ProcureYard archetype
// (B2B procurement, supply chain, industrial tech, dev infra, or high-growth fintech).
func IsProcureYardArchetype(sector, industry, description string) bool {
	combined := strings.Join([]string{sector, industry, description}, " ")
	return procureYardPattern.MatchString(combined) || devInfraPattern.MatchString(combined) || fintechPattern.MatchString(combined)
}

// ClassifyCompanySector determines the high-level sector for a startup.
func ClassifyCompanySector(sector, industry, description string) string {
	combined := strings.Join([]string{sector, industry, description}, " ")
	switch {
	case procureYardPattern.MatchString(combined):
		return SectorProcurementSupplyChain
	case devInfraPattern.MatchString(combined):
		return SectorDevInfra
	case fintechPattern.MatchString(combined):
		return SectorFinTech
	default:
		return SectorEnterprise
	}
}

// Pay thresholds. Annual INR: 25 LPA and up is HIGH, 15 to under 25 LPA is MEDIUM. A monthly INR figure is never
// read as annual: it is compared with the same bands expressed per month (HIGH from Rs 2,00,000 a month, MEDIUM from
// Rs 1,25,000), so a Rs 40k/month stipend is LOW, not HIGH. Annual USD: $120k and up is HIGH, $80k and up MEDIUM
// (a monthly USD figure is multiplied by 12; hourly, daily and weekly figures are ignored).
const (
	highINRAnnual    = 25_00_000
	mediumINRAnnual  = 15_00_000
	highINRMonthly   = 2_00_000
	mediumINRMonthly = 1_25_000
	highUSDAnnual    = 120_000
	mediumUSDAnnual  = 80_000
)

// EvaluatePayPotential rates pay from advertised or employee-reported pay text only. Funding stage, company size
// and reputation say nothing about pay and are not inputs. No readable figure is LOW/UNKNOWN, which is "not known",
// never a reason to drop a company.
func EvaluatePayPotential(advertisedPay, employeeReportedPay string) string {
	text := advertisedPay + " " + employeeReportedPay
	level := PayPotentialLowUnknown
	rank := func(l string) {
		if l == PayPotentialHigh || (l == PayPotentialMedium && level == PayPotentialLowUnknown) {
			level = l
		}
	}
	band := func(v, high, medium float64) string {
		switch {
		case v >= high:
			return PayPotentialHigh
		case v >= medium:
			return PayPotentialMedium
		}
		return PayPotentialLowUnknown
	}
	for _, m := range lpaPayPattern.FindAllStringSubmatch(text, -1) {
		lakhs, _ := strconv.ParseFloat(m[1], 64)
		rank(band(lakhs*1e5, highINRAnnual, mediumINRAnnual))
	}
	for _, m := range inrPayPattern.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(m[2]) {
		case "k":
			v *= 1e3
		case "lakh", "lakhs", "lac", "lacs":
			v *= 1e5
		case "cr", "crore", "crores":
			v *= 1e7
		}
		switch period := strings.ToLower(m[3]); {
		case strings.HasPrefix(period, "m") || period == "pm":
			rank(band(v, highINRMonthly, mediumINRMonthly))
		case strings.HasPrefix(period, "y") || period == "annum" || period == "pa":
			rank(band(v, highINRAnnual, mediumINRAnnual))
		}
		// No period: the figure could be monthly or annual, so it is not used.
	}
	for _, m := range usdPayPattern.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			continue
		}
		if strings.EqualFold(m[2], "k") {
			v *= 1e3
		}
		switch period := strings.ToLower(m[3]); {
		case strings.HasPrefix(period, "m"):
			v *= 12
		case strings.HasPrefix(period, "h"), strings.HasPrefix(period, "w"), strings.HasPrefix(period, "d"):
			continue
		}
		rank(band(v, highUSDAnnual, mediumUSDAnnual))
	}
	return level
}

// EvaluateFundingStrength rates the funding stage. It is a separate fact from pay: a well-funded company can
// pay little, and the two are never combined into one rating.
func EvaluateFundingStrength(fundingStage string) string {
	fs := strings.ToUpper(fundingStage)
	switch {
	case strings.Contains(fs, "SERIES B"), strings.Contains(fs, "SERIES C"), strings.Contains(fs, "GROWTH"):
		return StrengthHigh
	case strings.Contains(fs, "SERIES A"), strings.Contains(fs, "SEED"):
		return StrengthMedium
	}
	return StrengthLowUnknown
}

var (
	// IT services and consulting body shops: Mass hiring with low engineering leverage and minimal pay growth.
	itServicesPattern = regexp.MustCompile(`(?i)\b(tcs|tata consultancy|infosys|wipro|cognizant|accenture|capgemini|hcl\s*tech|tech\s*mahindra|l&t\s*infotech|ltimindtree|mindtree|mphasis|hexaware|syntel|persistent\s*systems|zensar|genpact|virtusa|birlasoft)\b`)

	// Mega-corps / Big Tech giants: Fixed application portals, closed seasonal deadlines, and low referral conversion.
	megaCorpPattern = regexp.MustCompile(`(?i)\b(amazon|aws|microsoft|google|alphabet|meta|facebook|apple|oracle|cisco|intel|ibm|qualcomm|dell|hp|salesforce)\b`)
)

// IsExcludedEmployer detects whether an employer is an IT services consultancy or an impersonal mega-corp.
// High-growth startups and venture-backed scale-ups (ProcureYard archetype) are prioritised instead.
func IsExcludedEmployer(company string) (excluded bool, reason string) {
	c := strings.TrimSpace(company)
	if itServicesPattern.MatchString(c) {
		return true, "IT Services / Consultancy: Mass recruitment agency with low compensation potential and lack of product engineering referral leverage."
	}
	if megaCorpPattern.MatchString(c) {
		return true, "Mega-Corp / Big Tech: Strict standardized ATS portals where referral influence is ineffective and application cycles are closed."
	}
	return false, ""
}
