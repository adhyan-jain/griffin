package sources

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixtures are dated around this moment (2026-11-22 10:03 UTC).
var communityNow = time.Unix(1795343000, 0).UTC()

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHackerNewsHitsAreReadWithTheirOwnDiscussionLink(t *testing.T) {
	ts, err := ParseHN(fixture(t, "hn_ask.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 7 {
		t.Fatalf("seven hits in the saved response, got %d", len(ts))
	}
	var ask *Thread
	for i := range ts {
		if ts[i].URL == "https://news.ycombinator.com/item?id=50503017" {
			ask = &ts[i]
		}
	}
	if ask == nil || ask.Platform != HackerNews || ask.Community != "Ask HN" || ask.Comments != 2 ||
		ask.Title != "Ask HN: What AI agent infrastructure are you using in 2026?" || ask.Posted.Unix() != 1794705763 {
		t.Fatalf("the Ask HN thread: %+v", ask)
	}
}

func TestRedditListingIsReadAndAWallOfHTMLIsAnError(t *testing.T) {
	ts, err := ParseReddit(fixture(t, "reddit_listing.json"))
	if err != nil || len(ts) != 7 {
		t.Fatalf("seven threads in the saved listing: %d %v", len(ts), err)
	}
	first := ts[0]
	if first.Community != "r/golang" || first.URL != "https://www.reddit.com/r/golang/comments/1abc001/how_do_you_build_a_task_queue_with_redis/" ||
		first.Comments != 4 || first.Posted.Unix() != 1795340000 || !strings.Contains(first.Text, "worker nodes") {
		t.Fatalf("first thread: %+v", first)
	}
	if !ts[2].Stickied || !ts[3].Locked || !ts[4].NSFW || !ts[5].Archived {
		t.Fatal("the flags are read as the site shows them")
	}
	if _, err := ParseReddit([]byte("<body class=theme-beta><div>blocked</div>")); err == nil {
		t.Fatal("an HTML page is not a listing")
	}
}

func TestOnlyThreadsForTheOwnerAreKeptAndBestComeFirst(t *testing.T) {
	reddit, _ := ParseReddit(fixture(t, "reddit_listing.json"))
	hn, _ := ParseHN(fixture(t, "hn_ask.json"))
	picked := PickCommunityThreads(append(reddit, hn...), nil, communityNow, 15)
	got := map[string]Scored{}
	for _, s := range picked {
		got[s.Thread.URL] = s
	}
	task := got["https://www.reddit.com/r/golang/comments/1abc001/how_do_you_build_a_task_queue_with_redis/"]
	if task.Mention == "" || !strings.Contains(task.Mention, "ComputeHive") || !strings.Contains(task.Why, "task queues") {
		t.Fatalf("a task-queue question in r/golang is where ComputeHive is worth a mention: %+v", task)
	}
	if picked[0].Thread.URL != task.Thread.URL {
		t.Fatalf("the best fit is first: %+v", picked[0])
	}
	vllm := got["https://www.reddit.com/r/LocalLLaMA/comments/1abc002/best_way_to_serve_a_quantized_model_with_vllm/"]
	if vllm.Score == 0 || vllm.Mention != "" {
		t.Fatalf("an inference question fits but names no project: %+v", vllm)
	}
	agents := got["https://news.ycombinator.com/item?id=50589077"]
	if agents.Score == 0 || !strings.Contains(agents.Mention, "agent work") {
		t.Fatalf("a question about an agent framework fits and his agent work is worth a mention: %+v", agents)
	}
	for _, skipped := range []string{
		"/r/golang/comments/1abc003/", "/r/developersIndia/comments/1abc004/", "/r/csMajors/comments/1abc005/",
		"/r/MachineLearning/comments/1abc006/", "/r/cscareerquestionsIN/comments/1abc007/",
		"id=50494696", "id=50513451", "id=50528548", "id=50503017", // older than 48 hours
	} {
		for u := range got {
			if strings.Contains(u, skipped) {
				t.Errorf("%s should not be listed (pinned, locked, adult, archived, off topic or too old)", u)
			}
		}
	}
}

func TestAThreadAlreadyListedOrSeenTwiceIsKeptOnce(t *testing.T) {
	reddit, _ := ParseReddit(fixture(t, "reddit_listing.json"))
	both := append(append([]Thread{}, reddit...), reddit...)
	if got := PickCommunityThreads(both, nil, communityNow, 15); len(got) != 2 {
		t.Fatalf("the same thread from new and top is one: %d", len(got))
	}
	known := map[string]bool{strings.ToLower(reddit[0].URL): true}
	got := PickCommunityThreads(reddit, known, communityNow, 15)
	if len(got) != 1 || strings.Contains(got[0].Thread.URL, "1abc001") {
		t.Fatalf("a listed thread is not listed again: %+v", got)
	}
	if got := PickCommunityThreads(reddit, nil, communityNow, 1); len(got) != 1 {
		t.Fatal("only the top few are kept")
	}
}

func TestAHugeThreadScoresBelowASmallOne(t *testing.T) {
	small := Thread{Platform: HackerNews, Title: "Ask HN: how do I learn LLM inference?", URL: "u1", Posted: communityNow.Add(-2 * time.Hour), Comments: 5}
	huge := small
	huge.URL, huge.Comments = "u2", 400
	a, _ := ScoreThread(small, nil, communityNow)
	b, _ := ScoreThread(huge, nil, communityNow)
	if a.Score <= b.Score {
		t.Fatalf("a reply is seen on the small one: %d vs %d", a.Score, b.Score)
	}
	if _, ok := ScoreThread(Thread{Title: "How do I cook rice?", URL: "u3", Posted: communityNow}, nil, communityNow); ok {
		t.Fatal("a thread about none of his topics is dropped")
	}
}

func TestRedditBlockedOnceStopsRedditButHackerNewsStillReads(t *testing.T) {
	var redditCalls int
	get := func(_ context.Context, u string) ([]byte, error) {
		switch {
		case strings.Contains(u, "reddit.com"):
			redditCalls++
			return nil, &StatusError{URL: u, Code: 403}
		case strings.Contains(u, "hn.algolia.com"):
			return os.ReadFile("testdata/hn_ask.json")
		}
		return nil, errors.New("unexpected " + u)
	}
	ts, problems := ReadCommunities(context.Background(), get, communityNow)
	if redditCalls != 1 {
		t.Fatalf("a block ends the Reddit reads after one request, got %d", redditCalls)
	}
	if len(ts) == 0 || len(problems) != 1 || !strings.Contains(problems[0], "HTTP 403") || !strings.Contains(problems[0], "r/LocalLLaMA") {
		t.Fatalf("one problem names the block and Hacker News still read: %d threads, %v", len(ts), problems)
	}
}

func TestEachSiteIsAskedWithItsOwnUrlShape(t *testing.T) {
	var urls []string
	get := func(_ context.Context, u string) ([]byte, error) {
		urls = append(urls, u)
		if strings.Contains(u, "reddit.com") {
			return os.ReadFile("testdata/reddit_listing.json")
		}
		return []byte(`{"hits":[]}`), nil
	}
	_, problems := ReadCommunities(context.Background(), get, communityNow)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	want := []string{
		"https://www.reddit.com/r/golang/new.json?limit=50",
		"https://www.reddit.com/r/golang/top.json?t=day&limit=50",
		"https://hn.algolia.com/api/v1/search_by_date?tags=ask_hn&query=agents&hitsPerPage=30&numericFilters=created_at_i%3E1795170200",
	}
	joined := strings.Join(urls, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %s in\n%s", w, joined)
		}
	}
}
