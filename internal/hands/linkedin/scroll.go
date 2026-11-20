// Package linkedin reads the LinkedIn app on an adb-attached phone. It scrolls the feed, expands
// truncated posts by tapping their "…more" link, and keeps only genuine job posts from a named person
// or company. The only thing it ever taps is a "…more" link: no likes, comments, applies or messages.
package linkedin

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/griffin/core/internal/hands/whatsapp"
)

const pkg = "com.linkedin.android"

var (
	boundsRe = regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)
	urlRe    = regexp.MustCompile(`https?://[^\s"')]+`)
	moreRe   = regexp.MustCompile(`(?i)(…|\.\.\.)\s*more$`)
	// sponsoredRe marks the label LinkedIn puts on ads.
	sponsoredRe = regexp.MustCompile(`(?i)^(promoted|sponsored)$`)
	// jobRe marks a post that is about hiring.
	jobRe = regexp.MustCompile(`(?i)\b(hiring|internships?|we are looking for|job opening|open roles?|vacanc\w*|apply (now|here|at|via|on)|referral)\b`)
	// roleRe marks a post that names an actual role.
	roleRe = regexp.MustCompile(`(?i)\b(engineer|developer|intern|analyst|scientist|sde|swe|designer|architect|researcher|associate|manager)\b`)
	// seekerRe marks someone asking for work rather than offering it.
	seekerRe = regexp.MustCompile(`(?i)(open to work|i am looking for|i'?m looking for|looking for (a |an )?(new )?(job|role|opportunit)|laid off)`)
)

// Post is a job post from a named author that passed every filter.
type Post struct {
	Author string   `json:"author"`
	Text   string   `json:"text"`
	URLs   []string `json:"urls,omitempty"`
}

// Stats counts why posts were dropped, so a run can be audited.
type Stats struct {
	Seen, Ads, NoAuthor, NotAJob, Seeker, NotReal, Unverified, Kept int
	Stuck                                                           bool // ended early because the screen stopped changing
}

type uiNode struct {
	Text      string   `xml:"text,attr"`
	Desc      string   `xml:"content-desc,attr"`
	ID        string   `xml:"resource-id,attr"`
	Clickable string   `xml:"clickable,attr"`
	Bounds    string   `xml:"bounds,attr"`
	Children  []uiNode `xml:"node"`
}

// item is one piece of readable text on screen with its position.
type item struct {
	text              string
	l, t, r, b        int
	clickable, isTabs bool
}

func (i item) cx() int { return (i.l + i.r) / 2 }
func (i item) cy() int { return (i.t + i.b) / 2 }

func flatten(n uiNode, out *[]item) {
	s := strings.TrimSpace(n.Text)
	if s == "" {
		s = strings.TrimSpace(n.Desc)
	}
	if m := boundsRe.FindStringSubmatch(n.Bounds); m != nil && s != "" {
		v := func(k int) int { x, _ := strconv.Atoi(m[k]); return x }
		*out = append(*out, item{text: s, l: v(1), t: v(2), r: v(3), b: v(4), clickable: n.Clickable == "true"})
	}
	if strings.HasSuffix(n.ID, ":id/tab_feed") {
		*out = append(*out, item{text: "\x00tabbar"})
	}
	for _, c := range n.Children {
		flatten(c, out)
	}
}

func parse(dump string) ([]item, error) {
	var h struct {
		Nodes []uiNode `xml:"node"`
	}
	dec := xml.NewDecoder(strings.NewReader(dump))
	dec.Strict = false
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("could not read the phone's screen: %w", err)
	}
	var out []item
	for _, n := range h.Nodes {
		flatten(n, &out)
	}
	return out, nil
}

// isBody reports whether the item is a post's text: wide, tappable, and long enough to be prose.
func isBody(i item) bool {
	return i.clickable && i.r-i.l >= 900 && len(i.text) >= 30 && !strings.HasPrefix(i.text, "View image")
}

