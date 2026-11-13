package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/whatsapp"
)

const formPage = `<!doctype html><title>Compose</title><body>
<p>New message</p>
<textarea aria-label="Message"></textarea>
<button id="send" onclick="document.body.dataset.sent='yes'; document.title='Sent: '+document.querySelector('textarea').value">Send</button>
<input type="password" aria-label="Password" value="hunter2">`

// Drives a real headless Chromium against a local page: snapshot, type, and the stop-before-send rule. It is
// skipped when no Chrome-like browser is installed.
func TestChromeStagesThenReplaysExactly(t *testing.T) {
	if _, err := whatsapp.FindBrowser(""); err != nil {
		t.Skip("no Chrome or Chromium on this machine")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(formPage)) }))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	profile := t.TempDir()
	ch, err := NewChrome(ctx, ChromeConfig{ProfileDir: profile, NoSandbox: os.Getenv("CI") != "" || os.Geteuid() == 0})
	if err != nil {
		t.Skipf("cannot start the browser here: %v", err)
	}
	defer ch.Close()

	act := &domain.BrowserAction{Goal: "send hello", StartURL: srv.URL, MaxSteps: 5}
	dec := &scripted{moves: []Decision{
		{Step: domain.BrowserStep{Kind: domain.StepType, Ref: "1", Text: "hello"}},
		{Step: domain.BrowserStep{Kind: domain.StepClick, Ref: "2", Final: true}},
	}}
	out, err := Run(ctx, ch, dec, act)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := ch.Snapshot(ctx)
	if page.Title != "Compose" {
		t.Fatalf("nothing may be sent before approval; title is %q", page.Title)
	}
	for _, e := range page.Elements {
		if e.Role == "textbox" && e.Name == "Password" && e.Value != "" {
			t.Fatal("a password's value must never be read")
		}
	}
	if out.Staged == nil || out.Staged.Steps[1].Role != "textbox" || out.Staged.Steps[2].Name != "Send" {
		t.Fatalf("steps should record what they act on: %+v", out.Staged)
	}

	// Replay on a fresh page: the approved script runs exactly, and sends.
	if err := Replay(ctx, ch, out.Staged.Steps); err != nil {
		t.Fatal(err)
	}
	page, _ = ch.Snapshot(ctx)
	if page.Title != "Sent: hello" {
		t.Fatalf("replay should have typed and sent: title %q", page.Title)
	}

	// A step whose element changed must refuse rather than click something else.
	bad := []domain.BrowserStep{{Kind: domain.StepGoto, URL: srv.URL}, {Kind: domain.StepClick, Ref: "2", Role: "button", Name: "Delete everything", Final: true}}
	if err := Replay(ctx, ch, bad); err == nil {
		t.Fatal("clicking an element that does not match what was approved must fail")
	}
}

// A line break typed into a message box must never submit it: chromedp would turn a bare "\n" into an Enter press.
func TestChromeTypingANewlineNeverSubmits(t *testing.T) {
	if _, err := whatsapp.FindBrowser(""); err != nil {
		t.Skip("no Chrome or Chromium on this machine")
	}
	const page = `<!doctype html><title>Compose</title><body>
<textarea aria-label="Message"></textarea>
<input type="text" aria-label="Subject">
<script>
document.querySelector('textarea').addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) { document.title = 'Sent'; e.preventDefault(); }
});
</script>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(page)) }))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ch, err := NewChrome(ctx, ChromeConfig{ProfileDir: t.TempDir(), NoSandbox: os.Getenv("CI") != "" || os.Geteuid() == 0})
	if err != nil {
		t.Skipf("cannot start the browser here: %v", err)
	}
	defer ch.Close()

	if err := ch.Do(ctx, domain.BrowserStep{Kind: domain.StepGoto, URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if err := ch.Do(ctx, domain.BrowserStep{Kind: domain.StepType, Ref: "1", Text: "line one\r\nline two"}); err != nil {
		t.Fatal(err)
	}
	page1, err := ch.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if page1.Title == "Sent" {
		t.Fatal("typing a line break pressed Enter and submitted the message")
	}
	var value string
	for _, e := range page1.Elements {
		if e.Name == "Message" {
			value = e.Value
		}
	}
	if value != "line one\nline two" {
		t.Fatalf("the message should hold both lines on separate lines, got %q", value)
	}

	// A one-line input cannot hold a line break: refuse instead of typing.
	if err := ch.Do(ctx, domain.BrowserStep{Kind: domain.StepType, Ref: "2", Text: "a\nb"}); err == nil {
		t.Fatal("multi-line text into a single-line input must be refused")
	}
	// Single-line text still types normally.
	if err := ch.Do(ctx, domain.BrowserStep{Kind: domain.StepType, Ref: "2", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
}
