package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	hnItemAPI      = "https://hn.algolia.com/api/v1/items/"
	threadTextCap  = 3000
	threadCommentN = 5
	commentTextCap = 400
)

// ReadThreadText reads what a thread says, for writing a reply to it: its title, its body and (on Hacker News)
// its top comments, as plain text. It always returns at least the title and whatever the listing already carried;
// err says why the fuller read failed (Reddit often refuses automated reads), so a caller can stop asking.
func ReadThreadText(ctx context.Context, get Get, t Thread) (string, error) {
	if get == nil {
		get = CommunityGet
	}
	fallback := joinThreadText(t.Title, t.Text, nil)
	switch t.Platform {
	case HackerNews:
		i := strings.LastIndex(t.URL, "id=")
		if i < 0 {
			return fallback, fmt.Errorf("no Hacker News id in %s", t.URL)
		}
		body, err := get(ctx, hnItemAPI+t.URL[i+3:])
		if err != nil {
			return fallback, err
		}
		title, text, comments, err := parseHNItem(body)
		if err != nil {
			return fallback, err
		}
		if title == "" {
			title = t.Title
		}
		return joinThreadText(title, text, comments), nil
	case Reddit:
		body, err := get(ctx, strings.TrimRight(t.URL, "/")+".json")
		if err != nil {
			return fallback, err
		}
		text, err := parseRedditPost(body)
		if err != nil {
			return fallback, err
		}
		if text == "" {
			text = t.Text
		}
		return joinThreadText(t.Title, text, nil), nil
	}
	return fallback, nil
}

func parseHNItem(body []byte) (title, text string, comments []string, err error) {
	var r struct {
		Title     string `json:"title"`
		Text      string `json:"text"`
		StoryText string `json:"story_text"`
		Children  []struct {
			Text string `json:"text"`
		} `json:"children"`
	}
	if err = json.Unmarshal(body, &r); err != nil {
		return "", "", nil, fmt.Errorf("unreadable Hacker News item")
	}
	text = plainText(r.StoryText)
	if text == "" {
		text = plainText(r.Text)
	}
	for _, c := range r.Children {
		if s := plainText(c.Text); s != "" {
			comments = append(comments, capRunes(s, commentTextCap))
		}
		if len(comments) == threadCommentN {
			break
		}
	}
	return clean(r.Title), text, comments, nil
}

// parseRedditPost reads the post body (selftext) from a thread's .json: a list whose first listing holds the post.
func parseRedditPost(body []byte) (string, error) {
	var r []struct {
		Data struct {
			Children []struct {
				Data struct {
					Selftext string `json:"selftext"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil || len(r) == 0 || len(r[0].Data.Children) == 0 {
		return "", fmt.Errorf("unreadable Reddit thread")
	}
	return plainText(r[0].Data.Children[0].Data.Selftext), nil
}

func joinThreadText(title, text string, comments []string) string {
	var b strings.Builder
	b.WriteString(title)
	if text != "" {
		b.WriteString("\n" + text)
	}
	if len(comments) > 0 {
		b.WriteString("\nTop comments:")
		for _, c := range comments {
			b.WriteString("\n- " + c)
		}
	}
	return capRunes(b.String(), threadTextCap)
}

// plainText drops markup from a post or comment body (Hacker News sends HTML) and tidies its spaces.
func plainText(s string) string {
	s = strings.NewReplacer("&#x27;", "'", "&#x2F;", "/").Replace(s)
	return clean(htmlTag.ReplaceAllString(s, " "))
}

func capRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
