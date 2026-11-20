// linkedin_scroll scrolls LinkedIn and saves the real job posts it saw. It only reads.
//
// It prefers the laptop: a Chrome window on Griffin's signed-in profile, kept on a Hyprland workspace out of
// sight. If the laptop route is not usable (no Chrome, not signed in to LinkedIn) it falls back to the
// attached Android phone.
//
// Usage: go run ./cmd/linkedin_scroll [-minutes 10] [-via auto|laptop|phone] [-workspace 8] [-out file.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/hands/linkedin"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/provider/gemini"
)

// visibleWindow is set by -show.
var visibleWindow bool

func main() {
	minutes := flag.Float64("minutes", 10, "how long to scroll")
	via := flag.String("via", "auto", "auto (laptop, else phone), laptop, or phone")
	workspace := flag.Int("workspace", 8, "Hyprland workspace for the laptop window")
	fromChrome := flag.String("from-chrome", "Default", "your own Chrome profile whose LinkedIn login Griffin's browser borrows (empty to disable)")
	home, _ := os.UserHomeDir()
	out := flag.String("out", filepath.Join(home, "Documents", "Griffin", "internships", "linkedin_posts.json"), "where to save the posts")
	dump := flag.Bool("dump-posts", false, "laptop only: print every post the reader sees (author, ad or not, length, links), unfiltered, to check the reader")
	inspect := flag.Bool("inspect", false, "laptop only: print what the feed's markup looks like, to fix the reader when LinkedIn changes it")
	show := flag.Bool("show", false, "laptop only: show the browser window (default: headless, nothing on screen)")
	flag.Parse()
	visibleWindow = *show
	if *dump {
		os.Exit(dumpPosts(*fromChrome, *workspace))
	}
	if *inspect {
		os.Exit(inspectFeed(*fromChrome))
	}

	model := &gemini.Client{APIKey: os.Getenv("GEMINI_API_KEY"), MaxOutputTokens: 10}
	if !model.Enabled() {
		fmt.Fprintln(os.Stderr, "set GEMINI_API_KEY: without it no post can be verified, so none would be saved")
		os.Exit(1)
	}
	cl := linkedin.Gemini{Client: model}
	d := time.Duration(*minutes * float64(time.Minute))
	ctx := context.Background()

	var posts []linkedin.Post
	var st linkedin.Stats
	var err error
	used := ""
	if *via == "auto" || *via == "laptop" {
		posts, st, err = viaLaptop(ctx, cl, d, *workspace, *fromChrome)
		used = "laptop"
		if err != nil && *via == "auto" {
			fmt.Fprintf(os.Stderr, "laptop route unavailable (%v); using the phone\n", err)
			posts, st, err, used = nil, linkedin.Stats{}, nil, ""
		}
	}
	if used == "" {
		posts, st, err = linkedin.Scroller{ADB: whatsapp.ExecADB{Serial: os.Getenv("ANDROID_SERIAL")}, Classifier: cl}.Run(ctx, d)
		used = "phone"
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "scroll stopped: %v (saving the %d posts found)\n", err, len(posts))
	}
	raw, _ := json.MarshalIndent(posts, "", "  ")
	_ = os.MkdirAll(filepath.Dir(*out), 0o755)
	if werr := os.WriteFile(*out, raw, 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		os.Exit(1)
	}
	fmt.Printf("%d real job posts saved to %s (via %s)\n%+v\n", len(posts), *out, used, st)
	if err != nil {
		os.Exit(1)
	}
}

// viaLaptop scrolls the feed in Griffin's own Chrome profile, signed in with the owner's own LinkedIn login.
func viaLaptop(ctx context.Context, cl linkedin.Classifier, d time.Duration, workspace int, fromChrome string) ([]linkedin.Post, linkedin.Stats, error) {
	profile := os.Getenv("BROWSER_PROFILE_DIR")
	if profile == "" {
		home, _ := os.UserHomeDir()
		profile = filepath.Join(home, ".griffin-host", "browser-profile")
	}
	page, closeFn, err := linkedin.OpenLaptop(ctx, linkedin.LaptopConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: profile, FromChrome: fromChrome, Visible: visibleWindow}, workspace)
	if err != nil {
		return nil, linkedin.Stats{}, err
	}
	defer closeFn()
	return linkedin.WebScroller{Page: page, Classifier: cl}.Run(ctx, d)
}

