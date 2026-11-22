package assess

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// ---- skills -----------------------------------------------------------------------------------------------------

type skillDef struct {
	Name string
	re   *regexp.Regexp
}

// vocab is every technology Griffin recognises in a posting. Whether a skill counts as the owner's is a separate
// question (OwnerSkills): a posting asking for Rust is read, but Rust is not matched unless the owner's profile has it.
var vocab = []skillDef{
	{"Go", regexp.MustCompile(`(?i:golang)|(?:\b(?:in|with|using|write|writing|build|building|and|or|of|experience|knowledge|proficiency)\s|[,/(])\s*Go\b(?:\s*(?:and|or|,|/|\)|\.|services?|programming|language|developers?|engineers?|microservices?|backend|code|concurrency|routines?)|$)|\bGo (?:programming|language|developers?|engineers?|services?|microservices?)\b`)},
	{"Python", regexp.MustCompile(`(?i)\bpython\b`)},
	{"JavaScript", regexp.MustCompile(`(?i)\bjavascript\b`)},
	{"TypeScript", regexp.MustCompile(`(?i)\btypescript\b`)},
	{"React", regexp.MustCompile(`\bReact(?:\.js|JS)?\b`)},
	{"Next.js", regexp.MustCompile(`(?i)\bnext\.?js\b`)},
	{"Node.js", regexp.MustCompile(`\bNode\.?js\b|\bNodeJS\b|\bNode (?:developer|backend)\b`)},
	{"PostgreSQL", regexp.MustCompile(`(?i)\bpostgres(?:ql)?\b`)},
	{"MongoDB", regexp.MustCompile(`(?i)\bmongo(?:db)?\b`)},
	{"Redis", regexp.MustCompile(`(?i)\bredis\b`)},
	{"Docker", regexp.MustCompile(`(?i)\bdocker\b`)},
	{"gRPC", regexp.MustCompile(`(?i)\bgrpc\b`)},
	{"AWS", regexp.MustCompile(`\bAWS\b|(?i:amazon web services)`)},
	{"LLMs", regexp.MustCompile(`(?i)\bllms?\b|large language models?`)},
	{"RAG", regexp.MustCompile(`\bRAG\b|(?i:retrieval[- ]augmented)`)},
	{"AI agents", regexp.MustCompile(`(?i)\b(?:ai|llm) agents?\b|\bagentic\b`)},
	{"Distributed systems", regexp.MustCompile(`(?i)\bdistributed systems?\b`)},
	{"ML systems and inference", regexp.MustCompile(`(?i)\binference\b|\bmodel serving\b|\bml systems?\b|\bmlops\b`)},
	// Read in postings; the owner's only when the owner's profile names them.
	{"Java", regexp.MustCompile(`(?i)\bjava\b`)},
	{"C++", regexp.MustCompile(`(?i)\bc\+\+`)},
	{"Rust", regexp.MustCompile(`\bRust\b`)},
	{"Kubernetes", regexp.MustCompile(`(?i)\bkubernetes\b|\bk8s\b`)},
	{"PyTorch", regexp.MustCompile(`(?i)\bpytorch\b`)},
	{"TensorFlow", regexp.MustCompile(`(?i)\btensorflow\b`)},
	{"Kafka", regexp.MustCompile(`(?i)\bkafka\b`)},
	{"GraphQL", regexp.MustCompile(`(?i)\bgraphql\b`)},
	{"SQL", regexp.MustCompile(`\bSQL\b`)},
	{"Flutter", regexp.MustCompile(`(?i)\bflutter\b`)},
	{"Terraform", regexp.MustCompile(`(?i)\bterraform\b`)},
}

// ownerDefaultSkills are the skills the owner stated for this assessor: backend and distributed systems, Go, Python,
// JavaScript and TypeScript, React, Next.js and Node, Postgres, Mongo and Redis, Docker, gRPC and AWS, and AI
// engineering (LLM apps, RAG, agents) and ML systems. Anything else counts only when the owner's profile text names it.
var ownerDefaultSkills = []string{
	"Go", "Python", "JavaScript", "TypeScript", "React", "Next.js", "Node.js", "PostgreSQL", "MongoDB", "Redis",
	"Docker", "gRPC", "AWS", "LLMs", "RAG", "AI agents", "Distributed systems", "ML systems and inference",
}

// OwnerSkills is the set of skills that count as the owner's: the stated defaults plus every vocabulary skill the
// owner's profile text names. ownerText may be empty.
func OwnerSkills(ownerText string) map[string]bool {
	out := map[string]bool{}
	for _, s := range ownerDefaultSkills {
		out[s] = true
	}
	text := norm(ownerText)
	for _, v := range vocab {
		if text != "" && v.re.MatchString(text) {
			out[v.Name] = true
		}
	}
	return out
}

// MatchSkills returns the owner's skills a posting asks for, each with the words that show it. The title counts as
// text, so a "Python Developer Intern" with no description still matches.
func MatchSkills(title, description string, owner map[string]bool) []domain.SkillMatch {
	text := norm(title + ". " + description)
	var out []domain.SkillMatch
	for _, v := range vocab {
		if !owner[v.Name] {
			continue
		}
		if q, ok := snip(v.re, text, 30); ok {
			out = append(out, domain.SkillMatch{Skill: v.Name, Quote: q})
		}
	}
	return out
}

