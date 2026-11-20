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

func main() {
	minutes := flag.Float64("minutes", 10, "how long to scroll")
	via := flag.String("via", "auto", "auto (laptop, else phone), laptop, or phone")
	workspace := flag.Int("workspace", 8, "Hyprland workspace for the laptop window")
	home, _ := os.UserHomeDir()
	out := flag.String("out", filepath.Join(home, "Documents", "Griffin", "internships", "linkedin_posts.json"), "where to save the posts")
	flag.Parse()

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
		posts, st, err = viaLaptop(ctx, cl, d, *workspace)
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

// viaLaptop scrolls the feed in Griffin's own Chrome profile. It returns an error, before scrolling anything,
// when Chrome is missing or the profile is not signed in to LinkedIn.
func viaLaptop(ctx context.Context, cl linkedin.Classifier, d time.Duration, workspace int) ([]linkedin.Post, linkedin.Stats, error) {
	profile := os.Getenv("BROWSER_PROFILE_DIR")
	if profile == "" {
		home, _ := os.UserHomeDir()
		profile = filepath.Join(home, ".griffin-host", "browser-profile")
	}
	if _, err := os.Stat(profile); err != nil {
		return nil, linkedin.Stats{}, fmt.Errorf("no signed-in browser profile (run griffin-host browser-login)")
	}
	// Chrome keeps a SingletonLock in a profile that is open. The host service holds Griffin's profile while it
	// runs, and a second Chrome cannot share it, so say so instead of failing halfway.
	if _, err := os.Lstat(filepath.Join(profile, "SingletonLock")); err == nil {
		return nil, linkedin.Stats{}, fmt.Errorf("the browser profile is in use (griffin-host is running); the laptop route needs the host to own the scroll")
	}
	chrome, err := browser.NewChrome(ctx, browser.ChromeConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: profile, Visible: true})
	if err != nil {
		return nil, linkedin.Stats{}, err
	}
	defer chrome.Close()
	var url, title string
	if err := chromedp.Run(chrome.Context(), chromedp.Navigate("https://www.linkedin.com/feed/"), chromedp.Sleep(4*time.Second), chromedp.Location(&url), chromedp.Title(&title)); err != nil {
		return nil, linkedin.Stats{}, err
	}
	if strings.Contains(url, "/login") || strings.Contains(url, "/authwall") || strings.Contains(url, "/checkpoint") {
		return nil, linkedin.Stats{}, fmt.Errorf("the browser profile is not signed in to LinkedIn (it landed on %s, titled %q)", strings.SplitN(url, "?", 2)[0], title)
	}
	if err := linkedin.MoveToWorkspace(ctx, workspace, "LinkedIn"); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return linkedin.WebScroller{Page: linkedin.ChromePage{Ctx: chrome.Context()}, Classifier: cl}.Run(ctx, d)
}
