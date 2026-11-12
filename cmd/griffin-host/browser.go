package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/griffin/core/internal/hands"
	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/provider/gemini"
)

// Configuration for the browser hand (environment):
//
//	HOST_BROWSER          "on" to let Griffin use this computer's browser for goals (default off)
//	BROWSER_PROFILE_DIR   folder holding the sign-ins (default ~/.griffin-host/browser-profile)
//	BROWSER_MODEL         Gemini model that chooses the steps (default gemini-flash-latest)
//	GEMINI_API_KEY        the key it uses
//	BROWSER_NO_SANDBOX    "true" where Chrome cannot run its sandbox (containers)

func browserProfileDir() string {
	return envOr("BROWSER_PROFILE_DIR", filepath.Join(stateDir(), "browser-profile"))
}

func browserConfig(visible bool) browser.ChromeConfig {
	return browser.ChromeConfig{
		BrowserPath: os.Getenv("WHATSAPP_BROWSER"),
		ProfileDir:  browserProfileDir(),
		Visible:     visible,
		NoSandbox:   strings.ToLower(os.Getenv("BROWSER_NO_SANDBOX")) == "true",
	}
}

// newBrowserService returns the browser hand, or nil when it is off or cannot work here (no browser, no key).
func newBrowserService(logger *slog.Logger) *hands.BrowserService {
	if strings.ToLower(envOr("HOST_BROWSER", "off")) != "on" {
		return nil
	}
	if _, err := whatsapp.FindBrowser(os.Getenv("WHATSAPP_BROWSER")); err != nil {
		logger.Warn("the browser hand is unavailable: no Chrome, Chromium or Brave", "reason", err.Error())
		return nil
	}
	model := &gemini.Client{APIKey: os.Getenv("GEMINI_API_KEY"), Model: envOr("BROWSER_MODEL", "gemini-flash-latest"), MaxOutputTokens: 700}
	if !model.Enabled() {
		logger.Warn("the browser hand is unavailable: set GEMINI_API_KEY so it can choose steps")
		return nil
	}
	return &hands.BrowserService{
		Open: func(ctx context.Context) (browser.Driver, func(), error) {
			c, err := browser.NewChrome(ctx, browserConfig(false))
			if err != nil {
				return nil, nil, err
			}
			return c, c.Close, nil
		},
		Decider: &browser.ModelDecider{Model: model},
		Stage:   browser.Stage{Dir: filepath.Join(stateDir(), "browser-staged")},
	}
}

// browserLogin opens a visible browser on the same profile Griffin uses, so the owner can sign in to Gmail,
// LinkedIn and anything else once. The sign-ins stay in the profile; Griffin never sees the passwords.
func browserLogin(ctx context.Context, logger *slog.Logger) error {
	if _, err := whatsapp.FindBrowser(os.Getenv("WHATSAPP_BROWSER")); err != nil {
		return errors.New("no Chrome, Chromium or Brave found (set WHATSAPP_BROWSER)")
	}
	fmt.Println("Opening a browser window with Griffin's own profile.")
	fmt.Println("Sign in to the sites you want Griffin to use (Gmail, LinkedIn, ...), then come back here and press Enter.")
	fmt.Println("Stop griffin-host first if it is running: the profile can only be open once.")
	c, err := browser.NewChrome(ctx, browserConfig(true))
	if err != nil {
		return err
	}
	defer c.Close()
	done := make(chan struct{})
	go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	fmt.Println("Saved. Set HOST_BROWSER=on and restart griffin-host to let Griffin use it.")
	return nil
}
