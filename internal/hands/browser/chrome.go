package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/whatsapp"
)

// Chrome drives a real Chrome or Chromium that keeps its own profile folder, so the sites the owner has signed
// in to stay signed in. It runs headless (in the background) unless Visible is set, which the owner uses once to
// sign in to a site.
type Chrome struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// ChromeConfig says where the browser lives and whether to show it.
type ChromeConfig struct {
	BrowserPath string // empty finds Chrome, Chromium or Brave
	ProfileDir  string
	Visible     bool
	// NoSandbox is for containers and some Linux setups that cannot run Chrome's sandbox.
	NoSandbox bool
}

// NewChrome starts the browser. Close it when done: it holds the profile open.
func NewChrome(ctx context.Context, cfg ChromeConfig) (*Chrome, error) {
	path, err := whatsapp.FindBrowser(cfg.BrowserPath)
	if err != nil {
		return nil, err
	}
	if cfg.ProfileDir == "" {
		return nil, fmt.Errorf("no browser profile folder is configured")
	}
	if err := os.MkdirAll(cfg.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the browser profile folder: %w", err)
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(path),
		chromedp.UserDataDir(cfg.ProfileDir),
		chromedp.Flag("headless", !cfg.Visible),
		chromedp.Flag("mute-audio", true),
		chromedp.WindowSize(1280, 900),
	)
	if cfg.NoSandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	actx, cancelA := chromedp.NewExecAllocator(ctx, opts...)
	bctx, cancelB := chromedp.NewContext(actx)
	cancel := func() { _ = chromedp.Cancel(bctx); cancelB(); cancelA() }
	if err := chromedp.Run(bctx); err != nil {
		cancel()
		return nil, fmt.Errorf("could not start the browser (is its profile open in another window?): %w", err)
	}
	return &Chrome{ctx: bctx, cancel: cancel}, nil
}

// Close shuts the browser down and releases the profile.
func (c *Chrome) Close() { c.cancel() }

// tagScript numbers the visible things a person could act on and describes them. It runs before every snapshot
// and before every click or type, so a ref always points at what the agent was shown. A password field is
// listed but its value is never read.
const tagScript = `(() => {
  document.querySelectorAll('[data-griffin-ref]').forEach(e => e.removeAttribute('data-griffin-ref'));
  const sel = 'a[href],button,input:not([type=hidden]),textarea,select,[role=button],[role=link],[role=textbox],[contenteditable=""],[contenteditable=true]';
  const out = []; let n = 0;
  for (const e of document.querySelectorAll(sel)) {
    const r = e.getBoundingClientRect(), cs = getComputedStyle(e);
    if (r.width < 2 || r.height < 2 || cs.visibility === 'hidden' || cs.display === 'none') continue;
    if (n >= 150) break;
    n++; e.setAttribute('data-griffin-ref', String(n));
    const tag = e.tagName, type = (e.type || '').toLowerCase();
    let role = e.getAttribute('role');
    if (!role) role = tag === 'A' ? 'link' : tag === 'BUTTON' ? 'button' : tag === 'SELECT' ? 'select'
      : tag === 'INPUT' ? (type === 'submit' || type === 'button' ? 'button' : (type === 'checkbox' || type === 'radio') ? type : 'textbox') : 'textbox';
    const name = (e.getAttribute('aria-label') || e.innerText || e.placeholder || e.title || e.getAttribute('name') || '').trim().replace(/\s+/g, ' ').slice(0, 80);
    const value = (tag === 'INPUT' || tag === 'TEXTAREA') && type !== 'password' ? String(e.value || '').slice(0, 200) : '';
    out.push({ref: String(n), role, name, value});
  }
  return {url: location.href, title: document.title, text: (document.body ? document.body.innerText : '').slice(0, 6000), elements: out};
})()`

// Snapshot numbers the page's elements and returns what the agent may see.
func (c *Chrome) Snapshot(ctx context.Context) (*Page, error) {
	var raw json.RawMessage
	if err := c.run(ctx, chromedp.Evaluate(tagScript, &raw)); err != nil {
		return nil, err
	}
	var p Page
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Chrome) run(ctx context.Context, actions ...chromedp.Action) error {
	rctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(rctx, actions...)
}

// Do performs one step. For click and type it re-numbers the page first and refuses to act unless the element
// at the step's ref still has the role and name it had when the step was chosen.
func (c *Chrome) Do(ctx context.Context, s domain.BrowserStep) error {
	switch s.Kind {
	case domain.StepGoto:
		if err := domain.ValidateBrowserURL(s.URL); err != nil {
			return err
		}
		return c.run(ctx, chromedp.Navigate(s.URL), chromedp.WaitReady("body"), chromedp.Sleep(700*time.Millisecond))
	case domain.StepClick, domain.StepType:
		page, err := c.Snapshot(ctx)
		if err != nil {
			return err
		}
		el := page.find(s.Ref)
		if el == nil || (s.Name != "" && (el.Role != s.Role || el.Name != s.Name)) {
			return fmt.Errorf("the page changed: element %s is no longer the %s %q", s.Ref, s.Role, s.Name)
		}
		sel := fmt.Sprintf(`[data-griffin-ref="%s"]`, s.Ref)
		if s.Kind == domain.StepClick {
			return c.run(ctx, chromedp.Click(sel, chromedp.ByQuery), chromedp.Sleep(900*time.Millisecond))
		}
		return c.typeText(ctx, sel, s.Text)
	case domain.StepPress:
		key, ok := keyNames[strings.ToLower(s.Text)]
		if !ok {
			return fmt.Errorf("unknown key %q", s.Text)
		}
		return c.run(ctx, chromedp.KeyEvent(key), chromedp.Sleep(600*time.Millisecond))
	case domain.StepScroll:
		return c.run(ctx, chromedp.Evaluate(`window.scrollBy(0, window.innerHeight * 0.8)`, nil), chromedp.Sleep(300*time.Millisecond))
	case domain.StepWait:
		return c.run(ctx, chromedp.Sleep(1500*time.Millisecond))
	}
	return fmt.Errorf("unknown step %q", s.Kind)
}

// typeText clicks the field and types text into it. chromedp turns "\n" and "\r" into an Enter press, which
// submits a chat box or compose form before the owner has approved anything, so each line is typed on its own and
// Shift+Enter (a soft line break) joins them. A single-line <input> cannot hold a line break, so text with one is
// refused rather than typed.
func (c *Chrome) typeText(ctx context.Context, sel, text string) error {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		var singleLine bool
		check := fmt.Sprintf(`document.querySelector(%q).tagName === 'INPUT'`, sel)
		if err := c.run(ctx, chromedp.Evaluate(check, &singleLine)); err != nil {
			return err
		}
		if singleLine {
			return fmt.Errorf("this field takes one line, so text with a line break cannot be typed into it")
		}
	}
	actions := []chromedp.Action{chromedp.Click(sel, chromedp.ByQuery)}
	for i, line := range lines {
		if i > 0 {
			actions = append(actions, chromedp.KeyEvent(kb.Enter, chromedp.KeyModifiers(input.ModifierShift)))
		}
		if line != "" {
			actions = append(actions, chromedp.SendKeys(sel, line, chromedp.ByQuery))
		}
	}
	return c.run(ctx, actions...)
}

var keyNames = map[string]string{
	"enter": kb.Enter, "tab": kb.Tab, "escape": kb.Escape, "esc": kb.Escape, "down": kb.ArrowDown, "up": kb.ArrowUp,
	"pagedown": kb.PageDown, "pageup": kb.PageUp, "backspace": kb.Backspace,
}