// above returns the nearest item above the body within 450px that satisfies match.
func above(items []item, body item, match func(item) bool) (item, bool) {
	best, found := item{}, false
	for _, i := range items {
		if i.b <= body.t+5 && body.t-i.t <= 450 && match(i) && (!found || i.b > best.b) {
			best, found = i, true
		}
	}
	return best, found
}

// Judge decides whether one post is kept. author is empty when no named author was found above it.
func Judge(author, text string, promoted bool, st *Stats) bool {
	st.Seen++
	switch {
	case promoted:
		st.Ads++
	case author == "":
		st.NoAuthor++
	case seekerRe.MatchString(text):
		st.Seeker++
	case !jobRe.MatchString(text) || !roleRe.MatchString(text):
		st.NotAJob++
	default:
		st.Kept++
		return true
	}
	return false
}

func isAuthorLine(t string) bool {
	return (strings.HasPrefix(t, "Follow ") && len(t) > 7) || strings.Contains(t, "Verified Profile") || strings.HasSuffix(t, " Following")
}

func authorOf(items []item, body item) string {
	if f, ok := above(items, body, func(i item) bool { return strings.HasPrefix(i.text, "Follow ") && len(i.text) > 7 }); ok {
		return strings.TrimPrefix(f.text, "Follow ")
	}
	if f, ok := above(items, body, func(i item) bool {
		return strings.Contains(i.text, "Verified Profile") || strings.HasSuffix(i.text, " Following")
	}); ok {
		return strings.TrimSpace(strings.SplitN(f.text, "Verified Profile", 2)[0])
	}
	return ""
}

// Classifier makes the final call on a post that passed the cheap filters: is the author really offering
// this job, or are they joking about one, quoting it, complaining, or replying to somebody else's post?
type Classifier interface {
	IsRealJob(ctx context.Context, author, text string) (bool, error)
}

// Scroller scrolls the LinkedIn feed. Without a Classifier nothing is kept: a post is never added to the
// list on the strength of keywords alone.
type Scroller struct {
	ADB        whatsapp.ADB
	Classifier Classifier
	Settle     time.Duration // wait for the app to open; default 4 s
	Pause      time.Duration // wait after each swipe or tap; default 1.5 s
}

// dump reads the screen. uiautomator fails now and then while the app is redrawing (or the phone dozes),
// so a failed read is retried a few times before the run gives up.
func (s Scroller) dump(ctx context.Context) ([]item, error) {
	var err error
	for try := 0; try < 4; try++ {
		if try > 0 {
			sleep(ctx, s.Pause)
		}
		var raw string
		if _, err = s.ADB.Run(ctx, "shell", "uiautomator", "dump", "/sdcard/griffin_li.xml"); err != nil {
			continue
		}
		if raw, err = s.ADB.Run(ctx, "exec-out", "cat", "/sdcard/griffin_li.xml"); err != nil {
			continue
		}
		return parse(raw)
	}
	return nil, err
}

func hasTabBar(items []item) bool {
	for _, i := range items {
		if i.text == "\x00tabbar" {
			return true
		}
	}
	return false
}

// expand taps a body's "…more" link and returns the full text. It leaves the app where it found it.
func (s Scroller) expand(ctx context.Context, body item) (string, error) {
	if !moreRe.MatchString(body.text) {
		return body.text, nil
	}
	if _, err := s.ADB.Run(ctx, "shell", "input", "tap", strconv.Itoa(body.cx()), strconv.Itoa(body.cy())); err != nil {
		return body.text, err
	}
	sleep(ctx, s.Pause)
	after, err := s.dump(ctx)
	if err != nil {
		return body.text, err
	}
	full := body.text
	onFeed := hasTabBar(after)
	for _, i := range after {
		if onFeed {
			// Expanded in place: the same tappable block, now longer.
			if isBody(i) && len(i.text) > len(full) && abs(i.t-body.t) < 400 {
				full = i.text
			}
			continue
		}
		// The tap opened the post's own page, where the text is a plain (not tappable) block: take the longest
		// wide one that carries on from the text we already had.
		if i.r-i.l >= 700 && len(i.text) > len(full) && strings.HasPrefix(i.text, trimMore(body.text)) {
			full = i.text
		}
	}
	if !onFeed { // come back to the feed
		_, _ = s.ADB.Run(ctx, "shell", "input", "keyevent", "KEYCODE_BACK")
		sleep(ctx, s.Pause)
	}
	return full, nil
}

