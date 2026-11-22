package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/griffin/core/internal/budget"
)

// Community platforms, recorded on each thread.
const (
	Reddit     = "Reddit"
	HackerNews = "Hacker News"
)

// CommunityUserAgent says what is reading, and who is not named: no personal details go to any site.
const CommunityUserAgent = "griffin-community-scan/1.0"

// RedditSub is a subreddit worth reading. Topic is what the whole sub is about, so a thread there counts for that
// topic even when its title never says it; "" when the sub is too broad to say anything (the thread's own words
// decide).
type RedditSub struct {
	Name  string
	Topic string
}

// RedditSubs is the one place the subreddits are listed.
var RedditSubs = []RedditSub{
	{"LocalLLaMA", "LLMs"},
	{"MachineLearning", "ML"},
	{"learnmachinelearning", "ML"},
	{"golang", "Go"},
	{"developersIndia", ""},
	{"csMajors", ""},
	{"cscareerquestionsIN", ""},
}

// HNSearch is one Hacker News (Algolia) search: a tag and a query.
type HNSearch struct{ Tag, Query string }

// HNSearches is the one place the Hacker News searches are listed. Ask HN is where people ask, so most are
// asks; a couple of story searches catch discussions of Go and task queues.
var HNSearches = []HNSearch{
	{"ask_hn", "agents"}, {"ask_hn", "LLM"}, {"ask_hn", "RAG"}, {"ask_hn", "inference"},
	{"ask_hn", "golang"}, {"ask_hn", "distributed systems"}, {"ask_hn", "task queue"}, {"ask_hn", "internship"},
	{"story", "golang"}, {"story", "task queue"},
}

// TODO(x): X/Twitter has no free read API and must not be scraped. When an X source is added it plugs in here, as
// a read by the owner's signed-in browser on the laptop (a hand), returning []Thread with Platform "X" like the
// two readers below; ReadCommunities would then append them before scoring. Nothing reads X today.

const (
	redditBase = "https://www.reddit.com"
	hnBase     = "https://hn.algolia.com/api/v1/search_by_date"
	hnItem     = "https://news.ycombinator.com/item?id="
)

// Thread is one public conversation, read from the site's own listing.
type Thread struct {
	Platform  string
	Community string // "r/golang", or "Ask HN" / "HN"
	Title     string
	Text      string // the start of the post's body, when the listing carries it
	URL       string // the discussion page, never an outside link
	Posted    time.Time
	Comments  int
	Score     int
	// Closed to replies or not for the owner to see: such a thread is never listed.
	NSFW, Locked, Archived, Stickied bool
}

// StatusError is a site answering something other than 200.
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s answered HTTP %d", e.URL, e.Code) }

// CommunityGet is the real Get for the community sites: it says what it is and reads only.
func CommunityGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", CommunityUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{URL: u, Code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// ReadCommunities reads Reddit and Hacker News. Every page that fails is named in problems and the rest still
// count. Reddit often refuses automated reads (403, 429 or an HTML wall): the first refusal ends the Reddit part
// of the run, so a block costs one line, not fourteen.
func ReadCommunities(ctx context.Context, get Get, now time.Time) (threads []Thread, problems []string) {
	if get == nil {
		get = CommunityGet
	}
	skipped, timeLeft := 0, 0 // pages skipped after a refusal, and pages the time budget did not reach
	blocked := ""
	for _, sub := range RedditSubs {
		for _, page := range []string{"new.json?limit=50", "top.json?t=day&limit=50"} {
			if blocked != "" {
				skipped++
				continue
			}
			if budget.Short(ctx) {
				timeLeft++
				continue
			}
			u := redditBase + "/r/" + sub.Name + "/" + page
			body, err := get(ctx, u)
			if err == nil {
				var ts []Thread
				if ts, err = ParseReddit(body); err == nil {
					threads = append(threads, ts...)
					continue
				}
			}
			blocked = "r/" + sub.Name + ": " + describeReadError(err)
		}
	}
	if blocked != "" {
		problems = append(problems, fmt.Sprintf("Reddit refused the read (%s); %d Reddit pages skipped", blocked, skipped))
	}
	since := now.Add(-48 * time.Hour).Unix()
	for _, s := range HNSearches {
		if budget.Short(ctx) {
			timeLeft++
			continue
		}
		u := fmt.Sprintf("%s?tags=%s&query=%s&hitsPerPage=30&numericFilters=%s", hnBase,
			s.Tag, url.QueryEscape(s.Query), url.QueryEscape("created_at_i>"+strconv.FormatInt(since, 10)))
		body, err := get(ctx, u)
		if err != nil {
			problems = append(problems, "Hacker News "+s.Tag+" \""+s.Query+"\"")
			continue
		}
		ts, err := ParseHN(body)
		if err != nil {
			problems = append(problems, "Hacker News "+s.Tag+" \""+s.Query+"\"")
			continue
		}
		threads = append(threads, ts...)
	}
	if timeLeft > 0 {
		problems = append(problems, fmt.Sprintf("%d pages left for the next run (out of time)", timeLeft))
	}
	return threads, problems
}

