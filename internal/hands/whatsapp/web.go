package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/griffin/core/internal/domain"
)

// Selectors locate the parts of WhatsApp Web the driver uses. Each field is a CSS selector list. They are
// all in one place on purpose: when WhatsApp changes its page, this struct (and `griffin-host
// whatsapp-check`, which reports which of them match) is the only thing to touch.
type Selectors struct {
	ChatList      string // present once signed in
	QR            string // present while signed out
	Search        string
	Row           string // a chat list row (also used for search results)
	RowTitle      string // inside a row; the chat name is its title attribute or text
	RowPreview    string
	RowUnread     string
	Composer      string
	InMsg         string
	OutMsg        string
	MsgText       string
	MsgMeta       string // element carrying data-pre-plain-text, attribute holds "[time, date] Name: "
	Pending       string // inside an outgoing message that has not been sent yet (the clock icon)
	InvalidDialog string
	HeaderTitle   string
}

// DefaultSelectors matches WhatsApp Web as last seen. They are best-effort: WhatsApp can change them
// without notice, which is why whatsapp-check exists.
func DefaultSelectors() Selectors {
	return Selectors{
		ChatList:      `#pane-side, [data-testid="chat-list"]`,
		QR:            `canvas[aria-label*="QR" i], [data-testid="qrcode"], div[data-ref]`,
		Search:        `div[contenteditable="true"][role="textbox"][aria-label*="Search" i], [data-testid="chat-list-search"], div[contenteditable="true"][data-tab="3"]`,
		Row:           `#pane-side [role="listitem"], #pane-side [data-testid="cell-frame-container"]`,
		RowTitle:      `span[title]`,
		RowPreview:    `span[data-testid="last-msg"], div[role="gridcell"] span[dir="ltr"], span.preview`,
		RowUnread:     `span[aria-label*="unread" i]`,
		Composer:      `footer div[contenteditable="true"][role="textbox"], [data-testid="conversation-compose-box-input"], footer div[contenteditable="true"][data-tab="10"]`,
		InMsg:         `div.message-in`,
		OutMsg:        `div.message-out`,
		MsgText:       `span.selectable-text, [data-testid="selectable-text"]`,
		MsgMeta:       `[data-pre-plain-text]`,
		Pending:       `span[data-icon="msg-time"]`,
		InvalidDialog: `div[data-animate-modal-popup="true"], [data-testid="popup-contents"]`,
		HeaderTitle:   `#main header span[title], [data-testid="conversation-info-header-chat-title"]`,
	}
}

// WebConfig configures the WhatsApp Web driver.
type WebConfig struct {
	// BrowserPath is a Chrome or Chromium binary. Empty means find one on PATH.
	BrowserPath string
	// ProfileDir holds the browser profile, which is where the WhatsApp login lives. Keep it private.
	ProfileDir string
	// Headless runs without a window. WhatsApp sometimes dislikes it; set false to see what happens.
	Headless bool
	// BaseURL defaults to https://web.whatsapp.com (tests point it at a stand-in page).
	BaseURL   string
	Selectors Selectors
	// LoadTimeout is how long to wait for the page to show chats or a QR code. Default 45 s.
	LoadTimeout time.Duration
	// ConfirmTimeout is how long to wait to see a sent message leave the "pending" state. Default 25 s.
	ConfirmTimeout time.Duration
	Logger         *slog.Logger
}

// Web drives WhatsApp Web in a local browser. One operation at a time: the profile can only be open in
// one browser, so operations are serialized.
type Web struct {
	cfg WebConfig
	mu  sync.Mutex
}

// NewWeb builds the driver, applying defaults.
func NewWeb(cfg WebConfig) *Web {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://web.whatsapp.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Selectors == (Selectors{}) {
		cfg.Selectors = DefaultSelectors()
	}
	if cfg.LoadTimeout <= 0 {
		cfg.LoadTimeout = 45 * time.Second
	}
	if cfg.ConfirmTimeout <= 0 {
		cfg.ConfirmTimeout = 25 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Web{cfg: cfg}
}

// Name implements Driver.
func (w *Web) Name() string { return "whatsapp-web" }

// Device implements Driver.
func (w *Web) Device() string { return "laptop" }

