package domain

import (
	"regexp"
	"strings"
)

// HabitProfile models the owner's personal communication patterns, colloquial lingo,
// alias mappings, and channel defaults.
type HabitProfile struct {
	// ContactAliases maps colloquial terms (e.g., "bhai", "bro", "mom", "dad") to contact records or tags.
	ContactAliases map[string]string `json:"contact_aliases"`
	// DefaultChannel is the fallback channel for interpersonal communication when unspecified.
	DefaultChannel string `json:"default_channel"`
	// ShorthandMap maps common mobile typing slips, slang, and phonetic shorthands to canonical terms.
	ShorthandMap map[string]string `json:"shorthand_map"`
}

var (
	// wordBoundaryRe matches single word tokens for shorthand replacement.
	wordBoundaryRe = regexp.MustCompile(`\b[a-zA-Z0-9']+\b`)
)

// DefaultHabitProfile returns the built-in habit profile reflecting the owner's conventions.
func DefaultHabitProfile() *HabitProfile {
	return &HabitProfile{
		ContactAliases: map[string]string{
			"bhai":    "bhai",
			"bro":     "brother",
			"brother": "brother",
			"mom":     "mom",
			"maa":     "mom",
			"dad":     "dad",
			"papa":    "dad",
		},
		DefaultChannel: MessagingChannelWhatsApp,
		ShorthandMap: map[string]string{
			"sned":    "send",
			"snd":     "send",
			"msg":     "message",
			"txt":     "text",
			"fwd":     "forward",
			"watsapp": "whatsapp",
			"wapp":    "whatsapp",
			"watsp":   "whatsapp",
			"whatsap": "whatsapp",
			"wassup":  "wassup",
			"sup":     "wassup",
			"yo":      "wassup",
			"hellooo": "hello",
		},
	}
}

// NormalizeLingo standardizes known typing slips and mobile shorthands in a request.
func (p *HabitProfile) NormalizeLingo(text string) string {
	if p == nil || len(p.ShorthandMap) == 0 {
		return text
	}
	return wordBoundaryRe.ReplaceAllStringFunc(text, func(w string) string {
		lower := strings.ToLower(w)
		if repl, ok := p.ShorthandMap[lower]; ok {
			// Keep original casing for exact match if it was uppercase or title
			if w == strings.ToUpper(w) && len(w) > 1 {
				return strings.ToUpper(repl)
			}
			if len(w) > 0 && unicodeIsUpper(rune(w[0])) {
				return strings.Title(repl)
			}
			return repl
		}
		return w
	})
}

func unicodeIsUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

// ResolveContact resolves a contact alias (e.g. "bhai") to its target name or returns the original.
func (p *HabitProfile) ResolveContact(name string) string {
	if p == nil || len(p.ContactAliases) == 0 {
		return name
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if target, ok := p.ContactAliases[lower]; ok {
		return target
	}
	return name
}
