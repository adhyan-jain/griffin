package domain

import (
	"strings"
	"testing"
)

const claimsFacts = `Adhyan Jain, B.Tech Computer Science at Vellore Institute of Technology, expected graduation 2028.
ComputeHive: a distributed task runner in Go with gRPC and Postgres. Palni: AI Engineer intern.
Posting: Backend Intern at Acme Technologies, Bengaluru. You will work on Go services.`

func TestCheckClaimsAcceptsOnlyWhatTheFactsSay(t *testing.T) {
	good := "Dear Hiring Team, I am applying for the Backend Intern role at Acme Technologies. At Palni I worked as an AI Engineer intern, and I built ComputeHive, a distributed task runner in Go. Sincerely, Adhyan Jain"
	if p := CheckClaims(good, claimsFacts); len(p) != 0 {
		t.Fatalf("a grounded letter was rejected: %v", p)
	}
}

func TestCheckClaimsRejectsInventedClaims(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"an invented number", "I improved ComputeHive throughput by 40% across the cluster.", "40%"},
		{"an invented employer mid-sentence", "Before this I interned at Google on the search team.", "Google"},
		{"an invented employer opening a sentence", "Google was where I first learned systems. I built ComputeHive.", "Google"},
		{"a count in words", "I have five years of experience building services.", "five years"},
		{"a placeholder", "I would love to join [Company Name] this summer.", "[Company Name]"},
		{"an invented product", "I also shipped Zephyr, a payments platform, in my first year.", "Zephyr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := CheckClaims(c.text, claimsFacts)
			if len(problems) == 0 {
				t.Fatalf("an invented claim passed: %q", c.text)
			}
			if !strings.Contains(strings.Join(problems, " | "), c.want) {
				t.Fatalf("problems %v do not name %q", problems, c.want)
			}
		})
	}
}

func TestCheckClaimsAllowsYearsAndLetterWords(t *testing.T) {
	text := "Dear Hiring Team, I will graduate in 2028 and would like the internship. Sincerely, Adhyan"
	if p := CheckClaims(text, claimsFacts); len(p) != 0 {
		t.Fatalf("ordinary letter words or a year were flagged: %v", p)
	}
}