// FindBrowser returns the browser to use, or an error naming what was looked for.
func FindBrowser(configured string) (string, error) {
	if configured != "" {
		if p, err := exec.LookPath(configured); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("browser %q was not found", configured)
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "brave-browser", "brave", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome, Chromium or Brave found on PATH (set WHATSAPP_BROWSER)")
}

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

func (w *Web) allocator(ctx context.Context, headless bool) (context.Context, context.CancelFunc, error) {
	path, err := FindBrowser(w.cfg.BrowserPath)
	if err != nil {
		return nil, nil, notReady("%v", err)
	}
	if w.cfg.ProfileDir == "" {
		return nil, nil, notReady("no browser profile folder is configured")
	}
	if err := os.MkdirAll(w.cfg.ProfileDir, 0o700); err != nil {
		return nil, nil, notReady("cannot create the browser profile folder: %v", err)
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(path),
		chromedp.UserDataDir(w.cfg.ProfileDir),
		chromedp.Flag("headless", headless),
		chromedp.Flag("hide-scrollbars", false),
		chromedp.Flag("mute-audio", true),
		chromedp.WindowSize(1280, 900),
	)
	if headless {
		opts = append(opts, chromedp.UserAgent(defaultUserAgent))
	}
	actx, cancelA := chromedp.NewExecAllocator(ctx, opts...)
	bctx, cancelB := chromedp.NewContext(actx)
	// Wait for the browser process to exit: it holds the profile open, and the next operation (or the
	// owner signing in) needs the profile free.
	cancel := func() { _ = chromedp.Cancel(bctx); cancelB(); cancelA() }
	if err := chromedp.Run(bctx); err != nil {
		cancel()
		return nil, nil, notReady("could not start the browser (is WhatsApp's browser profile already open in another window?): %v", err)
	}
	return bctx, cancel, nil
}

// session is one browser session on the WhatsApp Web page.
type session struct {
	ctx context.Context
	w   *Web
}

// Do implements Driver.
func (w *Web) Do(ctx context.Context, act *domain.MessagingAction) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	bctx, cancel, err := w.allocator(ctx, w.cfg.Headless)
	if err != nil {
		return nil, err
	}
	defer cancel()
	s := &session{ctx: bctx, w: w}

	if err := s.load(); err != nil {
		return nil, err
	}
	switch act.Op {
	case domain.MessagingUnread:
		chats, err := s.unreadChats()
		if err != nil {
			return nil, err
		}
		return &Result{Chats: chats}, nil
	case domain.MessagingReadRecent:
		if err := s.openChat(act); err != nil {
			return nil, err
		}
		msgs, err := s.recentMessages(act.Limit)
		if err != nil {
			return nil, err
		}
		return &Result{Messages: msgs}, nil
	case domain.MessagingSend:
		if err := s.openChat(act); err != nil {
			return nil, err
		}
		if err := s.send(act.Text); err != nil {
			return nil, err
		}
		return &Result{Sent: true}, nil
	}
	return nil, fmt.Errorf("unsupported op %q", act.Op)
}

// load opens WhatsApp Web and waits until it shows the chat list. A QR code means signed out.
func (s *session) load() error {
	if err := chromedp.Run(s.ctx, chromedp.Navigate(s.w.cfg.BaseURL)); err != nil {
		return notReady("could not open WhatsApp Web: %v", err)
	}
	sel := s.w.cfg.Selectors
	state, err := poll(s.ctx, s.w.cfg.LoadTimeout, 500*time.Millisecond, func() (string, bool, error) {
		var st string
		err := s.eval(&st, `(c,q)=>document.querySelector(c)?"chats":(document.querySelector(q)?"qr":"")`, sel.ChatList, sel.QR)
		return st, st != "", err
	})
	if err != nil {
		return notReady("WhatsApp Web did not finish loading")
	}
	if state == "qr" {
		return notReady("WhatsApp Web is not signed in on this computer; run `griffin-host whatsapp-login` and scan the code with your phone")
	}
	return nil
}

// openChat opens the chat for the action: by phone number when given (the reliable way), otherwise by
// an exact name match in the chat list.
func (s *session) openChat(act *domain.MessagingAction) error {
	if act.Phone != "" {
		return s.openByPhone(act.Phone)
	}
	return s.openByName(act.Contact)
}

