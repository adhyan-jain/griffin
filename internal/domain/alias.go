package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// An alias is a name the owner uses for someone or something ("mummy" for "mom"). It is stored lower-cased and the
// server rewrites it to its target in what the owner says, so every client understands it.

var aliasWords = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} '.-]*$`)

// NormalizeAlias lower-cases, trims and validates an alias or its target.
func NormalizeAlias(s string, max int) (string, error) {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	switch {
	case s == "":
		return "", fmt.Errorf("%w: a name is required", ErrValidation)
	case len([]rune(s)) > max:
		return "", fmt.Errorf("%w: a name must be at most %d characters", ErrValidation, max)
	case !aliasWords.MatchString(s):
		return "", fmt.Errorf("%w: a name may only hold letters, numbers, spaces and . ' -", ErrValidation)
	}
	return s, nil
}

var aliasToken = regexp.MustCompile(`[\p{L}\p{N}]+(?:['.-][\p{L}\p{N}]+)*`)

// ApplyAliases replaces each alias in text with its target. It matches whole words only, ignores case, and the
// longest alias wins ("bade bhai" before "bhai"). Text that was just inserted is never scanned again, so an alias
// whose target contains another alias cannot loop.
func ApplyAliases(text string, aliases map[string]string) string {
	if len(aliases) == 0 {
		return text
	}
	maxWords := 1
	for k := range aliases {
		if n := len(strings.Fields(k)); n > maxWords {
			maxWords = n
		}
	}
	toks := aliasToken.FindAllStringIndex(text, -1)
	var b strings.Builder
	last := 0
	for i := 0; i < len(toks); {
		replaced := false
		for n := min(maxWords, len(toks)-i); n >= 1 && !replaced; n-- {
			span := text[toks[i][0]:toks[i+n-1][1]]
			// A span of several words must be separated by single spaces to count as one alias.
			if n > 1 && strings.Join(strings.Fields(span), " ") != span {
				continue
			}
			if t, ok := aliases[strings.ToLower(span)]; ok {
				b.WriteString(text[last:toks[i][0]])
				b.WriteString(t)
				last = toks[i+n-1][1]
				i += n
				replaced = true
			}
		}
		if !replaced {
			i++
		}
	}
	b.WriteString(text[last:])
	return b.String()
}
