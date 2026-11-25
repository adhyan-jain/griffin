package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	model := &gemini.Client{APIKey: os.Getenv("GEMINI_API_KEY"), Model: envOr("BROWSER_MODEL", gemini.DefaultModel), MaxOutputTokens: 700}
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

// browserLogin opens a visible, normal browser window on Griffin's profile (free of automation flags)
// so the owner can sign in to Gmail, LinkedIn, and other services without Google/LinkedIn blocking the session.
func browserLogin(ctx context.Context, logger *slog.Logger) error {
	path, err := whatsapp.FindBrowser(os.Getenv("WHATSAPP_BROWSER"))
	if err != nil {
		return errors.New("no Chrome, Chromium or Brave found (set WHATSAPP_BROWSER)")
	}

	profileDir := browserProfileDir()
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return fmt.Errorf("cannot create the browser profile folder: %w", err)
	}

	// Auto-pause griffin-host background service if active so profile lock is released
	wasServiceRunning := false
	if err := exec.Command("systemctl", "--user", "is-active", "--quiet", "griffin-host").Run(); err == nil {
		wasServiceRunning = true
		fmt.Println("Temporarily pausing griffin-host background service to release profile lock...")
		_ = exec.Command("systemctl", "--user", "stop", "griffin-host").Run()
		time.Sleep(1 * time.Second)
	}

	fmt.Println("Opening Chrome with Griffin's profile (clean mode, no automation flags).")
	fmt.Println("Login tabs for Google Accounts and LinkedIn are opening.")
	fmt.Println("Sign in, then close the browser window or press Enter here when finished.")

	args := []string{
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--disable-save-password-bubble",
		"--disable-features=AutofillServerCommunication,PasswordManager,PasswordGeneration",
		"--disable-autofill",
		"--disable-single-click-autofill",
		"--password-store=basic",
		"https://accounts.google.com",
		"https://www.linkedin.com/login",
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		if wasServiceRunning {
			_ = exec.Command("systemctl", "--user", "start", "griffin-host").Run()
		}
		return fmt.Errorf("could not start browser: %w", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		close(done)
	}()

	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	select {
	case <-done:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	case err := <-cmdDone:
		if err != nil {
			logger.Debug("browser process exited", "err", err)
		}
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}

	fmt.Println("Profile saved cleanly.")
	if wasServiceRunning {
		fmt.Println("Resuming griffin-host background service...")
		_ = exec.Command("systemctl", "--user", "start", "griffin-host").Run()
		fmt.Println("griffin-host is running.")
	} else {
		fmt.Println("Set HOST_BROWSER=on and restart griffin-host to let Griffin use it.")
	}
	return nil
}
