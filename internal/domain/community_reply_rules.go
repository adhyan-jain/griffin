package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Community replies: the short answers written for Reddit and Hacker News threads. They reuse the outreach
// checker's number and name logic (numberToken, unknownNames), so a reply can only state what the thread or the
// owner's own profile states.
const (
	MinCommunityReplyWords = 40
	MaxCommunityReplyWords = 90
)

var (
	hashtagToken       = regexp.MustCompile(`(^|\s)#\w`)
	linkToken          = regexp.MustCompile(`(?i)https?://|www\.`)
	experienceClaim    = regexp.MustCompile(`(?i)\b(at (my|our) (job|company|work|startup|office|team)|my (company|employer|boss|manager)|years? of experience|in production at|when i worked)\b`)
	projectMentionRe   = regexp.MustCompile(`(?i)computehive|\bmy (own |side )?(project|projects|agent|agents|app)\b|\bi (built|made|wrote|created|am building|'m building|’m building|have been building)\b|\bwe built\b`)
	replySentenceSplit = regexp.MustCompile(`[.!?]+\s+|\n`)
)

// CheckCommunityReply returns every reason a drafted reply to a public thread must not be written into the
// tracker; none means it may. threadText is everything the model was shown of the thread (title, body, comments)
// and ownerFacts the owner's verified profile: every number and every name or product in the reply must come from
// one of them, so nothing is claimed or invented that neither source says.
func CheckCommunityReply(reply, threadText, ownerFacts string) []string {
	var problems []string
	reply = strings.TrimSpace(reply)
	if strings.TrimSpace(ownerFacts) == "" {
		return []string{"no verified owner profile to check the reply against"}
	}
	if reply == "" {
		return []string{"the reply is empty"}
	}
	if n := len(strings.Fields(reply)); n < MinCommunityReplyWords || n > MaxCommunityReplyWords {
		problems = append(problems, fmt.Sprintf("%d words (must be %d to %d)", n, MinCommunityReplyWords, MaxCommunityReplyWords))
	}
	if linkToken.MatchString(reply) {
		problems = append(problems, "contains a link")
	}
	if hashtagToken.MatchString(reply) {
		problems = append(problems, "contains a hashtag")
	}
	for _, r := range reply {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F {
			problems = append(problems, "contains an emoji")
			break
		}
	}
	if m := experienceClaim.FindString(reply); m != "" {
		problems = append(problems, "claims experience that is not in your profile: \""+m+"\"")
	}
	known := strings.ToLower(threadText + " " + ownerFacts)
	knownDigits := numberToken.FindAllString(known, -1)
	for _, num := range numberToken.FindAllString(reply, -1) {
		num = strings.TrimRight(num, ".,")
		if containsString(knownDigits, num) {
			continue
		}
		problems = append(problems, "number not in the thread or your profile: \""+num+"\"")
	}
	for _, w := range unknownNames(reply, known) {
		problems = append(problems, "name not in the thread or your profile: \""+w+"\"")
	}
	return problems
}

// CheckCommunityMention enforces the row's "Mention a project?" flag: with it unset the reply must not talk about
// the owner's own work at all; with it set, at most one sentence may.
func CheckCommunityMention(reply string, mentionAllowed bool) []string {
	n := 0
	for _, s := range replySentenceSplit.Split(reply, -1) {
		if projectMentionRe.MatchString(s) {
			n++
		}
	}
	switch {
	case n > 0 && !mentionAllowed:
		return []string{"promotes a project on a thread that is not flagged for a mention"}
	case n > 1:
		return []string{"mentions a project in more than one sentence"}
	}
	return nil
}
