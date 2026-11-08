package whatsapp

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// ADB runs one adb command and returns its standard output. It is an interface so tests can play a phone.
type ADB interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// ExecADB runs the real adb binary, optionally against one device when several are attached.
type ExecADB struct {
	Binary string // default "adb"
	Serial string // ANDROID_SERIAL, empty for the only attached device
}

// Run implements ADB.
func (a ExecADB) Run(ctx context.Context, args ...string) (string, error) {
	bin := a.Binary
	if bin == "" {
		bin = "adb"
	}
	full := args
	if a.Serial != "" {
		full = append([]string{"-s", a.Serial}, args...)
	}
	cmd := exec.CommandContext(ctx, bin, full...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// AndroidConfig configures the Android driver.
type AndroidConfig struct {
	ADB ADB
	// Package is WhatsApp's package name. Default com.whatsapp (use com.whatsapp.w4b for WhatsApp Business).
	Package string
	// ConfirmTimeout is how long to wait to see a sent message appear. Default 20 s.
	ConfirmTimeout time.Duration
	// LoadTimeout is how long to wait for a screen to appear. Default 15 s.
	LoadTimeout time.Duration
	// Interval is the polling interval. Default 600 ms.
	Interval time.Duration
	Logger   *slog.Logger
}

// Android drives the WhatsApp app on a phone attached with adb (USB or wireless debugging). It reads the
// screen with uiautomator and acts with intents and taps. It is the fallback when WhatsApp Web is not
// usable, and the way to work when the laptop browser is not signed in.
type Android struct {
	cfg AndroidConfig
}

// NewAndroid builds the driver, applying defaults.
func NewAndroid(cfg AndroidConfig) *Android {
	if cfg.ADB == nil {
		cfg.ADB = ExecADB{}
	}
	if cfg.Package == "" {
		cfg.Package = "com.whatsapp"
	}
	if cfg.ConfirmTimeout <= 0 {
		cfg.ConfirmTimeout = 20 * time.Second
	}
	if cfg.LoadTimeout <= 0 {
		cfg.LoadTimeout = 15 * time.Second
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 600 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Android{cfg: cfg}
}

// Name implements Driver.
func (a *Android) Name() string { return "whatsapp-android" }

// Device implements Driver.
func (a *Android) Device() string { return "phone" }

// Resource ids and labels in the WhatsApp app. They differ between versions, which is why they are named
// constants here and why every lookup also accepts the visible label.
func (a *Android) id(name string) string { return a.cfg.Package + ":id/" + name }

// ---- screen model ----

type uiNode struct {
	Text        string   `xml:"text,attr"`
	ResourceID  string   `xml:"resource-id,attr"`
	ContentDesc string   `xml:"content-desc,attr"`
	Package     string   `xml:"package,attr"`
	Bounds      string   `xml:"bounds,attr"`
	Children    []uiNode `xml:"node"`
}

type screen struct{ root uiNode }

type rect struct{ l, t, r, b int }

func (r rect) cx() int { return (r.l + r.r) / 2 }
func (r rect) cy() int { return (r.t + r.b) / 2 }

var boundsRe = regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)

func parseBounds(s string) (rect, bool) {
	m := boundsRe.FindStringSubmatch(s)
	if m == nil {
		return rect{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return rect{n(1), n(2), n(3), n(4)}, true
}

func (s screen) walk(f func(n *uiNode, parent *uiNode)) {
	var rec func(n, p *uiNode)
	rec = func(n, p *uiNode) {
		f(n, p)
		for i := range n.Children {
			rec(&n.Children[i], n)
		}
	}
	for i := range s.root.Children {
		rec(&s.root.Children[i], &s.root)
	}
}

// find returns every node whose resource id matches id, in document order.
func (s screen) byID(id string) []*uiNode {
	var out []*uiNode
	s.walk(func(n, _ *uiNode) {
		if n.ResourceID == id {
			out = append(out, n)
		}
	})
	return out
}

func (s screen) byDesc(desc string) []*uiNode {
	var out []*uiNode
	s.walk(func(n, _ *uiNode) {
		if strings.EqualFold(n.ContentDesc, desc) {
			out = append(out, n)
		}
	})
	return out
}

func (s screen) width() int {
	max := 0
	s.walk(func(n, _ *uiNode) {
		if r, ok := parseBounds(n.Bounds); ok && r.r > max {
			max = r.r
		}
	})
	return max
}

func descendants(n *uiNode, id string) []*uiNode {
	var out []*uiNode
	var rec func(x *uiNode)
	rec = func(x *uiNode) {
		for i := range x.Children {
			c := &x.Children[i]
			if c.ResourceID == id {
				out = append(out, c)
			}
			rec(c)
		}
	}
	rec(n)
	return out
}

func (a *Android) dump(ctx context.Context) (screen, error) {
	if _, err := a.cfg.ADB.Run(ctx, "shell", "uiautomator", "dump", "/sdcard/griffin_ui.xml"); err != nil {
		return screen{}, err
	}
	xmlText, err := a.cfg.ADB.Run(ctx, "exec-out", "cat", "/sdcard/griffin_ui.xml")
	if err != nil {
		return screen{}, err
	}
	var h struct {
		Nodes []uiNode `xml:"node"`
	}
	dec := xml.NewDecoder(strings.NewReader(xmlText))
	dec.Strict = false
	if err := dec.Decode(&h); err != nil {
		return screen{}, fmt.Errorf("could not read the phone's screen: %w", err)
	}
	return screen{root: uiNode{Children: h.Nodes}}, nil
}

func (a *Android) tap(ctx context.Context, r rect) error {
	_, err := a.cfg.ADB.Run(ctx, "shell", "input", "tap", strconv.Itoa(r.cx()), strconv.Itoa(r.cy()))
	return err
}

func (a *Android) tapNode(ctx context.Context, n *uiNode) error {
	r, ok := parseBounds(n.Bounds)
	if !ok {
		return errors.New("a control on the phone has no position")
	}
	return a.tap(ctx, r)
}

// waitFor polls the screen until check says it is the screen wanted.
func (a *Android) waitFor(ctx context.Context, timeout time.Duration, check func(screen) bool) (screen, error) {
	return poll(ctx, timeout, a.cfg.Interval, func() (screen, bool, error) {
		s, err := a.dump(ctx)
		if err != nil {
			return screen{}, false, err
		}
		return s, check(s), nil
	})
}

// ---- readiness ----

var safeURL = regexp.MustCompile(`^[A-Za-z0-9%/:.?=_~-]+$`)
var plainName = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)

func (a *Android) ready(ctx context.Context) error {
	st, err := a.cfg.ADB.Run(ctx, "get-state")
	if err != nil || strings.TrimSpace(st) != "device" {
		return notReady("no phone is connected with adb (connect it by USB or wireless debugging and allow this computer)")
	}
	pk, err := a.cfg.ADB.Run(ctx, "shell", "pm", "list", "packages", a.cfg.Package)
	if err != nil || !strings.Contains(pk, "package:"+a.cfg.Package) {
		return notReady("WhatsApp (%s) is not installed on the phone", a.cfg.Package)
	}
	_, _ = a.cfg.ADB.Run(ctx, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	s, err := a.dump(ctx)
	if err != nil {
		return notReady("could not read the phone's screen: %v", err)
	}
	locked := false
	s.walk(func(n, _ *uiNode) {
		if n.Package == "com.android.systemui" && strings.Contains(strings.ToLower(n.ResourceID), "keyguard") {
			locked = true
		}
	})
	if locked {
		return notReady("the phone is locked; unlock it and try again")
	}
	return nil
}

// ---- actions ----

// Do implements Driver.
func (a *Android) Do(ctx context.Context, act *domain.MessagingAction) (*Result, error) {
	if err := a.ready(ctx); err != nil {
		return nil, err
	}
	switch act.Op {
	case domain.MessagingUnread:
		chats, err := a.unread(ctx)
		if err != nil {
			return nil, err
		}
		return &Result{Chats: chats}, nil
	case domain.MessagingReadRecent:
		if err := a.openChat(ctx, act, ""); err != nil {
			return nil, err
		}
		msgs, err := a.visibleMessages(ctx, act.Limit)
		if err != nil {
			return nil, err
		}
		return &Result{Messages: msgs, Notes: []string{"only the messages visible on the phone's screen are read"}}, nil
	case domain.MessagingSend:
		if err := a.openChat(ctx, act, act.Text); err != nil {
			return nil, err
		}
		if err := a.send(ctx, act.Text); err != nil {
			return nil, err
		}
		return &Result{Sent: true}, nil
	}
	return nil, fmt.Errorf("unsupported op %q", act.Op)
}

func (a *Android) launchURL(ctx context.Context, u string) error {
	if !safeURL.MatchString(u) {
		return notReady("refusing to open an unexpected link")
	}
	// The link is single-quoted because adb joins its arguments into one shell command on the phone.
	_, err := a.cfg.ADB.Run(ctx, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "'"+u+"'", "-p", a.cfg.Package)
	if err != nil {
		return notReady("could not open WhatsApp: %v", err)
	}
	return nil
}

// openChat opens the chat. With a phone number it uses WhatsApp's own link, which carries the text to
// send as a prefilled draft; with only a name it searches the chat list for an exact match.
func (a *Android) openChat(ctx context.Context, act *domain.MessagingAction, draft string) error {
	if act.Phone != "" {
		u := "https://wa.me/" + act.Phone
		if draft != "" {
			u += "?text=" + strings.ReplaceAll(url.QueryEscape(draft), "+", "%20")
		}
		if err := a.launchURL(ctx, u); err != nil {
			return err
		}
		_, err := a.waitFor(ctx, a.cfg.LoadTimeout, func(s screen) bool { return len(s.byID(a.id("entry"))) > 0 })
		if err != nil {
			return notReady("the chat did not open on the phone")
		}
		return nil
	}
	return a.openByName(ctx, act.Contact, draft)
}

func (a *Android) openByName(ctx context.Context, name, draft string) error {
	if !plainName.MatchString(name) {
		return notReady("the phone can search only plain letters and digits in a name; give the phone number instead")
	}
	if _, err := a.cfg.ADB.Run(ctx, "shell", "monkey", "-p", a.cfg.Package, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return notReady("could not open WhatsApp: %v", err)
	}
	home, err := a.waitFor(ctx, a.cfg.LoadTimeout, func(s screen) bool { return len(s.byDesc("Search")) > 0 })
	if err != nil {
		return notReady("WhatsApp's chat list did not appear on the phone")
	}
	if err := a.tapNode(ctx, home.byDesc("Search")[0]); err != nil {
		return notReady("%v", err)
	}
	if _, err := a.cfg.ADB.Run(ctx, "shell", "input", "text", strings.ReplaceAll(name, " ", "%s")); err != nil {
		return notReady("could not type into the search box: %v", err)
	}
	want := normalizeName(name)
	res, err := a.waitFor(ctx, a.cfg.LoadTimeout, func(s screen) bool { return len(s.byID(a.id("conversations_row_contact_name"))) > 0 })
	if err != nil {
		return fmt.Errorf("%w: no chat matched %q", ErrContactNotFound, name)
	}
	var matches []*uiNode
	for _, n := range res.byID(a.id("conversations_row_contact_name")) {
		if normalizeName(n.Text) == want {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 0:
		return fmt.Errorf("%w: no chat is named exactly %q", ErrContactNotFound, name)
	case 1:
	default:
		return fmt.Errorf("%w: %q matches %d chats; use the phone number", ErrAmbiguousContact, name, len(matches))
	}
	if err := a.tapNode(ctx, matches[0]); err != nil {
		return notReady("%v", err)
	}
	chat, err := a.waitFor(ctx, a.cfg.LoadTimeout, func(s screen) bool {
		h := s.byID(a.id("conversation_contact_name"))
		return len(h) > 0 && normalizeName(h[0].Text) == want
	})
	if err != nil || len(chat.byID(a.id("entry"))) == 0 {
		return notReady("the chat with %q did not open on the phone", name)
	}
	if draft != "" {
		if entry := chat.byID(a.id("entry")); len(entry) > 0 {
			if err := a.tapNode(ctx, entry[0]); err != nil {
				return notReady("%v", err)
			}
		}
		if _, err := a.cfg.ADB.Run(ctx, "shell", "input", "text", "'"+draftForInput(draft)+"'"); err != nil {
			return notReady("could not type the message: %v", err)
		}
	}
	return nil
}

// draftForInput is only used for the name-search path, where adb's `input text` cannot carry anything but
// simple characters. send() verifies the composer holds exactly the text, so a mismatch is caught before
// anything is sent. Reject anything unsafe by replacing it; the later check then refuses the send.
func draftForInput(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == ',', r == '!', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteString("%s")
		default:
			b.WriteRune('?') // forces the composer check to fail rather than send something different
		}
	}
	return b.String()
}

func (a *Android) composerText(s screen) (string, bool) {
	e := s.byID(a.id("entry"))
	if len(e) == 0 {
		return "", false
	}
	return e[0].Text, true
}

// outgoing returns the texts of visible messages on the right half of the screen (your own messages).
func (a *Android) outgoing(s screen) []string {
	w := s.width()
	var out []string
	for _, n := range s.byID(a.id("message_text")) {
		if r, ok := parseBounds(n.Bounds); ok && r.cx() > w/2 {
			out = append(out, n.Text)
		}
	}
	return out
}

func countOf(list []string, text string) int {
	n := 0
	for _, t := range list {
		if normalizeBody(t) == normalizeBody(text) {
			n++
		}
	}
	return n
}

// send checks the draft is exactly the text, taps send, then looks for the message to appear. Before the
// tap, problems are "not ready" (nothing sent); after it, ErrUnconfirmed.
func (a *Android) send(ctx context.Context, text string) error {
	s, err := a.dump(ctx)
	if err != nil {
		return notReady("could not read the phone's screen: %v", err)
	}
	got, ok := a.composerText(s)
	if !ok || normalizeBody(got) != normalizeBody(text) {
		return notReady("the phone's message box did not hold exactly the text to send")
	}
	btn := s.byID(a.id("send"))
	if len(btn) == 0 {
		btn = s.byDesc("Send")
	}
	if len(btn) == 0 {
		return notReady("the send button was not found on the phone")
	}
	before := countOf(a.outgoing(s), text)
	// Point of no return.
	if err := a.tapNode(ctx, btn[0]); err != nil {
		return fmt.Errorf("%w: tapping send failed: %v", ErrUnconfirmed, err)
	}
	_, err = a.waitFor(ctx, a.cfg.ConfirmTimeout, func(s screen) bool {
		if countOf(a.outgoing(s), text) <= before {
			return false
		}
		if left, ok := a.composerText(s); ok && normalizeBody(left) != "" && normalizeBody(left) == normalizeBody(text) {
			return false // still sitting in the box
		}
		// WhatsApp shows a clock while the message is waiting to leave the phone.
		return len(s.byDesc("Pending")) == 0
	})
	if err != nil {
		return fmt.Errorf("%w: send was tapped, but the message did not show as sent (is the phone online?)", ErrUnconfirmed)
	}
	return nil
}

func (a *Android) visibleMessages(ctx context.Context, limit int) ([]Message, error) {
	s, err := a.dump(ctx)
	if err != nil {
		return nil, notReady("could not read the phone's screen: %v", err)
	}
	w := s.width()
	var msgs []Message
	for _, n := range s.byID(a.id("message_text")) {
		r, ok := parseBounds(n.Bounds)
		from := "them"
		if ok && r.cx() > w/2 {
			from = "me"
		}
		msgs = append(msgs, Message{From: from, Text: n.Text})
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

func (a *Android) unread(ctx context.Context) ([]Chat, error) {
	if _, err := a.cfg.ADB.Run(ctx, "shell", "monkey", "-p", a.cfg.Package, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return nil, notReady("could not open WhatsApp: %v", err)
	}
	s, err := a.waitFor(ctx, a.cfg.LoadTimeout, func(s screen) bool { return len(s.byID(a.id("conversations_row_contact_name"))) > 0 })
	if err != nil {
		return nil, notReady("WhatsApp's chat list did not appear on the phone")
	}
	var chats []Chat
	s.walk(func(n, _ *uiNode) {
		names := descendants(n, a.id("conversations_row_contact_name"))
		counts := descendants(n, a.id("conversations_row_message_count"))
		if len(names) != 1 || len(counts) == 0 {
			return
		}
		cnt, _ := strconv.Atoi(strings.TrimSpace(counts[0].Text))
		if cnt <= 0 {
			cnt = 1
		}
		c := Chat{Name: names[0].Text, Unread: cnt}
		if p := descendants(n, a.id("single_msg_tv")); len(p) > 0 {
			c.Preview = p[0].Text
		}
		chats = append(chats, c)
	})
	return chats, nil
}

// Status reports whether a phone is attached and has WhatsApp, for `griffin-host --check`.
func (a *Android) Status(ctx context.Context) error {
	st, err := a.cfg.ADB.Run(ctx, "get-state")
	if err != nil || strings.TrimSpace(st) != "device" {
		return errors.New("no phone connected with adb")
	}
	pk, err := a.cfg.ADB.Run(ctx, "shell", "pm", "list", "packages", a.cfg.Package)
	if err != nil || !strings.Contains(pk, "package:"+a.cfg.Package) {
		return fmt.Errorf("WhatsApp (%s) is not installed on the phone", a.cfg.Package)
	}
	return nil
}