func (s *session) openByPhone(phone string) error {
	sel := s.w.cfg.Selectors
	if err := chromedp.Run(s.ctx, chromedp.Navigate(s.w.cfg.BaseURL+"/send?phone="+url.QueryEscape(phone))); err != nil {
		return notReady("could not open the chat: %v", err)
	}
	outcome, err := poll(s.ctx, s.w.cfg.LoadTimeout, 400*time.Millisecond, func() (string, bool, error) {
		var o string
		err := s.eval(&o, `(c,d)=>{const m=document.querySelector(c);if(m&&m.offsetParent!==null)return "chat";const e=document.querySelector(d);if(e&&/invalid|not on whatsapp|isn.t on whatsapp/i.test(e.innerText))return "invalid";return ""}`, sel.Composer, sel.InvalidDialog)
		return o, o != "", err
	})
	if err != nil {
		return notReady("the chat did not open")
	}
	if outcome == "invalid" {
		return fmt.Errorf("%w: WhatsApp says %s is not a valid WhatsApp number", ErrContactNotFound, phone)
	}
	return nil
}

// rowInfo is a chat list row as seen by the page script.
type rowInfo struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	Preview string `json:"preview"`
	Unread  int    `json:"unread"`
}

const rowsScript = `(row,title,prev,unread)=>{
  return JSON.stringify(Array.from(document.querySelectorAll(row)).map((el,i)=>{
    const t=el.querySelector(title);
    const p=el.querySelector(prev);
    const u=el.querySelector(unread);
    let n=0; if(u){const m=(u.getAttribute("aria-label")||u.innerText||"").match(/\d+/); n=m?parseInt(m[0],10):1;}
    return {index:i,title:t?(t.getAttribute("title")||t.innerText||"").trim():"",preview:p?(p.innerText||"").trim():"",unread:n};
  }));
}`

func (s *session) rows() ([]rowInfo, error) {
	sel := s.w.cfg.Selectors
	var raw string
	if err := s.eval(&raw, rowsScript, sel.Row, sel.RowTitle, sel.RowPreview, sel.RowUnread); err != nil {
		return nil, err
	}
	var rows []rowInfo
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *session) openByName(name string) error {
	sel := s.w.cfg.Selectors
	if err := chromedp.Run(s.ctx,
		chromedp.Click(sel.Search, chromedp.ByQuery),
		chromedp.ActionFunc(func(c context.Context) error { return input.InsertText(name).Do(c) }),
	); err != nil {
		return notReady("could not use the search box: %v", err)
	}
	// Results arrive asynchronously: wait until the list settles on the same rows twice.
	var last string
	var stable int
	_, err := poll(s.ctx, 10*time.Second, 400*time.Millisecond, func() (struct{}, bool, error) {
		rows, err := s.rows()
		if err != nil {
			return struct{}{}, false, err
		}
		b, _ := json.Marshal(rows)
		if string(b) == last && len(rows) > 0 {
			stable++
		} else {
			stable, last = 0, string(b)
		}
		return struct{}{}, stable >= 2, nil
	})
	if err != nil {
		return fmt.Errorf("%w: no chat matched %q", ErrContactNotFound, name)
	}
	rows, err := s.rows()
	if err != nil {
		return notReady("could not read the search results: %v", err)
	}
	want := normalizeName(name)
	var matches []rowInfo
	for _, r := range rows {
		if normalizeName(r.Title) == want {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return fmt.Errorf("%w: no chat is named exactly %q", ErrContactNotFound, name)
	case 1:
	default:
		return fmt.Errorf("%w: %q matches %d chats; use the phone number", ErrAmbiguousContact, name, len(matches))
	}
	if err := s.eval(nil, `(row,i)=>{const el=document.querySelectorAll(row)[i]; el.scrollIntoView(); el.click();}`, sel.Row, matches[0].Index); err != nil {
		return notReady("could not open the chat: %v", err)
	}
	// Confirm the right chat is open before anything is typed into it.
	ok, err := poll(s.ctx, 10*time.Second, 300*time.Millisecond, func() (bool, bool, error) {
		var t string
		if err := s.eval(&t, `(h)=>{const e=document.querySelector(h);return e?(e.getAttribute("title")||e.innerText||""):""}`, sel.HeaderTitle); err != nil {
			return false, false, err
		}
		return normalizeName(t) == want, normalizeName(t) == want, nil
	})
	if err != nil || !ok {
		return notReady("the chat with %q did not open", name)
	}
	return nil
}

// typeText puts text in the composer and checks it landed exactly. It returns ErrNotReady-class errors
// only: nothing has been sent yet.
func (s *session) typeText(text string) error {
	sel := s.w.cfg.Selectors
	if err := chromedp.Run(s.ctx, chromedp.Focus(sel.Composer, chromedp.ByQuery)); err != nil {
		return notReady("the message box is not available: %v", err)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			if err := chromedp.Run(s.ctx, chromedp.ActionFunc(func(c context.Context) error { return input.InsertText(line).Do(c) })); err != nil {
				s.clearComposer()
				return notReady("typing failed: %v", err)
			}
		}
		if i < len(lines)-1 {
			// Shift+Enter is a line break; plain Enter would send.
			if err := chromedp.Run(s.ctx, chromedp.KeyEvent(kb.Enter, chromedp.KeyModifiers(input.ModifierShift))); err != nil {
				s.clearComposer()
				return notReady("typing failed: %v", err)
			}
		}
	}
	var got string
	if err := s.eval(&got, `(c)=>{const e=document.querySelector(c);return e?e.innerText:""}`, sel.Composer); err != nil {
		s.clearComposer()
		return notReady("could not check what was typed: %v", err)
	}
	if normalizeBody(got) != normalizeBody(text) {
		s.clearComposer()
		return notReady("the message box did not hold exactly the text to send")
	}
	return nil
}

