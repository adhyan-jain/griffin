package whatsapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

// The stand-in page below implements the structure the default selectors expect: a chat list in #pane-side,
// a search box, a chat pane with a message box in the footer, outgoing and incoming message bubbles, and a
// "pending clock" that clears shortly after sending. It is not WhatsApp; it proves the driver's logic
// (exact matching, typing, confirmation, no retry) in a real browser. Against the real site, run
// `griffin-host whatsapp-check` to confirm the selectors still match.
const fakePage = `<!doctype html><html><head><meta charset="utf-8"><title>fake</title></head><body>
%s
<script>
const MODE = %q;               // ok | never (a send that never appears)
const CHATS = [["Mom",0],["Dad",3],["Sam Lee",0],["Sam Li",0],["Twin",0],["Twin",0]];
const app = document.getElementById("app");
function openChat(title){
  document.getElementById("main").hidden = false;
  document.getElementById("hdr").innerHTML = '<span title="'+title+'">'+title+'</span>';
}
if (document.getElementById("pane-side")) {
  const list = document.getElementById("rows");
  function render(q){
    list.innerHTML = "";
    CHATS.filter(c => c[0].toLowerCase().includes(q.toLowerCase())).forEach(c => {
      const r = document.createElement("div"); r.setAttribute("role","listitem");
      r.innerHTML = '<span title="'+c[0]+'">'+c[0]+'</span><span class="preview" dir="ltr">hey from '+c[0]+'</span>' +
        (c[1] ? '<span aria-label="'+c[1]+' unread messages">'+c[1]+'</span>' : '');
      r.onclick = () => openChat(c[0]);
      list.appendChild(r);
    });
  }
  render("");
  document.getElementById("search").addEventListener("input", e => render(e.target.innerText));
}
const params = new URLSearchParams(location.search);
if (location.pathname === "/send") {
  const ph = params.get("phone");
  if (ph === "000") {
    document.body.insertAdjacentHTML("beforeend", '<div data-animate-modal-popup="true">Phone number shared via url is invalid.</div>');
  } else { openChat("+" + ph); }
}
const composer = document.getElementById("composer");
if (composer) composer.addEventListener("keydown", e => {
  if (e.key !== "Enter" || e.shiftKey) return;
  e.preventDefault();
  const text = composer.innerText.replace(/\n+$/,"");
  if (!text || MODE === "never") return;
  composer.innerHTML = "";
  const m = document.createElement("div"); m.className = "message-out";
  m.innerHTML = '<div data-pre-plain-text="[10:32 am, 11/7/2026] Me: "><span class="selectable-text"></span></div><span data-icon="msg-time"></span>';
  m.querySelector(".selectable-text").innerText = text;
  document.getElementById("msgs").appendChild(m);
  setTimeout(() => m.querySelector('[data-icon="msg-time"]').remove(), 400);
  window.__sent = (window.__sent||[]).concat([text]);
  fetch("/__sent", {method:"POST", body:text});
});
</script></body></html>`

const signedInBody = `<div id="app">
  <div id="side"><div id="pane-side"><div id="rows"></div></div>
  <div contenteditable="true" role="textbox" aria-label="Search input textbox" data-tab="3" id="search"></div></div>
  <div id="main" hidden><header id="hdr"></header>
    <div id="msgs">
      <div class="message-in"><div data-pre-plain-text="[10:30 am, 11/7/2026] Mom: "><span class="selectable-text">are you coming?</span></div></div>
      <div class="message-out"><div data-pre-plain-text="[10:31 am, 11/7/2026] Me: "><span class="selectable-text">yes on my way</span></div></div>
    </div>
    <footer><div contenteditable="true" role="textbox" data-tab="10" id="composer"></div></footer></div></div>`

const signedOutBody = `<div id="app"><canvas aria-label="Scan this QR code to link a device!" width="200" height="200"></canvas></div>`

type fakeSite struct {
	srv  *httptest.Server
	sent atomic.Value // []string
	n    atomic.Int32
}

func newSite(t *testing.T, signedIn bool, mode string) *fakeSite {
	t.Helper()
	f := &fakeSite{}
	f.sent.Store([]string{})
	body := signedOutBody
	if signedIn {
		body = signedInBody
	}
	mux := http.NewServeMux()
	page := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, fakePage, body, mode)
	}
	mux.HandleFunc("/", page)
	mux.HandleFunc("/send", page)
	mux.HandleFunc("/__sent", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8192)
		n, _ := r.Body.Read(b)
		f.sent.Store(append(f.sent.Load().([]string), string(b[:n])))
		f.n.Add(1)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSite) sentTexts() []string { return f.sent.Load().([]string) }

func newWebDriver(t *testing.T, f *fakeSite) *Web {
	t.Helper()
	if _, err := FindBrowser(os.Getenv("WHATSAPP_BROWSER")); err != nil {
		t.Skip("no Chrome/Chromium available: " + err.Error())
	}
	return NewWeb(WebConfig{
		ProfileDir: t.TempDir(), Headless: true, BaseURL: f.srv.URL,
		LoadTimeout: 10 * time.Second, ConfirmTimeout: 3 * time.Second,
	})
}

