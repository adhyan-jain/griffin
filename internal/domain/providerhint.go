package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ProviderHint names the AI provider a plan step is best suited to. It is chosen by what the step
// needs, not by how hard it is: work that touches the outside world belongs on the machine-side
// provider (agy); pure language work can go to Claude when that is enabled.
type ProviderHint string

const (
	ProviderAgy    ProviderHint = "agy"
	ProviderClaude ProviderHint = "claude"

	// ProviderHintKey is where the hint lives inside a task's input_payload JSON (no schema change).
	ProviderHintKey = "provider_hint"
)

var (
	// Work that needs the machine or the outside world.
	needsMachine = regexp.MustCompile(`(?i)\b(browse|browser|website|web|search|scrape|crawl|download|upload|log ?in|sign ?in|apply|submit|fill|form|click|install|run|execute|script|shell|terminal|command|git|repo|repository|clone|commit|deploy|build|test|file|files|folder|directory|spreadsheet|csv|whatsapp|telegram|slack|email|inbox|calendar|screenshot|api|database|linkedin|monitor|poll|watch|schedule)\b`)
	// Work that is purely language: reading what it is given and producing text.
	pureLanguage = regexp.MustCompile(`(?i)\b(write|draft|summari[sz]e|summary|analy[sz]e|analysis|review|compare|comparison|evaluate|classify|rank|rewrite|edit|proofread|explain|outline|brainstorm|translate|recommend|critique|assess|decide|synthesi[sz]e|polish|tone|tailor|resume|resumes|cv|cvs|cover\s*letters?|curriculum\s*vitae)\b`)

	// Resume and CV tailoring/formatting is deep language work best suited for Claude even if surrounding text mentions applications.
	resumeWork    = regexp.MustCompile(`(?i)\b(resume|resumes|cv|cvs|cover\s*letters?|curriculum\s*vitae)\b`)
	resumeAction  = regexp.MustCompile(`(?i)\b(tailor|draft|write|format|polish|keyword|keywords|align|edit|rewrite|compose|create)\b`)
	machineAction = regexp.MustCompile(`(?i)\b(submit|send|fill\s+form|upload|click|crawl|scrape|browse)\b`)
)

func isResumeTailoring(text string) bool {
	return resumeWork.MatchString(text) && resumeAction.MatchString(text) && !machineAction.MatchString(text)
}

// ClassifyProvider picks the provider a step is best suited to. Anything that touches the outside
// world stays on agy even if it also writes text; only clearly language-only steps are planned for
// Claude. An unclear step defaults to agy, the provider that is always available.
func ClassifyProvider(title, goal string) ProviderHint {
	text := title + " " + goal
	if isResumeTailoring(text) {
		return ProviderClaude
	}
	if needsMachine.MatchString(text) {
		return ProviderAgy
	}
	if pureLanguage.MatchString(text) {
		return ProviderClaude
	}
	return ProviderAgy
}

// WithExplicitProviderHint sets or overrides the provider hint in a payload.
func WithExplicitProviderHint(payload json.RawMessage, hint ProviderHint) json.RawMessage {
	obj := map[string]json.RawMessage{}
	if s := strings.TrimSpace(string(payload)); s != "" && s != "null" {
		_ = json.Unmarshal(payload, &obj)
	}
	h, _ := json.Marshal(hint)
	obj[ProviderHintKey] = h
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

// WithProviderHint records the planned provider in a step's input payload so the choice is visible
// and stable (a retry goes to the same provider). An existing hint is respected. A payload that is
// not a JSON object is returned unchanged rather than corrupted.
func WithProviderHint(payload json.RawMessage, title, goal string) json.RawMessage {
	obj := map[string]json.RawMessage{}
	if s := strings.TrimSpace(string(payload)); s != "" && s != "null" {
		if err := json.Unmarshal(payload, &obj); err != nil {
			return payload
		}
	}
	if _, ok := obj[ProviderHintKey]; ok {
		return payload
	}
	h, _ := json.Marshal(ClassifyProvider(title, goal))
	obj[ProviderHintKey] = h
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

type providerPayloadView struct {
	Kind string       `json:"kind"`
	Hint ProviderHint `json:"provider_hint"`
}

// ProviderHintFor returns the provider a task is planned for. Chat always uses agy (its prompt states
// the runtime facts of that provider). An explicit hint wins; otherwise the step is classified.
func ProviderHintFor(task *Task) ProviderHint {
	var v providerPayloadView
	if len(task.InputPayload) > 0 {
		_ = json.Unmarshal(task.InputPayload, &v)
	}
	if v.Kind == "chat" {
		return ProviderAgy
	}
	if v.Hint == ProviderAgy || v.Hint == ProviderClaude {
		return v.Hint
	}
	return ClassifyProvider(task.Title, task.Goal)
}
