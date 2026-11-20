package linkedin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func node(text, bounds string, clickable bool) string {
	c := "false"
	if clickable {
		c = "true"
	}
	return `<node text="` + text + `" content-desc="" resource-id="" clickable="` + c + `" bounds="` + bounds + `"/>`
}

// A feed with an ad, a real hiring post (truncated), a joke about a job post, and chatter with no author.
var feed = `<hierarchy>` +
	node("Promoted", "[204,333][948,361]", false) +
	node("Sixty compliance checks for India, hiring engineers… more", "[0,361][1080,475]", true) +
	node("Follow Vishakha Singhal", "[836,412][1068,508]", false) +
	node("American Express is hiring a Backend Engineer intern. Apply now… more", "[0,571][1080,762]", true) +
	node("Follow Some Comedian", "[836,900][1068,990]", false) +
	node("lol another hiring post for an engineer with 10 years of experience for an intern role", "[0,1000][1080,1200]", true) +
	node("Great weekend everyone, here is a long photo caption about hiking trips", "[0,1500][1080,1700]", true) +
	`<node text="" content-desc="" resource-id="com.linkedin.android:id/tab_feed" clickable="true" bounds="[0,2124][216,2268]"/>` +
	`</hierarchy>`

type fakeADB struct {
	calls    []string
	expanded bool
	failDump int // how many uiautomator dumps fail before one works
}

func (f *fakeADB) Run(_ context.Context, args ...string) (string, error) {
	line := strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if strings.Contains(line, "uiautomator dump") && f.failDump > 0 {
		f.failDump--
		return "", errors.New("exit status 255")
	}
	if strings.HasPrefix(line, "shell input tap") {
		f.expanded = true
	}
	if args[0] == "exec-out" {
		if f.expanded {
			return strings.Replace(feed, "Apply now… more", "Apply now at https://amex.example/jobs/9 for summer 2027.", 1), nil
		}
		return feed, nil
	}
	return "", nil
}

type fakeClassifier struct {
	answers map[string]bool
	err     error
	asked   []string
}

func (c *fakeClassifier) IsRealJob(_ context.Context, author, _ string) (bool, error) {
	c.asked = append(c.asked, author)
	return c.answers[author], c.err
}

func run(t *testing.T, cl Classifier) ([]Post, Stats, *fakeADB) {
	t.Helper()
	f := &fakeADB{}
	posts, st, err := Scroller{ADB: f, Classifier: cl, Pause: time.Millisecond, Settle: time.Millisecond}.Run(context.Background(), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return posts, st, f
}

func TestKeepsOnlyARealJobFromANamedAuthor(t *testing.T) {
	cl := &fakeClassifier{answers: map[string]bool{"Vishakha Singhal": true, "Some Comedian": false}}
	posts, st, _ := run(t, cl)
	if len(posts) != 1 || posts[0].Author != "Vishakha Singhal" || len(posts[0].URLs) != 1 {
		t.Fatalf("posts=%+v stats=%+v", posts, st)
	}
	if st.Ads != 1 || st.NotReal != 1 || st.NoAuthor != 1 || st.Kept != 1 {
		t.Fatalf("stats=%+v", st)
	}
	for _, a := range cl.asked {
		if a == "" {
			t.Fatal("the classifier must never be asked about an ad or an authorless post")
		}
	}
}

func TestNothingIsKeptWithoutAClassifierOrWhenItFails(t *testing.T) {
	for _, cl := range []Classifier{nil, &fakeClassifier{err: errors.New("quota")}} {
		posts, st, _ := run(t, cl)
		if len(posts) != 0 || st.Unverified == 0 {
			t.Fatalf("an unverified post must not be kept: posts=%+v stats=%+v", posts, st)
		}
	}
}

func TestOnlyEverTapsAMoreLink(t *testing.T) {
	_, _, f := run(t, &fakeClassifier{answers: map[string]bool{"Vishakha Singhal": true}})
	for _, c := range f.calls {
		if strings.Contains(c, "input text") || strings.HasPrefix(c, "shell input tap") && !strings.Contains(c, "540 666") {
			t.Fatalf("unexpected input: %s", c)
		}
	}
	// the ad's "more" link is never opened
	for _, c := range f.calls {
		if strings.Contains(c, "540 418") {
			t.Fatalf("the ad was expanded: %s", c)
		}
	}
}

func TestARedrawingScreenIsRetriedNotFatal(t *testing.T) {
	f := &fakeADB{failDump: 2}
	cl := &fakeClassifier{answers: map[string]bool{"Vishakha Singhal": true}}
	posts, _, err := Scroller{ADB: f, Classifier: cl, Pause: time.Millisecond, Settle: time.Millisecond}.Run(context.Background(), 20*time.Millisecond)
	if err != nil || len(posts) != 1 {
		t.Fatalf("a transient dump failure must not end the run: posts=%+v err=%v", posts, err)
	}
}