func describeReadError(err error) string {
	if se, ok := err.(*StatusError); ok {
		return "HTTP " + strconv.Itoa(se.Code)
	}
	return "no answer"
}

// ParseReddit reads a subreddit listing (new.json or top.json). A body that is not a listing (the HTML wall
// Reddit serves to automated readers) is an error.
func ParseReddit(body []byte) ([]Thread, error) {
	var r struct {
		Data struct {
			Children []struct {
				Data struct {
					Title      string  `json:"title"`
					Selftext   string  `json:"selftext"`
					Permalink  string  `json:"permalink"`
					Subreddit  string  `json:"subreddit"`
					CreatedUTC float64 `json:"created_utc"`
					Comments   int     `json:"num_comments"`
					Score      int     `json:"score"`
					Over18     bool    `json:"over_18"`
					Locked     bool    `json:"locked"`
					Archived   bool    `json:"archived"`
					Stickied   bool    `json:"stickied"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("unreadable Reddit listing")
	}
	var out []Thread
	for _, c := range r.Data.Children {
		d := c.Data
		if d.Title == "" || d.Permalink == "" {
			continue
		}
		out = append(out, Thread{
			Platform: Reddit, Community: "r/" + d.Subreddit, Title: clean(d.Title), Text: snippet(d.Selftext),
			URL:    redditBase + d.Permalink,
			Posted: time.Unix(int64(d.CreatedUTC), 0).UTC(), Comments: d.Comments, Score: d.Score,
			NSFW: d.Over18, Locked: d.Locked, Archived: d.Archived, Stickied: d.Stickied,
		})
	}
	return out, nil
}

// ParseHN reads a Hacker News (Algolia) search result.
func ParseHN(body []byte) ([]Thread, error) {
	var r struct {
		Hits []struct {
			ObjectID  string   `json:"objectID"`
			Title     string   `json:"title"`
			Comments  int      `json:"num_comments"`
			Points    int      `json:"points"`
			CreatedAt int64    `json:"created_at_i"`
			Tags      []string `json:"_tags"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("unreadable Hacker News result")
	}
	var out []Thread
	for _, h := range r.Hits {
		if h.ObjectID == "" || h.Title == "" {
			continue
		}
		community := "HN"
		for _, t := range h.Tags {
			if t == "ask_hn" {
				community = "Ask HN"
			}
		}
		out = append(out, Thread{
			Platform: HackerNews, Community: community, Title: clean(h.Title), URL: hnItem + h.ObjectID,
			Posted: time.Unix(h.CreatedAt, 0).UTC(), Comments: h.Comments, Score: h.Points,
		})
	}
	return out, nil
}

// snippet is the first part of a post body, enough to read its topic.
func snippet(s string) string {
	s = clean(s)
	if r := []rune(s); len(r) > 600 {
		return string(r[:600])
	}
	return s
}
