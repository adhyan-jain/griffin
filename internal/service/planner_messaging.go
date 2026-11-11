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
	whatsAppWord         = regexp.MustCompile(`\b(?:whats\s?app|watsapp|wapp|watsp|whatsap|whatsapp)\b`)
	instaWord            = regexp.MustCompile(`\b(insta|instagram|ig)\b`)
	dmWord               = regexp.MustCompile(`\b(dm|dms|direct message)\b`)
	messagingVerb        = regexp.MustCompile(`\b(send|sned|snd|text|txt|message|msg|tell|ping|reply|respond|read|check|show|list|summari[sz]e|unread|chats?|inbox|dm|forward|fwd)\b`)
	buildingVerb         = regexp.MustCompile(`\b(code|coding|bot|bots|api|apis|webhook\w*|sdk|build|develop\w*|implement\w*|integrat\w*|librar(y|ies)|script|scrape\w*|automation framework)\b`)
	whatsAppLead         = regexp.MustCompile(`(?i)(?:^|\b(?:can|could|would|will)\s+you\s+(?:please\s+)?|\b(?:please|lets|let's|go ahead and)\s+|\bto\s+)(?:whats\s?app|watsapp|wapp|watsp|whatsap|whatsapp)\b\s*(.*)$`)
	contactStop          = map[string]bool{"on": true, "in": true, "via": true, "using": true, "through": true, "that": true, "about": true, "saying": true, "sayin": true, "with": true, "and": true, "a": true, "an": true, "the": true, "whatsapp": true, "instagram": true, "insta": true, "ig": true, "i": true, "i'm": true, "i'll": true, "im": true, "ill": true, "we": true, "to": true, "my": true, "from": true, "if": true, "when": true, "please": true, "message": true, "msg": true, "dm": true, "now": true, "phone": true, "laptop": true, "hello": true, "hellooo": true, "hi": true, "hey": true, "wassup": true, "sup": true, "yo": true, "me": true, "us": true, "myself": true, "yourself": true, "someone": true, "anyone": true, "number": true, "no": true, "num": true}
	contactAfter         = regexp.MustCompile(`(?i)\b(?:to|tell|text|txt|message|msg|ping|reply to|respond to|dm|send to|sned to|snd to|send|sned|snd)\s+(?:my\s+)?([^\n,.:;!?]+)`)
	contactAfterWhatsApp = regexp.MustCompile(`(?i)(?:^|\b(?:please|can you(?: please)?|could you(?: please)?|would you(?: please)?|i want you to|i need you to|let's|lets|go ahead and)\s+)(?:whats\s?app|watsapp|wapp|watsp|whatsap|whatsapp)\s+(?:my\s+)?([^\n,.:;!?]+)`)
	nonVerbAfterWhatsApp = map[string]bool{
		"is": true, "are": true, "was": true, "were": true, "has": true, "have": true, "had": true,
		"does": true, "did": true, "do": true, "will": true, "would": true, "can": true, "could": true,
		"web": true, "app": true, "status": true, "account": true,
	}
	emailWord       = regexp.MustCompile(`\b(?:email|emails|mail|mails|gmail|outlook)\b`)
	deliverableWord = regexp.MustCompile(`\b(?:reports?|tables?|documents?|proposals?|spreadsheets?|presentations?|slide deck|whitepaper|dossier|essay|article|brief|spec|readme|program|plan|comparison|sources|citations|references|roadmap|checklist)\b`)
	checkUnreadRe   = regexp.MustCompile(`(?i)\b(?:check|read|show|list)\s+(?:my\s+)?(?:unread\s+)?(?:messages|msgs|texts|chats|inbox)\b`)
)

func wantsWhatsApp(text string) bool {
	lower := strings.ToLower(text)
	if buildingVerb.MatchString(lower) || emailWord.MatchString(lower) {
		return false
	}
	if wantsInstagram(text) {
		return false
	}
	// Explicit WhatsApp mentions
	if whatsAppWord.MatchString(lower) && messagingVerb.MatchString(lower) {
		return true
	}
	if m := whatsAppLead.FindStringSubmatch(lower); len(m) > 1 {
		rest := strings.TrimSpace(m[1])
		if rest == "" {
			return false
		}
		fields := strings.Fields(rest)
		first := strings.Trim(fields[0], `"'?!.,;:`)
		if nonVerbAfterWhatsApp[first] {
			return false
		}
		if strings.HasPrefix(rest, "to ") || strings.Contains(rest, " to ") {
			return true
		}
		return true
	}
	// Implicit messaging: when the user asks to message/text/msg/ping/tell a contact without
	// specifying a platform (e.g. "message bhai wassup", "text bhai wassup", "tell bhai I'm late"),
	// WhatsApp is the user's primary daily messaging hand.
	if isImplicitMessaging(lower) {
		return true
	}
	return false
}

func isImplicitMessaging(lower string) bool {
	if buildingVerb.MatchString(lower) || emailWord.MatchString(lower) || deliverableWord.MatchString(lower) {
		return false
	}
	if wantsWeb(lower) {
		return false
	}
	if checkUnreadRe.MatchString(lower) {
		return true
	}
	if messagingVerb.MatchString(lower) && intentContactFrom(lower) != "" {
		return true
	}
	return false
}

