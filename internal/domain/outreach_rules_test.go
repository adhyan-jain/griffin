package domain

import (
	"strings"
	"testing"
)

const ownerFacts = `# Owner profile
- Full name: Adhyan Jain
- [f1] Education: B.Tech CSE at VIT Vellore, pre-final year
- [f2] Project: ComputeHive, a distributed task runner in Go with Redis and Docker
- [f3] Experience: AI Engineer intern at Palni
- [f4] Award: won a track at the DevSoc hackathon
- [f5] Club: senior core member of ACM VIT`

func engineer() OutreachContact {
	return OutreachContact{Name: "Riya Sharma", Title: "Software Engineer II", Company: "Razorpay", Link: "https://www.linkedin.com/in/riya-sharma", Club: ""}
}

func TestTheKurtKeutzerEmailIsRejected(t *testing.T) {
	// The real draft that went out for approval: a co-founder and Berkeley professor, a long message, ACM pushed
	// on someone who is not ACM, and claims the owner never made.
	c := OutreachContact{Name: "Kurt Keutzer", Title: "Co-founder; Professor, UC Berkeley", Company: "NexusFlow", Link: "https://www.linkedin.com/in/kurtkeutzer",
		Message: "Dear Professor Keutzer, I'm Adhyan Jain, a pre-final-year CSE student at VIT and a senior core member of ACM VIT, where I lead our systems track. " +
			"I've been following NexusFlow's work on agentic models and have written SGLang guides for our club, and my ComputeHive scheduler has run over 1,000 jobs. " +
			"I interned as an AI Engineer at Palni and won a track at the DevSoc hackathon. I would be grateful for a referral for the Software Engineer Intern role, " +
			"or any advice on how to stand out. I'd be happy to share my resume. Thank you so much for your time and consideration, and no worries at all if it's not possible."}
	problems := CheckOutreach(c, ownerFacts)
	joined := strings.Join(problems, " | ")
	for _, want := range []string{"senior", "ACM", "too long", "SGLang", "1,000"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the Keutzer draft must be rejected for %q; got: %s", want, joined)
		}
	}
}

func TestAShortGroundedMessageToAnEngineerPasses(t *testing.T) {
	c := engineer()
	c.Message = "Hi Riya, I'm Adhyan, a pre-final-year CSE student at VIT. I'm applying for the SDE Intern role at Razorpay. " +
		"I built ComputeHive, a distributed task runner in Go with Redis and Docker, and interned as an AI Engineer at Palni. Would you be open to referring me? Happy to share my resume."
	if p := CheckOutreach(c, ownerFacts); len(p) != 0 {
		t.Fatalf("a short, factual message to an engineer must pass: %v", p)
	}
}

func TestSeniorPeopleAreOnlyForStartups(t *testing.T) {
	for _, title := range []string{"Co-Founder & CEO", "CTO", "VP Engineering", "Director of Engineering", "Head of AI", "Professor", "Principal Engineer", "Founder"} {
		if !IsSeniorTitle(title) {
			t.Errorf("%q is senior", title)
		}
	}
	for _, title := range []string{"Software Engineer", "SDE-2", "Senior Software Engineer", "ML Engineer", "Engineering Manager", "Backend Developer"} {
		if IsSeniorTitle(title) {
			t.Errorf("%q is not senior: message them", title)
		}
	}
	c := engineer()
	c.Title, c.Message = "Co-founder & CTO", "Hi Riya, I'm Adhyan, a CSE student at VIT. Would you be open to referring me for the SDE Intern role?"
	if p := CheckOutreach(c, ownerFacts); !strings.Contains(strings.Join(p, " "), "senior") {
		t.Fatalf("a CTO at a non-startup is refused: %v", p)
	}
	c.Startup = true
	if p := CheckOutreach(c, ownerFacts); len(p) != 0 {
		t.Fatalf("at a startup the founders are the right people: %v", p)
	}
}

func TestACMIsMentionedOnlyToACMPeople(t *testing.T) {
	c := engineer()
	c.Message = "Hi Riya, I'm Adhyan, a CSE student at VIT and a senior core member of ACM VIT. Would you be open to referring me for the SDE Intern role?"
	if p := CheckOutreach(c, ownerFacts); !strings.Contains(strings.Join(p, " "), "ACM") {
		t.Fatalf("ACM is not mentioned to someone outside ACM: %v", p)
	}
	c.Club = "ACM VIT"
	if p := CheckOutreach(c, ownerFacts); len(p) != 0 {
		t.Fatalf("to an ACM VIT alum it is the best opener: %v", p)
	}
}

func TestAContactNeedsAVerifiedProfileLinkAndAClearAsk(t *testing.T) {
	c := engineer()
	c.Link = ""
	c.Message = "Hi Riya, I'm Adhyan, a CSE student at VIT. Would you be open to referring me for the SDE Intern role?"
	if p := CheckOutreach(c, ownerFacts); !strings.Contains(strings.Join(p, " "), "profile link") {
		t.Fatalf("no link, no message: %v", p)
	}
	c = engineer()
	c.Message = "Hi Riya, I'm Adhyan, a CSE student at VIT. I built ComputeHive."
	if p := CheckOutreach(c, ownerFacts); !strings.Contains(strings.Join(p, " "), "ask") {
		t.Fatalf("a message must ask for something: %v", p)
	}
}

func TestTheAlumniMessageNamesACMOnlyForACMPeopleAndIsShort(t *testing.T) {
	plain := AlumniMessageFor("Riya", "Razorpay", false)
	acm := AlumniMessageFor("Riya", "Razorpay", true)
	if strings.Contains(plain, "ACM") || !strings.Contains(acm, "ACM") {
		t.Fatalf("ACM only for ACM people:\n%s\n---\n%s", plain, acm)
	}
	for _, m := range []string{plain, acm} {
		c := engineer()
		c.Message = m
		if strings.Contains(m, "ACM") {
			c.Club = "ACM VIT"
		}
		if p := CheckOutreach(c, ownerFacts); len(p) != 0 {
			t.Errorf("the owner's own template must pass the rules: %v\n%s", p, m)
		}
	}
}

func TestTheSendGoalIsBuiltByCodeFromTheCheckedMessage(t *testing.T) {
	c := engineer()
	c.Message = "Hi Riya, would you be open to referring me for the SDE Intern role?"
	goal := OutreachSendGoal(c, "")
	if !strings.Contains(goal, c.Link) || !strings.Contains(goal, c.Message) || !strings.Contains(goal, "nobody else") {
		t.Fatalf("goal: %s", goal)
	}
}

func TestTheOwnersRealProfileWordingBacksTheTemplate(t *testing.T) {
	// How the owner's Library profile actually words things.
	profile := "- Full name: Adhyan Jain\n- Education: B.Tech in Computer Science and Engineering, Vellore Institute of Technology (VIT)\n- Expected graduation: 2028\n" +
		"- [computehive] ComputeHive: distributed task runner in Go, Redis, Docker\n- [palni] AI Engineer Intern, Palni\n" +
		"- [acm-recruitment] ACM - VIT Student Chapter (Senior Core Member)"
	for _, acm := range []bool{false, true} {
		c := engineer()
		c.Message = AlumniMessageFor("Riya", "Razorpay", acm)
		if acm {
			c.Club = "ACM VIT"
		}
		if p := CheckOutreach(c, profile); len(p) != 0 {
			t.Errorf("acm=%v: the template must pass against the real profile wording: %v", acm, p)
		}
	}
}
