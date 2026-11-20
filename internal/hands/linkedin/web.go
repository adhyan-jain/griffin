package linkedin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Page is the part of a browser the web scroller uses. *chromedp based drivers implement it; tests play one.
type Page interface {
	// Eval runs a script in the page and returns its JSON result.
	Eval(ctx context.Context, script string) (json.RawMessage, error)
}

// WebPost is one feed post as the page shows it.
type WebPost struct {
	Author   string   `json:"author"`
	Text     string   `json:"text"`
	Promoted bool     `json:"promoted"`
	URLs     []string `json:"urls"`
}

// feedScript reads the feed posts on screen. For each post it first clicks the post's own "see more" toggle
// (the only thing it ever clicks), then returns the author, the full text, whether it is an ad, and its links.
const feedScript = `(async () => {
  const out = [];
  const posts = document.querySelectorAll('div.feed-shared-update-v2, [data-urn^="urn:li:activity"]');
  for (const p of posts) {
    const more = p.querySelector('button.feed-shared-inline-show-more-text__see-more-less-toggle, button[aria-label*="see more" i]');
    if (more && /more/i.test(more.innerText || more.getAttribute('aria-label') || '')) { more.click(); await new Promise(r => setTimeout(r, 300)); }
    const actor = p.querySelector('.update-components-actor__title, .update-components-actor__name');
    const author = (actor ? (actor.querySelector('span[aria-hidden="true"]') || actor).innerText : '').trim().split('\n')[0];
    const sub = (p.querySelector('.update-components-actor__sub-description, .update-components-actor__meta') || {}).innerText || '';
    const promoted = /\b(promoted|sponsored)\b/i.test(sub) || !!p.querySelector('[aria-label*="Promoted" i], [aria-label*="Sponsored" i]');
    const body = p.querySelector('.update-components-text, .feed-shared-update-v2__description');
    const text = (body ? body.innerText : '').trim();
    const urls = Array.from(p.querySelectorAll('a[href^="http"]')).map(a => a.href).filter(h => !/linkedin\.com\/(in|company|feed|hashtag)\//.test(h) || /\/jobs\//.test(h));
    if (text) out.push({author, text, promoted, urls});
  }
  return out;
})()`

// WebScroller scrolls the LinkedIn feed in a browser window on the laptop and keeps only real job posts.
type WebScroller struct {
	Page       Page
	Classifier Classifier
	Pause      time.Duration // wait after each scroll; default 2 s
}

// Run scrolls for d and returns the posts that passed every filter, with the reasons others were dropped.
func (s WebScroller) Run(ctx context.Context, d time.Duration) ([]Post, Stats, error) {
	if s.Pause <= 0 {
		s.Pause = 2 * time.Second
	}
	var st Stats
	seen := map[string]bool{}
	var posts []Post
	last, same := "", 0
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		raw, err := s.Page.Eval(ctx, feedScript)
		if err != nil {
			return posts, st, fmt.Errorf("could not read the feed: %w", err)
		}
		var onScreen []WebPost
		if err := json.Unmarshal(raw, &onScreen); err != nil {
			return posts, st, fmt.Errorf("could not read the feed: %w", err)
		}
		if sig := sigOf(onScreen); sig == last {
			if same++; same >= 3 { // the feed stopped moving: its end, or a stuck page
				st.Stuck = true
				return posts, st, nil
			}
		} else {
			last, same = sig, 0
		}
		for _, p := range onScreen {
			key := strings.ToLower(p.Author + "|" + firstN(p.Text, 40))
			if seen[key] {
				continue
			}
			seen[key] = true
			if !Judge(p.Author, p.Text, p.Promoted, &st) {
				continue
			}
			st.Kept-- // only counted once the classifier agrees
			real, cerr := false, error(nil)
			if s.Classifier != nil {
				real, cerr = s.Classifier.IsRealJob(ctx, p.Author, p.Text)
			}
			switch {
			case s.Classifier == nil || cerr != nil:
				st.Unverified++
			case !real:
				st.NotReal++
			default:
				st.Kept++
				posts = append(posts, Post{Author: p.Author, Text: p.Text, URLs: p.URLs})
			}
		}
		if _, err := s.Page.Eval(ctx, `window.scrollBy(0, window.innerHeight * 0.8); null`); err != nil {
			return posts, st, err
		}
		if !sleep(ctx, s.Pause) {
			return posts, st, ctx.Err()
		}
	}
	return posts, st, nil
}

func firstN(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func sigOf(ps []WebPost) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Author + "|" + firstN(p.Text, 40) + ";")
	}
	return b.String()
}

// MoveToWorkspace puts the browser window on a Hyprland workspace without switching to it, so the scroll
// runs out of sight. It does nothing (and says so) where Hyprland is not available.
func MoveToWorkspace(ctx context.Context, workspace int, titleMatch string) error {
	if _, err := exec.LookPath("hyprctl"); err != nil {
		return fmt.Errorf("hyprctl not found: leaving the window where it opened")
	}
	arg := fmt.Sprintf("%d,title:(.*)%s(.*)", workspace, titleMatch)
	if out, err := exec.CommandContext(ctx, "hyprctl", "dispatch", "movetoworkspacesilent", arg).CombinedOutput(); err != nil {
		return fmt.Errorf("could not move the window: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ChromePage adapts a chromedp context to Page.
type ChromePage struct{ Ctx context.Context }

// Eval implements Page.
func (c ChromePage) Eval(ctx context.Context, script string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := chromedp.Run(c.Ctx, chromedp.Evaluate(script, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
	return raw, err
}
