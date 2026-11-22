package sources

import (
	"context"
	"strings"
	"testing"
)

func TestHackerNewsThreadTextIsItsBodyAndTopCommentsWithoutMarkup(t *testing.T) {
	var asked string
	get := func(_ context.Context, u string) ([]byte, error) {
		asked = u
		return []byte(`{"title":"Ask HN: agent framework?","text":"<p>I&#x27;m after a small one.<p>Any pointers?","children":[{"text":"Try <a href=\"x\">this</a> one"},{"text":""},{"text":"Second answer"}]}`), nil
	}
	text, err := ReadThreadText(context.Background(), get, Thread{Platform: HackerNews, Title: "Ask HN: agent framework?", URL: hnItem + "50589077"})
	if err != nil || asked != "https://hn.algolia.com/api/v1/items/50589077" {
		t.Fatalf("the item API is read: %v %s", err, asked)
	}
	for _, want := range []string{"Ask HN: agent framework?", "I'm after a small one. Any pointers?", "Top comments:", "- Try this one", "- Second answer"} {
		if !strings.Contains(text, want) {
			t.Errorf("thread text lacks %q:\n%s", want, text)
		}
	}
}

func TestRedditThreadTextFallsBackToTheTitleWhenReditIsBlocked(t *testing.T) {
	th := Thread{Platform: Reddit, Title: "How do I queue jobs?", Text: "listing snippet", URL: "https://www.reddit.com/r/golang/comments/1/q/"}
	ok := func(_ context.Context, u string) ([]byte, error) {
		if u != "https://www.reddit.com/r/golang/comments/1/q.json" {
			t.Fatalf("the thread's own .json is read: %s", u)
		}
		return []byte(`[{"data":{"children":[{"data":{"selftext":"the full post body"}}]}},{"data":{"children":[]}}]`), nil
	}
	if text, err := ReadThreadText(context.Background(), ok, th); err != nil || !strings.Contains(text, "the full post body") {
		t.Fatalf("selftext is used: %v %q", err, text)
	}
	blocked := func(_ context.Context, u string) ([]byte, error) { return nil, &StatusError{URL: u, Code: 403} }
	text, err := ReadThreadText(context.Background(), blocked, th)
	if err == nil || text != "How do I queue jobs?\nlisting snippet" {
		t.Fatalf("blocked: the title and the listing's snippet, and the error says why: %v %q", err, text)
	}
	html := func(context.Context, string) ([]byte, error) { return []byte("<html>wall</html>"), nil }
	if text, err = ReadThreadText(context.Background(), html, th); err == nil || !strings.HasPrefix(text, "How do I queue jobs?") {
		t.Fatalf("an HTML wall is an error with the fallback text: %v %q", err, text)
	}
}
