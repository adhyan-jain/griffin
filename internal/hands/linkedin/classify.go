package linkedin

import (
	"context"
	"strings"

	"github.com/griffin/core/internal/provider/gemini"
)

const classifySystem = `You screen LinkedIn posts for a job seeker. Answer with exactly one word: YES or NO.
YES only when the author is genuinely offering or announcing a real job or internship (their own company or team is hiring, or they are sharing a specific opening in earnest).
NO when the author is joking about, mocking, criticising or complaining about a job post or hiring practices, replying to or commenting on someone else's post, asking for work themselves, giving career advice, selling a course or service, or when no concrete role is offered.`

// Gemini is a Classifier backed by the Gemini API.
type Gemini struct{ Client *gemini.Client }

// IsRealJob implements Classifier. Any answer other than YES counts as no.
func (g Gemini) IsRealJob(ctx context.Context, author, text string) (bool, error) {
	if len(text) > 4000 {
		text = text[:4000]
	}
	reply, err := g.Client.Chat(ctx, classifySystem, []gemini.Turn{{Role: "user", Text: "Author: " + author + "\n\nPost:\n" + text}})
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reply)), "YES"), nil
}
