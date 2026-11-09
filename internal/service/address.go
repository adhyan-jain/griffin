package service

import (
	"regexp"
	"strings"
)

// StripLeadingName removes the manager's name when the owner opens with it ("JJ wassup", "hey Jarvis,
// plan my week"), so the rest is classified and answered as the request. The name is whatever the owner
// chose, so the default "jj" in the classifier's own filler list is not enough. Names are matched whole
// and case-insensitively; a name that merely starts a longer word ("Jarvisly") is left alone.
func StripLeadingName(text, name string) string {
	text = strings.TrimSpace(text)
	name = strings.TrimSpace(name)
	if name == "" {
		return text
	}
	re := regexp.MustCompile(`(?i)^(?:(?:hey|hi|hello|yo|ok|okay)[\s,]+)?` + regexp.QuoteMeta(name) + `(?:[\s,:;!.-]+|$)`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}
