// Package assess reads one listing's text and concludes what kind of job it is, whether the owner may apply, what it
// pays, which of the owner's skills it asks for and how much it is worth to the owner's career. Deterministic stages
// run first; a model is asked only for what code cannot decide, and every model conclusion must carry a quote that
// code finds in the posting, or it is discarded and the field stays unknown. Nothing here guesses.
package assess

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// norm collapses every run of whitespace to one space, the form a posting is stored and quoted in.
func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// firstMatch returns the first match of re in text, or "" and false.
func firstMatch(re *regexp.Regexp, text string) (string, bool) {
	if m := re.FindString(text); m != "" {
		return m, true
	}
	return "", false
}

// around returns the words of text around the byte range [start, end), widened by about pad bytes on each side to
// word boundaries. The result is a substring of text, so it can be verified against it.
func around(text string, start, end, pad int) string {
	lo, hi := start-pad, end+pad
	if lo < 0 {
		lo = 0
	}
	if hi > len(text) {
		hi = len(text)
	}
	for lo > 0 && text[lo-1] != ' ' {
		lo--
	}
	for hi < len(text) && text[hi] != ' ' {
		hi++
	}
	return strings.TrimSpace(text[lo:hi])
}

// Quote limits: a quote shorter than minQuote proves nothing; one longer than maxQuote is a pasted paragraph.
const (
	minQuote = 6
	maxQuote = 400
)

// QuoteIn reports whether quote appears in text, ignoring differences in whitespace and letter case. This is the
// only test a model conclusion has to pass to be kept.
func QuoteIn(text, quote string) bool {
	q := norm(quote)
	if utf8.RuneCountInString(q) < minQuote || len(q) > maxQuote {
		return false
	}
	return strings.Contains(strings.ToLower(norm(text)), strings.ToLower(q))
}
