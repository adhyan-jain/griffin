// Package linkedin reads the LinkedIn app on an adb-attached phone. It only reads: it scrolls the feed
// and collects the text that is on screen, and never taps, likes, comments, applies or messages.
package linkedin

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/hands/whatsapp"
)

const pkg = "com.linkedin.android"

// jobWords marks a post as a job or internship opportunity.
var jobWords = regexp.MustCompile(`(?i)\b(hiring|we'?re hiring|internships?|intern\b|apply|job opening|open roles?|vacanc|careers?|looking for (an? )?\w+ (engineer|developer|intern)|referral)\b`)

var urlRe = regexp.MustCompile(`https?://[^\s"')]+`)

// Post is one block of feed text that looks like a job opportunity.
type Post struct {
	Text string   `json:"text"`
	URLs []string `json:"urls,omitempty"`
}

type node struct {
	Text     string `xml:"text,attr"`
	Desc     string `xml:"content-desc,attr"`
	Children []node `xml:"node"`
}

func (n node) walk(f func(node)) {
	f(n)
	for _, c := range n.Children {
		c.walk(f)
	}
}

// ParseScreen returns the readable text blocks of one uiautomator dump, longest first-seen order kept.
func ParseScreen(dump string) ([]string, error) {
	var h struct {
		Nodes []node `xml:"node"`
	}
	dec := xml.NewDecoder(strings.NewReader(dump))
	dec.Strict = false
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("could not read the phone's screen: %w", err)
	}
	var out []string
	for _, n := range h.Nodes {
		n.walk(func(x node) {
			for _, s := range []string{x.Text, x.Desc} {
				if s = strings.TrimSpace(s); len(s) >= 40 {
					out = append(out, s)
				}
			}
		})
	}
	return out, nil
}

// Collect keeps the blocks that mention a job, deduplicated across scrolls.
func Collect(seen map[string]bool, blocks []string) []Post {
	var posts []Post
	for _, b := range blocks {
		key := strings.ToLower(b)
		if len(key) > 120 {
			key = key[:120]
		}
		if seen[key] || !jobWords.MatchString(b) {
			continue
		}
		seen[key] = true
		posts = append(posts, Post{Text: b, URLs: urlRe.FindAllString(b, -1)})
	}
	return posts
}

// Scroller scrolls the LinkedIn feed.
type Scroller struct {
	ADB   whatsapp.ADB
	Pause time.Duration // wait after each swipe; default 1.5 s
}

// Run opens LinkedIn and scrolls for the given duration, returning the job-like posts it saw.
func (s Scroller) Run(ctx context.Context, d time.Duration) ([]Post, error) {
	if s.Pause <= 0 {
		s.Pause = 1500 * time.Millisecond
	}
	if _, err := s.ADB.Run(ctx, "shell", "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return nil, fmt.Errorf("could not open LinkedIn: %w", err)
	}
	if !sleep(ctx, 4*time.Second) {
		return nil, ctx.Err()
	}
	seen := map[string]bool{}
	var posts []Post
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		xmlText, err := s.dump(ctx)
		if err != nil {
			return posts, err
		}
		blocks, err := ParseScreen(xmlText)
		if err != nil {
			return posts, err
		}
		posts = append(posts, Collect(seen, blocks)...)
		if _, err := s.ADB.Run(ctx, "shell", "input", "swipe", "540", "1800", "540", "600", "400"); err != nil {
			return posts, err
		}
		if !sleep(ctx, s.Pause) {
			return posts, ctx.Err()
		}
	}
	return posts, nil
}

func (s Scroller) dump(ctx context.Context) (string, error) {
	if _, err := s.ADB.Run(ctx, "shell", "uiautomator", "dump", "/sdcard/griffin_li.xml"); err != nil {
		return "", err
	}
	return s.ADB.Run(ctx, "exec-out", "cat", "/sdcard/griffin_li.xml")
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