func ctx30(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestWeb_SendsToAContactByExactName(t *testing.T) {
	f := newSite(t, true, "ok")
	res, err := newWebDriver(t, f).Do(ctx30(t), send(t, "mom", "", "Running late 🙏\nsee you at 6"))
	require.NoError(t, err)
	assert.True(t, res.Sent)
	assert.Equal(t, []string{"Running late 🙏\nsee you at 6"}, f.sentTexts(), "exactly one message, with the exact text and line break")
}

func TestWeb_SendsByPhoneNumber(t *testing.T) {
	f := newSite(t, true, "ok")
	res, err := newWebDriver(t, f).Do(ctx30(t), send(t, "", "919876543210", "hello"))
	require.NoError(t, err)
	assert.True(t, res.Sent)
	assert.Equal(t, []string{"hello"}, f.sentTexts())
}

func TestWeb_NamesAreNeverGuessed(t *testing.T) {
	f := newSite(t, true, "ok")
	w := newWebDriver(t, f)
	for name, want := range map[string]error{
		"Sam":    ErrContactNotFound,  // two chats contain it, none is exactly "Sam"
		"Mo":     ErrContactNotFound,  // a prefix of Mom
		"Twin":   ErrAmbiguousContact, // two chats are exactly "Twin"
		"Nobody": ErrContactNotFound,
	} {
		_, err := w.Do(ctx30(t), send(t, name, "", "hi"))
		require.Error(t, err, name)
		assert.ErrorIs(t, err, want, name)
	}
	assert.Empty(t, f.sentTexts(), "nothing was sent to anyone")
}

func TestWeb_InvalidNumberIsNotFoundAndStopsTheJob(t *testing.T) {
	f := newSite(t, true, "ok")
	a := &domain.MessagingAction{Op: domain.MessagingSend, Phone: "0000000", Text: "hi"}
	require.NoError(t, a.Validate())
	// The stand-in flags the number "000" as invalid; Validate keeps 7+ digits, so ask the driver directly.
	w := newWebDriver(t, f)
	bctx, cancel, err := w.allocator(ctx30(t), true)
	require.NoError(t, err)
	defer cancel()
	s := &session{ctx: bctx, w: w}
	require.NoError(t, s.load())
	err = s.openByPhone("000")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContactNotFound)
	assert.NotErrorIs(t, err, ErrNotReady, "another driver would not find the number either")
	assert.Empty(t, f.sentTexts())
}

func TestWeb_SignedOutFallsBackBecauseNothingWasTouched(t *testing.T) {
	f := newSite(t, false, "ok")
	_, err := newWebDriver(t, f).Do(ctx30(t), send(t, "Mom", "", "hi"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotReady)
	assert.Contains(t, err.Error(), "whatsapp-login")
	assert.Empty(t, f.sentTexts())
}

func TestWeb_ASendThatNeverAppearsIsUnconfirmedNotRetried(t *testing.T) {
	f := newSite(t, true, "never")
	_, err := newWebDriver(t, f).Do(ctx30(t), send(t, "Mom", "", "hi"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnconfirmed)
	assert.NotErrorIs(t, err, ErrNotReady)
}

func TestWeb_ReadRecentMessages(t *testing.T) {
	f := newSite(t, true, "ok")
	a := &domain.MessagingAction{Op: domain.MessagingReadRecent, Contact: "Mom", Limit: 5}
	require.NoError(t, a.Validate())
	res, err := newWebDriver(t, f).Do(ctx30(t), a)
	require.NoError(t, err)
	assert.Equal(t, []Message{
		{From: "them", Text: "are you coming?", Time: "10:30 am, 11/7/2026"},
		{From: "me", Text: "yes on my way", Time: "10:31 am, 11/7/2026"},
	}, res.Messages)
	assert.Empty(t, f.sentTexts(), "reading sends nothing")
}

func TestWeb_UnreadChats(t *testing.T) {
	f := newSite(t, true, "ok")
	a := &domain.MessagingAction{Op: domain.MessagingUnread}
	require.NoError(t, a.Validate())
	res, err := newWebDriver(t, f).Do(ctx30(t), a)
	require.NoError(t, err)
	require.Len(t, res.Chats, 1)
	assert.Equal(t, "Dad", res.Chats[0].Name)
	assert.Equal(t, 3, res.Chats[0].Unread)
	assert.True(t, strings.Contains(res.Chats[0].Preview, "hey from Dad"))
}

func TestWeb_CheckReportsWhichSelectorsMatch(t *testing.T) {
	f := newSite(t, true, "ok")
	rep, err := newWebDriver(t, f).Check(ctx30(t))
	require.NoError(t, err)
	assert.True(t, rep.SignedIn)
	assert.Equal(t, 6, rep.Chats)
	for name, ok := range rep.Selectors {
		assert.True(t, ok, "selector %q should match the stand-in page", name)
	}
}
