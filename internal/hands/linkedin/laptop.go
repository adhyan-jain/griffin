package linkedin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/griffin/core/internal/hands/browser"
)

// LaptopConfig says which browser profile the laptop route uses and whose login it borrows.
type LaptopConfig struct {
	BrowserPath string
	ProfileDir  string // Griffin's own browser profile folder
	FromChrome  string // the owner's Chrome profile whose LinkedIn login is borrowed ("" to use Griffin's own login)
	// Visible shows the browser window. The default is a headless browser: no window, nothing on screen.
	Visible bool
}

// OpenLaptop opens the laptop browser on LinkedIn's feed, signed in as the owner, and returns its page and a
// closer. It fails, before doing anything else, when the profile is in use, Chrome is missing, or LinkedIn is
// not signed in, so the caller can fall back to the phone.
func OpenLaptop(ctx context.Context, cfg LaptopConfig, workspace int) (Page, func(), error) {
	if _, err := os.Stat(cfg.ProfileDir); err != nil {
		return nil, nil, fmt.Errorf("no browser profile (run griffin-host browser-login)")
	}
	if !browser.ClearStaleLock(cfg.ProfileDir) {
		return nil, nil, fmt.Errorf("the browser profile is in use")
	}
	if cfg.FromChrome != "" {
		home, _ := os.UserHomeDir()
		src := filepath.Join(home, ".config", "google-chrome", cfg.FromChrome)
		if n, err := browser.BorrowCookies(src, cfg.ProfileDir, []string{"linkedin.com"}); err != nil {
			fmt.Fprintf(os.Stderr, "could not borrow your LinkedIn login (%v); using Griffin's own\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "borrowed %d LinkedIn cookies from your Chrome profile %q\n", n, cfg.FromChrome)
		}
	}
	chrome, err := browser.NewChrome(ctx, browser.ChromeConfig{BrowserPath: cfg.BrowserPath, ProfileDir: cfg.ProfileDir, Visible: cfg.Visible, SystemKeyring: cfg.FromChrome != ""})
	if err != nil {
		return nil, nil, err
	}
	var url, title string
	if err := chromedp.Run(chrome.Context(), chromedp.Navigate("https://www.linkedin.com/feed/"), chromedp.Sleep(4*time.Second), chromedp.Location(&url), chromedp.Title(&title)); err != nil {
		chrome.Close()
		return nil, nil, err
	}
	if strings.Contains(url, "/login") || strings.Contains(url, "/authwall") || strings.Contains(url, "/checkpoint") {
		chrome.Close()
		return nil, nil, fmt.Errorf("not signed in to LinkedIn (landed on %s)", strings.SplitN(url, "?", 2)[0])
	}
	if workspace > 0 && cfg.Visible { // a headless browser has no window to move
		if err := MoveToWorkspace(ctx, workspace, "LinkedIn"); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	return ChromePage{Ctx: chrome.Context()}, chrome.Close, nil
}