// ---- engineering depth, mentorship, PPO -------------------------------------------------------------------------

// Depth levels.
const (
	DepthDeep     = "deep"
	DepthModerate = "moderate"
	DepthShallow  = "shallow"
	DepthUnknown  = "unknown"
)

// Signal is a conclusion with the words that support it.
type Signal struct {
	Level  string
	Reason string
	Quote  string
}

var engineeringTerms = regexp.MustCompile(`(?i)\b(distributed systems?|microservices?|scalab\w+|low[- ]latency|latency|throughput|concurren\w+|pipelines?|ci/cd|kubernetes|docker|terraform|observability|databases?|sql|postgres\w*|mongodb|redis|kafka|grpc|rest(?:ful)? apis?|apis?|back-?end|infrastructure|compilers?|inference|model (?:serving|training|deployment)|llms?|rag|embeddings?|architecture|system design|production|code reviews?|open[- ]source|unit tests?|optimi[sz]\w+|refactor\w*|debug\w*|algorithms?|data structures?|research|papers?|benchmark\w*|sdks?|frameworks?|protocols?|schemas?|migrations?|reliab\w+|platforms?|automation|at scale|systems|tooling|librar(?:y|ies)|availability)\b`)

var shallowTerms = regexp.MustCompile(`(?i)\b(lead generation|cold calling|data entry|social media|content writing|telecalling|customer support|sales|marketing|bpo|back office|spreadsheets?)\b`)

// DepthSignal reads how much real engineering a posting describes: how many distinct engineering terms it uses. A
// posting with too little text to judge is unknown, not shallow. The reason says how it was counted.
func DepthSignal(description string) Signal {
	text := norm(description)
	if len(text) < 150 {
		return Signal{Level: DepthUnknown, Reason: "The posting has too little text to judge the engineering depth"}
	}
	distinct := map[string]bool{}
	first := ""
	for _, m := range engineeringTerms.FindAllStringIndex(text, -1) {
		w := strings.ToLower(text[m[0]:m[1]])
		if len(distinct) == 0 {
			first = around(text, m[0], m[1], 30)
		}
		distinct[w] = true
	}
	n := len(distinct)
	switch {
	case n >= 6 && len(text) >= 500:
		return Signal{DepthDeep, fmt.Sprintf("The posting describes engineering work in detail (%d distinct engineering terms)", n), first}
	case n >= 3:
		return Signal{DepthModerate, fmt.Sprintf("The posting names some engineering work (%d distinct engineering terms)", n), first}
	}
	if q, ok := snip(shallowTerms, text, 30); ok {
		return Signal{DepthShallow, "The posting describes non-engineering or routine work and little engineering", q}
	}
	return Signal{DepthShallow, fmt.Sprintf("The posting has %d distinct engineering terms in %d characters: little engineering detail", n, len(text)), ""}
}

var (
	mentorRe = regexp.MustCompile(`(?i)\bmentor\w*|\bguidance (?:from|of) (?:senior|experienced|our)\b|\bwork(?:ing)? (?:closely )?with (?:senior|experienced|our) engineers\b|\bpair[- ]program\w*|\bbuddy\b|\bonboarding program\b|\bcode reviews? from\b`)
	ppoRe    = regexp.MustCompile(`(?i)\bpre-?placement offers?\b|\bppo\b|\bfull[- ]time (?:offer|conversion)\b|\bconver(?:t|sion)\w* (?:in)?to (?:a )?full[- ]time\b|\breturn offers?\b|\bpossibility of (?:a )?(?:full[- ]time|job offer)\b|\bjob offer (?:upon|on|after)\b`)
	noPPORe  = regexp.MustCompile(`(?i)\bno (?:pre-?placement offers?|ppo|full[- ]time (?:offer|conversion))\b|\b(?:not|does not|doesn't) (?:lead|convert) to (?:a )?full[- ]time\b`)
	hedgeRe  = regexp.MustCompile(`(?i)\b(based on|depending on|subject to|performance|may|might|possib\w+|potential|opportunity|eligible)\b`)
)

// MentorshipSignal reports whether a posting says the intern is mentored. Absence is unknown, not "no mentorship".
func MentorshipSignal(description string) Signal {
	text := norm(description)
	if q, ok := snip(mentorRe, text, 30); ok {
		return Signal{Level: "present", Reason: "The posting mentions mentorship or working with senior engineers", Quote: q}
	}
	return Signal{Level: "unknown", Reason: "The posting does not say"}
}

// PPOSignal reports whether a posting offers a full-time offer after the internship: yes, conditional (hedged
// words around it), no (it says so) or unknown.
func PPOSignal(description string) Signal {
	text := norm(description)
	if q, ok := snip(noPPORe, text, 30); ok {
		return Signal{Level: "no", Reason: "The posting says there is no full-time offer", Quote: q}
	}
	if loc := ppoRe.FindStringIndex(text); loc != nil {
		q := around(text, loc[0], loc[1], 50)
		if hedgeRe.MatchString(q) {
			return Signal{Level: "conditional", Reason: "The posting mentions a possible full-time offer", Quote: q}
		}
		return Signal{Level: "yes", Reason: "The posting mentions a full-time offer", Quote: q}
	}
	return Signal{Level: "unknown", Reason: "The posting does not say"}
}