// inspectFeed loads the feed and prints the shape of what a post is made of: which containers exist and the
// class names inside the first ones. Nothing is saved.
func inspectFeed(fromChrome string) int {
	ctx := context.Background()
	profile := filepath.Join(os.Getenv("HOME"), ".griffin-host", "browser-profile")
	if !browser.ClearStaleLock(profile) {
		fmt.Fprintln(os.Stderr, "the browser profile is in use")
		return 1
	}
	if fromChrome != "" {
		_, _ = browser.BorrowCookies(filepath.Join(os.Getenv("HOME"), ".config", "google-chrome", fromChrome), profile, []string{"linkedin.com"})
	}
	chrome, err := browser.NewChrome(ctx, browser.ChromeConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: profile, Visible: true, SystemKeyring: fromChrome != ""})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer chrome.Close()
	var out string
	err = chromedp.Run(chrome.Context(), chromedp.Navigate("https://www.linkedin.com/feed/"), chromedp.Sleep(7*time.Second), chromedp.Evaluate(`(() => {
  const r = [];
  r.push('url ' + location.href + ' title ' + document.title);
  for (const sel of ['div.feed-shared-update-v2', '[data-urn^="urn:li:activity"]', '[data-id^="urn:li:activity"]', 'div[role="listitem"]', 'main', 'article', '[componentkey]']) r.push(sel + ' => ' + document.querySelectorAll(sel).length);
  document.querySelectorAll('div[role="listitem"]').forEach((item, i) => {
    const links = Array.from(item.querySelectorAll('a[href*="/in/"], a[href*="/company/"], a[href*="/school/"]')).slice(0, 2).map(a => a.getAttribute('href').split('?')[0].slice(0, 50) + ' "' + a.innerText.trim().split('\\n')[0].slice(0, 30) + '"');
    const btns = Array.from(item.querySelectorAll('button, [role="button"]')).map(b => (b.getAttribute('aria-label') || b.innerText || '').trim().replace(/\\s+/g, ' ').slice(0, 30)).filter(Boolean).slice(0, 8);
    const more = Array.from(item.querySelectorAll('button, span, a, [role="button"]')).filter(e => /^(…|\\.\\.\\.)?\\s*(see )?more$/i.test((e.innerText || '').trim())).map(e => e.tagName + (e.getAttribute('role') ? '[' + e.getAttribute('role') + ']' : ''));
    r.push('#' + i + ' text: ' + item.innerText.slice(0, 220).replace(/\\n+/g, ' / '));
    r.push('   links: ' + links.join(' ; '));
    r.push('   buttons: ' + btns.join(' | '));
    r.push('   more-controls: ' + more.join(','));
  });
  return r.join('\n');
})()`, &out))
	fmt.Println(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// dumpPosts reads the feed a few screens deep and prints each post unfiltered, so the reader can be checked
// against what is really on the page (is "more" being expanded, are authors and ads right).
func dumpPosts(fromChrome string, workspace int) int {
	ctx := context.Background()
	profile := filepath.Join(os.Getenv("HOME"), ".griffin-host", "browser-profile")
	page, closeFn, err := linkedin.OpenLaptop(ctx, linkedin.LaptopConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: profile, FromChrome: fromChrome, Visible: visibleWindow}, workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeFn()
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		posts, err := linkedin.ReadFeed(ctx, page)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, p := range posts {
			k := p.Author + p.Text[:min(30, len(p.Text))]
			if seen[k] {
				continue
			}
			seen[k] = true
			t := strings.ReplaceAll(p.Text, "\n", " ")
			end := t[max(0, len(t)-45):]
			fmt.Printf("author=%q ad=%v len=%d urls=%d ends=%q\n", p.Author, p.Promoted, len(p.Text), len(p.URLs), end)
		}
		_ = linkedin.Scroll(ctx, page)
		time.Sleep(2500 * time.Millisecond)
	}
	return 0
}
