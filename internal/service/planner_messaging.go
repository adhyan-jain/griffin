package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// A request is a WhatsApp job when it names WhatsApp and asks to send, read or check messages. A request
// that is about building something with WhatsApp (a bot, an API integration) is an engineering job and
// must not send anything, so those words veto the match.
var (
	whatsAppWord  = regexp.MustCompile(`\bwhats\s?app\b`)
	messagingVerb = regexp.MustCompile(`\b(send|text|message|msg|tell|ping|reply|respond|read|check|show|list|summari[sz]e|unread|chats?|inbox)\b`)
	buildingVerb  = regexp.MustCompile(`\b(code|coding|bot|bots|api|apis|webhook\w*|sdk|build|develop\w*|implement\w*|integrat\w*|librar(y|ies)|script|scrape\w*|automation framework)\b`)
	contactStop   = map[string]bool{"on": true, "in": true, "via": true, "using": true, "through": true, "that": true, "about": true, "saying": true, "with": true, "and": true, "a": true, "an": true, "the": true, "whatsapp": true, "i": true, "i'm": true, "i'll": true, "im": true, "ill": true, "we": true, "to": true, "my": true, "from": true, "if": true, "when": true, "please": true, "message": true, "msg": true, "now": true, "phone": true, "laptop": true}
	contactAfter  = regexp.MustCompile(`(?i)\b(?:to|tell|text|message|msg|ping|reply to|respond to)\s+(?:my\s+)?([^\n,.:;!?]+)`)
)

func wantsWhatsApp(text string) bool {
	lower := strings.ToLower(text)
	return whatsAppWord.MatchString(lower) && messagingVerb.MatchString(lower) && !buildingVerb.MatchString(lower)
}

// intentContactFrom is the person the owner's own words name, found after "to", "tell", "text" and
// similar. It is recorded on the plan so an action an agent composes can be matched against what the
// owner said. When unsure it returns "", and the send then asks for approval, which is the safe side.
func intentContactFrom(text string) string {
	for _, m := range contactAfter.FindAllStringSubmatch(text, -1) {
		var words []string
		for _, w := range strings.Fields(m[1]) {
			w = strings.Trim(w, `"'`)
			low := strings.ToLower(w)
			if len(words) == 0 && (low == "to" || low == "my" || low == "a" || low == "an" || low == "the") {
				continue // filler before the name
			}
			if contactStop[low] {
				break
			}
			words = append(words, w)
			if len(words) == 3 {
				break
			}
		}
		if len(words) > 0 {
			return strings.Join(words, " ")
		}
	}
	return ""
}

const composePrompt = `You turn a request into ONE WhatsApp action for a program to carry out.
Reply with ONLY a JSON object and nothing else.

Fields:
  "op":      "send", "read_recent" or "unread"
  "contact": the person's name exactly as the owner wrote it (leave out if a phone number is given)
  "phone":   digits with the country code, only if the owner gave a number
  "text":    for send only: the message, in the owner's voice, saying only what they asked for
  "limit":   for read_recent only: how many recent messages
  "prefer":  "phone" only if the owner asked to use their phone, "laptop" only if they asked for the laptop

Never invent a recipient or a number. If you cannot tell who to message, reply {"error":"no recipient"}.

Request: %q`

// whatsAppSteps plans a WhatsApp job as two steps: an agent turns the sentence into a strict action, and a
// step that needs the owner's device carries it out. The second step is where permissions and approval
// apply, not the first.
func whatsAppSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	do := map[string]any{"requires": []string{domain.HostCapMessaging}}
	if c := intentContactFrom(req.RequestText); c != "" {
		do["intent_contact"] = c
	}
	doPayload, _ := json.Marshal(do)
	composePayload := req.Context
	if len(composePayload) == 0 {
		composePayload = json.RawMessage(`{}`)
	}
	steps := []domain.ChildTaskPlan{
		{
			Key:           "wa_compose",
			Title:         "Work out the WhatsApp action",
			Goal:          fmt.Sprintf(composePrompt, req.RequestText),
			ManagerDomain: "personal_ops",
			InputPayload:  composePayload,
			Priority:      req.Priority,
		},
		{
			Key:           "wa_do",
			Title:         truncateString("WhatsApp: "+req.RequestText, 60),
			Goal:          req.RequestText,
			ManagerDomain: "personal_ops",
			InputPayload:  doPayload,
			Priority:      req.Priority,
		},
	}
	return steps, []domain.PlanDependency{{TaskKey: "wa_do", DependsOnKey: "wa_compose"}}
}