func (s *session) clearComposer() {
	sel := s.w.cfg.Selectors
	_ = s.eval(nil, `(c)=>{const e=document.querySelector(c);if(e){e.focus();document.execCommand("selectAll");document.execCommand("delete");}}`, sel.Composer)
}

func normalizeBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type outState struct {
	Count   int    `json:"count"`
	Last    string `json:"last"`
	Pending bool   `json:"pending"`
}

func (s *session) outState() (outState, error) {
	sel := s.w.cfg.Selectors
	var raw string
	err := s.eval(&raw, `(out,txt,pend)=>{const m=Array.from(document.querySelectorAll(out));const l=m[m.length-1];
      const t=l?Array.from(l.querySelectorAll(txt)).map(e=>e.innerText).join("\n"):"";
      return JSON.stringify({count:m.length,last:t,pending:!!(l&&l.querySelector(pend))});}`, sel.OutMsg, sel.MsgText, sel.Pending)
	if err != nil {
		return outState{}, err
	}
	var o outState
	return o, json.Unmarshal([]byte(raw), &o)
}

// send types the text and presses Enter. Everything before Enter is "not ready" and may fall back to
// another driver; everything after is ErrUnconfirmed and must not.
func (s *session) send(text string) error {
	before, err := s.outState()
	if err != nil {
		return notReady("could not read the chat: %v", err)
	}
	if err := s.typeText(text); err != nil {
		return err
	}
	// Point of no return.
	if err := chromedp.Run(s.ctx, chromedp.KeyEvent(kb.Enter)); err != nil {
		return fmt.Errorf("%w: pressing send failed: %v", ErrUnconfirmed, err)
	}
	want := normalizeBody(text)
	_, err = poll(s.ctx, s.w.cfg.ConfirmTimeout, 300*time.Millisecond, func() (struct{}, bool, error) {
		o, err := s.outState()
		if err != nil {
			return struct{}{}, false, err
		}
		done := o.Count > before.Count && normalizeBody(o.Last) == want && !o.Pending
		return struct{}{}, done, nil
	})
	if err != nil {
		return fmt.Errorf("%w: it was typed and sent, but WhatsApp did not show it as sent (is the phone online?)", ErrUnconfirmed)
	}
	return nil
}

