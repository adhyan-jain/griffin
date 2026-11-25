package domain

import (
	"testing"
)

func TestIsProcureYardArchetype(t *testing.T) {
	cases := []struct {
		sector, industry, desc string
		want                   bool
	}{
		{"B2B Commerce", "Procurement", "Raw material marketplace for infrastructure", true},
		{"Supply Chain", "Logistics", "Intercity freight transportation network", true},
		{"Developer Tools", "Software", "Open source GraphQL engine and data API", true},
		{"FinTech", "Payments", "Modern banking and payment infrastructure for GenZ", true},
		{"Retail", "Fashion", "Handmade scarves e-commerce shop", false},
	}

	for _, tc := range cases {
		got := IsProcureYardArchetype(tc.sector, tc.industry, tc.desc)
		if got != tc.want {
			t.Errorf("IsProcureYardArchetype(%q, %q, %q) = %v, want %v", tc.sector, tc.industry, tc.desc, got, tc.want)
		}
	}
}

func TestEvaluatePayPotential(t *testing.T) {
	cases := []struct {
		name, adPay, repPay, want string
	}{
		{"LPA high", "32 LPA fixed + ESOPs", "", PayPotentialHigh},
		{"employee-reported LPA high", "", "Employee reports 28 LPA SDE2", PayPotentialHigh},
		{"LPA edge 25", "25 LPA", "", PayPotentialHigh},
		{"LPA medium", "18 LPA CTC", "", PayPotentialMedium},
		{"LPA 24 medium", "24 lakh per annum", "", PayPotentialMedium},
		{"LPA below medium", "12 LPA", "", PayPotentialLowUnknown},
		{"INR annual written out", "INR 30,00,000 per annum", "", PayPotentialHigh},
		{"monthly stipend is never annual", "Stipend of ₹40k/month", "", PayPotentialLowUnknown},
		{"monthly stipend per month", "Rs 40,000 per month", "", PayPotentialLowUnknown},
		{"monthly high", "₹2,50,000 per month", "", PayPotentialHigh},
		{"monthly medium", "₹1.5 lakh a month", "", PayPotentialMedium},
		{"INR with no period is not used", "₹4000000", "", PayPotentialLowUnknown},
		{"USD k", "$150k base", "", PayPotentialHigh},
		{"USD comma", "$130,000 a year", "", PayPotentialHigh},
		{"USD medium", "$90k", "", PayPotentialMedium},
		{"USD low", "$60k", "", PayPotentialLowUnknown},
		{"USD hourly ignored", "$200/hour", "", PayPotentialLowUnknown},
		{"USD monthly annualised", "$1,200 per month", "", PayPotentialLowUnknown},
		{"no pay text", "", "", PayPotentialLowUnknown},
		{"boasts are not figures", "top of market, tier-1 pay", "", PayPotentialLowUnknown},
	}
	for _, tc := range cases {
		if got := EvaluatePayPotential(tc.adPay, tc.repPay); got != tc.want {
			t.Errorf("%s: EvaluatePayPotential(%q, %q) = %v, want %v", tc.name, tc.adPay, tc.repPay, got, tc.want)
		}
	}
}

func TestFundingStageIsNotPay(t *testing.T) {
	// A funding stage is not an input to pay potential, so a funded company with no pay figure is not rated.
	if got := EvaluatePayPotential("Series B ($25M)", ""); got != PayPotentialLowUnknown {
		t.Errorf("a stage in the pay text must not produce pay: %v", got)
	}
	for stage, want := range map[string]string{
		"Series B ($25M)": StrengthHigh, "Growth": StrengthHigh, "Series A": StrengthMedium, "Seed": StrengthMedium,
		"Bootstrapped": StrengthLowUnknown, "": StrengthLowUnknown,
	} {
		if got := EvaluateFundingStrength(stage); got != want {
			t.Errorf("EvaluateFundingStrength(%q) = %v, want %v", stage, got, want)
		}
	}
}

func TestIsExcludedEmployer(t *testing.T) {
	cases := []struct {
		employer     string
		wantExcluded bool
	}{
		// IT Services / Body Shops
		{"TCS", true},
		{"Tata Consultancy Services", true},
		{"Infosys", true},
		{"Wipro", true},
		{"Cognizant", true},
		{"Accenture", true},
		{"Capgemini", true},
		{"HCL Tech", true},
		{"LTIMindtree", true},
		{"Tech Mahindra", true},
		{"Hexaware", true},
		{"Mphasis", true},

		// Mega-corps / Big Tech giants
		{"Amazon", true},
		{"AWS", true},
		{"Microsoft", true},
		{"Google", true},
		{"Meta", true},
		{"Apple", true},
		{"Oracle", true},
		{"Cisco", true},
		{"Intel", true},
		{"IBM", true},
		{"Qualcomm", true},
		{"Salesforce", true},

		// High-growth startups & scale-ups (ProcureYard archetype)
		{"ProcureYard", false},
		{"Zetwerk", false},
		{"Infra.Market", false},
		{"OfBusiness", false},
		{"Procol", false},
		{"Bizongo", false},
		{"Moglix", false},
		{"Metalbook", false},
		{"Porter", false},
		{"Hasura", false},
		{"Appsmith", false},
		{"SigNoz", false},
		{"FamPay", false},
		{"Razorpay", false},
		{"Winuall", false},
		{"Cashfree", false},
	}

	for _, tc := range cases {
		excluded, _ := IsExcludedEmployer(tc.employer)
		if excluded != tc.wantExcluded {
			t.Errorf("IsExcludedEmployer(%q) = %v, want %v", tc.employer, excluded, tc.wantExcluded)
		}
	}
}
