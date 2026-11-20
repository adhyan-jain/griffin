package linkedin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakePage struct {
	feed    []WebPost
	scripts []string
}

func (f *fakePage) Eval(_ context.Context, script string) (json.RawMessage, error) {
	f.scripts = append(f.scripts, script)
	if strings.Contains(script, "Feed post") {
		return json.Marshal(f.feed)
	}
	return json.RawMessage("null"), nil
}

func TestWebScrollerKeepsOnlyVerifiedRealJobs(t *testing.T) {
	page := &fakePage{feed: []WebPost{
		{Author: "Vishakha Singhal", Text: "American Express is hiring a Backend Engineer intern. Apply now at https://amex.example/jobs/9", URLs: []string{"https://amex.example/jobs/9"}},
		{Author: "Some Comedian", Text: "lol another hiring post for an engineer with 10 years of experience for an intern role"},
		{Author: "Google Maps Platform", Text: "We are hiring engineers! Apply now", Promoted: true},
		{Author: "", Text: "Nobody wrote this hiring engineer post"},
		{Author: "A Friend", Text: "Great weekend everyone, here is a long photo caption about hiking trips"},
	}}
	cl := &fakeClassifier{answers: map[string]bool{"Vishakha Singhal": true, "Some Comedian": false}}
	posts, st, err := WebScroller{Page: page, Classifier: cl, Pause: time.Millisecond}.Run(context.Background(), 30*time.Millisecond)
	if err != nil || len(posts) != 1 || posts[0].Author != "Vishakha Singhal" || len(posts[0].URLs) != 1 {
		t.Fatalf("posts=%+v stats=%+v err=%v", posts, st, err)
	}
	if st.Ads != 1 || st.NoAuthor != 1 || st.NotAJob != 1 || st.NotReal != 1 || st.Kept != 1 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestWebScrollerKeepsNothingWithoutAClassifier(t *testing.T) {
	page := &fakePage{feed: []WebPost{{Author: "Vishakha Singhal", Text: "American Express is hiring a Backend Engineer intern. Apply now"}}}
	posts, st, _ := WebScroller{Page: page, Pause: time.Millisecond}.Run(context.Background(), 20*time.Millisecond)
	if len(posts) != 0 || st.Unverified != 1 {
		t.Fatalf("posts=%+v stats=%+v", posts, st)
	}
}

func TestWebScrollerStopsWhenTheFeedStopsMoving(t *testing.T) {
	start := time.Now()
	_, st, err := WebScroller{Page: &fakePage{feed: []WebPost{{Author: "A", Text: "x"}}}, Pause: time.Millisecond}.Run(context.Background(), 30*time.Second)
	if err != nil || !st.Stuck || time.Since(start) > 5*time.Second {
		t.Fatalf("stuck=%v err=%v after %v", st.Stuck, err, time.Since(start))
	}
}

func TestTheOnlyClickIsASeeMoreToggle(t *testing.T) {
	clicks := strings.Count(feedScript, ".click()")
	if clicks != 1 || !strings.Contains(feedScript, "more.click()") {
		t.Fatalf("the feed script must click exactly one thing, the see-more toggle (found %d clicks)", clicks)
	}
}