func (s *session) recentMessages(limit int) ([]Message, error) {
	sel := s.w.cfg.Selectors
	var raw string
	err := s.eval(&raw, `(inn,out,txt,meta,limit)=>{
      const all=Array.from(document.querySelectorAll(inn+","+out)).slice(-limit);
      return JSON.stringify(all.map(el=>{
        const t=Array.from(el.querySelectorAll(txt)).map(e=>e.innerText).join("\n");
        const m=el.querySelector(meta)||(el.matches(meta)?el:null);
        const pre=m?(m.getAttribute("data-pre-plain-text")||""):"";
        const tm=(pre.match(/^\[([^\]]*)\]/)||[])[1]||"";
        return {from:el.matches(out)?"me":"them",text:t,time:tm};
      }));}`, sel.InMsg, sel.OutMsg, sel.MsgText, sel.MsgMeta, limit)
	if err != nil {
		return nil, notReady("could not read the chat: %v", err)
	}
	var msgs []Message
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *session) unreadChats() ([]Chat, error) {
	rows, err := s.rows()
	if err != nil {
		return nil, notReady("could not read the chat list: %v", err)
	}
	var out []Chat
	for _, r := range rows {
		if r.Unread > 0 && r.Title != "" {
			out = append(out, Chat{Name: r.Title, Preview: r.Preview, Unread: r.Unread})
		}
	}
	return out, nil
}

// eval runs fn (a JavaScript arrow function) in the page with args and decodes its result into out.
func (s *session) eval(out any, fn string, args ...any) error {
	enc := make([]string, len(args))
	for i, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		enc[i] = string(b)
	}
	script := "(" + fn + ")(" + strings.Join(enc, ",") + ")"
	if out == nil {
		var discard any
		return chromedp.Run(s.ctx, chromedp.Evaluate(script, &discard))
	}
	return chromedp.Run(s.ctx, chromedp.Evaluate(script, out))
}

// poll calls f every interval until it reports done or the timeout passes. A navigation in progress can
// make a script fail for a moment, so errors are retried until the deadline.
func poll[T any](ctx context.Context, timeout, interval time.Duration, f func() (T, bool, error)) (T, error) {
	var zero T
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		v, done, err := f()
		if err == nil && done {
			return v, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				return zero, lastErr
			}
			return zero, errors.New("timed out")
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Report describes what WhatsApp Web looks like to the driver without changing anything. It is what
// `griffin-host whatsapp-check` prints, and the way to find out which selector stopped matching.
type Report struct {
	SignedIn  bool
	Chats     int
	Selectors map[string]bool
}

// Check opens WhatsApp Web read-only and reports whether it is signed in and which selectors match.
func (w *Web) Check(ctx context.Context) (*Report, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	bctx, cancel, err := w.allocator(ctx, w.cfg.Headless)
	if err != nil {
		return nil, err
	}
	defer cancel()
	s := &session{ctx: bctx, w: w}
	if err := s.load(); err != nil {
		return nil, err
	}
	rep := &Report{SignedIn: true, Selectors: map[string]bool{}}
	rows, _ := s.rows()
	rep.Chats = len(rows)
	sel := w.cfg.Selectors
	for name, css := range map[string]string{
		"chat list": sel.ChatList, "search box": sel.Search, "chat row": sel.Row, "row title": sel.RowTitle,
	} {
		var n int
		if err := s.eval(&n, `(c)=>document.querySelectorAll(c).length`, css); err == nil {
			rep.Selectors[name] = n > 0
		}
	}
	return rep, nil
}

// Login opens a visible browser on the WhatsApp profile and waits for the owner to scan the QR code.
func (w *Web) Login(ctx context.Context, wait time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	bctx, cancel, err := w.allocator(ctx, false)
	if err != nil {
		return err
	}
	defer cancel()
	s := &session{ctx: bctx, w: w}
	if err := chromedp.Run(bctx, chromedp.Navigate(w.cfg.BaseURL)); err != nil {
		return err
	}
	sel := w.cfg.Selectors
	_, err = poll(bctx, wait, time.Second, func() (bool, bool, error) {
		var ok bool
		err := s.eval(&ok, `(c)=>!!document.querySelector(c)`, sel.ChatList)
		return ok, ok, err
	})
	if err != nil {
		return fmt.Errorf("WhatsApp Web was not signed in within %s", wait)
	}
	// Give the profile a moment to flush the session to disk before the browser closes.
	time.Sleep(3 * time.Second)
	return nil
}

// DefaultProfileDir is where the WhatsApp browser profile lives by default.
func DefaultProfileDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".griffin-host", "whatsapp-profile")
	}
	return filepath.Join(os.TempDir(), "griffin-whatsapp-profile")
}