func wantsInstagram(text string) bool {
	lower := strings.ToLower(text)
	if buildingVerb.MatchString(lower) {
		return false
	}
	if instaWord.MatchString(lower) && (messagingVerb.MatchString(lower) || dmWord.MatchString(lower)) {
		return true
	}
	if dmWord.MatchString(lower) {
		return true
	}
	return false
}

func extractContactWords(s string) string {
	var words []string
	for _, w := range strings.Fields(s) {
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
		cand := strings.Join(words, " ")
		lowCand := strings.ToLower(cand)
		if contactStop[lowCand] || lowCand == "me" || lowCand == "us" {
			return ""
		}
		return cand
	}
	return ""
}

var (
	toContactRe  = regexp.MustCompile(`(?i)\bto\s+(?:my\s+)?([^\n,.:;!?]+)`)
	directVerbRe = regexp.MustCompile(`(?i)\b(?:tell|text|txt|message|msg|ping|reply to|respond to|dm)\s+(?:my\s+)?([^\n,.:;!?]+)`)
	sendDirectRe = regexp.MustCompile(`(?i)\b(?:send|sned|snd)\s+(?:my\s+)?([^\n,.:;!?]+)`)
)

// intentContactFrom is the person the owner's own words name, found after "to", "tell", "text" and
// similar. It is recorded on the plan so an action an agent composes can be matched against what the
// owner said. When unsure it returns "", and the send then asks for approval, which is the safe side.
func intentContactFrom(text string) string {
	// 1. "to <person>": e.g. "Send a WhatsApp message to Sam Lee", "whatsapp to bhai", "send text to mom"
	for _, m := range toContactRe.FindAllStringSubmatch(text, -1) {
		if c := extractContactWords(m[1]); c != "" {
			return c
		}
	}

	// 2. "whatsapp <person>": e.g. "whatsapp bhai message wassup", "whatsapp Sam Lee saying test 123"
	for _, m := range contactAfterWhatsApp.FindAllStringSubmatch(text, -1) {
		if c := extractContactWords(m[1]); c != "" {
			return c
		}
	}

	// 3. Direct verbs: "message bhai wassup", "text bhai wassup", "tell dad I'll be late", "msg bhai hello", "ping bhai"
	for _, m := range directVerbRe.FindAllStringSubmatch(text, -1) {
		if c := extractContactWords(m[1]); c != "" {
			return c
		}
	}

	// 4. "send/sned/snd <person> <text>": e.g. "send bhai wassup", "sned bhai hello"
	for _, m := range sendDirectRe.FindAllStringSubmatch(text, -1) {
		if c := extractContactWords(m[1]); c != "" {
			return c
		}
	}

	return ""
}

const composePrompt = `You turn a request into ONE messaging action for a program to carry out.
Reply with ONLY a JSON object and nothing else.

Fields:
  "channel": %q
  "op":      "send", "read_recent" or "unread"
  "contact": the person's name or username exactly as the owner wrote it (leave out if a phone number is given)
  "phone":   digits with the country code, only if the owner gave a number (for WhatsApp only)
  "text":    for send only: the message, in the owner's voice, saying only what they asked for
  "limit":   for read_recent only: how many recent messages
  "prefer":  "phone" (default for mobile/messaging apps, unless the owner explicitly asked to use laptop)

Never invent a recipient or a number. If you cannot tell who to message, reply {"error":"no recipient"}.

Request: %q`

func messagingSteps(channel string, req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	do := map[string]any{"requires": []string{domain.HostCapMessaging}}
	if c := intentContactFrom(req.RequestText); c != "" {
		do["intent_contact"] = c
	}
	doPayload, _ := json.Marshal(do)
	composePayload := domain.WithExplicitProviderHint(req.Context, domain.ProviderAgy)
	prefix := "WhatsApp"
	composeKey := "wa_compose"
	doKey := "wa_do"
	if channel == domain.MessagingChannelInstagram {
		prefix = "Instagram"
		composeKey = "insta_compose"
		doKey = "insta_do"
	}
	steps := []domain.ChildTaskPlan{
		{
			Key:           composeKey,
			Title:         fmt.Sprintf("Work out the %s action", prefix),
			Goal:          fmt.Sprintf(composePrompt, channel, req.RequestText),
			ManagerDomain: "personal_ops",
			InputPayload:  composePayload,
			Priority:      req.Priority,
		},
		{
			Key:           doKey,
			Title:         truncateString(prefix+": "+req.RequestText, 60),
			Goal:          req.RequestText,
			ManagerDomain: "personal_ops",
			InputPayload:  doPayload,
			Priority:      req.Priority,
		},
	}
	return steps, []domain.PlanDependency{{TaskKey: doKey, DependsOnKey: composeKey}}
}

// whatsAppSteps plans a WhatsApp job as two steps: an agent turns the sentence into a strict action, and a
// step that needs the owner's device carries it out. The second step is where permissions and approval
// apply, not the first.
func whatsAppSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	return messagingSteps(domain.MessagingChannelWhatsApp, req)
}

func instagramSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	return messagingSteps(domain.MessagingChannelInstagram, req)
}