// trimMore drops the trailing "… more" so the start of the post can be matched against its full text.
func trimMore(s string) string {
	return strings.TrimSpace(moreRe.ReplaceAllString(s, ""))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func unlock(ctx context.Context, adb whatsapp.ADB) {
	pin := os.Getenv("GRIFFIN_PHONE_PIN")
	if pin == "" {
		return
	}
	_, _ = adb.Run(ctx, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	_, _ = adb.Run(ctx, "shell", "input", "swipe", "540", "2000", "540", "600", "200")
	sleep(ctx, 400*time.Millisecond)
	_, _ = adb.Run(ctx, "shell", "input", "text", pin)
	_, _ = adb.Run(ctx, "shell", "input", "keyevent", "66")
	sleep(ctx, time.Second)
}

// Run opens LinkedIn and scrolls for the given duration, returning the posts that passed every filter.
func (s Scroller) Run(ctx context.Context, d time.Duration) ([]Post, Stats, error) {
	if s.Pause <= 0 {
		s.Pause = 1500 * time.Millisecond
	}
	if s.Settle <= 0 {
		s.Settle = 4 * time.Second
	}
	var st Stats
	unlock(ctx, s.ADB)
	if _, err := s.ADB.Run(ctx, "shell", "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return nil, st, fmt.Errorf("could not open LinkedIn: %w", err)
	}
	if !sleep(ctx, s.Settle) {
		return nil, st, ctx.Err()
	}
	seen := map[string]bool{}
	var posts []Post
	last, same := "", 0
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		items, err := s.dump(ctx)
		if err != nil {
			return posts, st, err
		}
		// Loop guard (an idea from OpenJarvis): an unchanged screen after a swipe means the feed ended or the
		// app froze, so stop instead of swiping at nothing for the rest of the time budget.
		if sig := screenSig(items); sig == last {
			if same++; same >= 3 {
				st.Stuck = true
				return posts, st, nil
			}
		} else {
			last, same = sig, 0
		}
		for _, body := range items {
			if !isBody(body) {
				continue
			}
			key := strings.ToLower(body.text)
			if len(key) > 40 { // a short prefix: the same post reads the same at its start before and after "…more"
				key = key[:40]
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			// The label nearest above the body decides: an earlier card's "Promoted" must not mark this one.
			head, _ := above(items, body, func(i item) bool { return sponsoredRe.MatchString(i.text) || isAuthorLine(i.text) })
			promoted := sponsoredRe.MatchString(head.text)
			author := authorOf(items, body)
			// Cheap checks first, so ads and strangers' chatter are never expanded.
			if promoted || author == "" || (!moreRe.MatchString(body.text) && !Judge(author, body.text, false, &Stats{})) {
				Judge(author, body.text, promoted, &st)
				continue
			}
			full, err := s.expand(ctx, body)
			if err != nil {
				return posts, st, err
			}
			if !Judge(author, full, false, &st) {
				continue
			}
			st.Kept-- // Judge counted it; it only stays counted if the classifier agrees
			real, err := false, error(nil)
			if s.Classifier != nil {
				real, err = s.Classifier.IsRealJob(ctx, author, full)
			}
			switch {
			case s.Classifier == nil || err != nil:
				st.Unverified++
			case !real:
				st.NotReal++
			default:
				st.Kept++
				posts = append(posts, Post{Author: author, Text: full, URLs: urlRe.FindAllString(full, -1)})
			}
		}
		if _, err := s.ADB.Run(ctx, "shell", "input", "swipe", "540", "1800", "540", "700", "400"); err != nil {
			return posts, st, err
		}
		if !sleep(ctx, s.Pause) {
			return posts, st, ctx.Err()
		}
	}
	return posts, st, nil
}

func screenSig(items []item) string {
	var b strings.Builder
	for _, i := range items {
		b.WriteString(i.text)
		b.WriteByte('|')
	}
	return b.String()
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
