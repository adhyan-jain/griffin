package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

const pkg = "com.whatsapp"

// fakePhone plays a phone running WhatsApp: it answers adb commands and renders uiautomator XML for
// whichever screen is showing, reacting to taps and typing the way the app does.
type fakePhone struct {
	mu        sync.Mutex
	connected bool
	locked    bool
	installed bool
	screen    string // home | search | chat
	chats     []string
	unread    map[string]int
	query     string
	chatName  string
	composer  string
	msgs      []fakeMsg
	sendWorks bool
	sendTaps  int
	urls      []string
	commands  []string
}

type fakeMsg struct {
	out  bool
	text string
}

func newPhone() *fakePhone {
	return &fakePhone{connected: true, installed: true, screen: "home", sendWorks: true,
		chats: []string{"Mom", "Dad", "Sam Lee", "Sam Li"}, unread: map[string]int{"Dad": 3}}
}

func node(rid, text, desc, bounds string, children ...string) string {
	return fmt.Sprintf(`<node index="0" text="%s" resource-id="%s" class="x" package="%s" content-desc="%s" bounds="%s">%s</node>`,
		html.EscapeString(text), rid, pkg, html.EscapeString(desc), bounds, strings.Join(children, ""))
}

func (p *fakePhone) render() string {
	var kids []string
	switch {
	case p.locked:
		return `<?xml version='1.0'?><hierarchy rotation="0"><node text="" resource-id="com.android.systemui:id/keyguard_host" package="com.android.systemui" content-desc="" bounds="[0,0][1080,2400]"/></hierarchy>`
	case p.screen == "home":
		kids = append(kids, node("", "", "Search", "[900,100][1000,200]"))
		for i, c := range p.chats {
			y := 300 + i*150
			row := []string{node(pkg+":id/conversations_row_contact_name", c, "", fmt.Sprintf("[200,%d][800,%d]", y, y+60))}
			if n := p.unread[c]; n > 0 {
				row = append(row, node(pkg+":id/conversations_row_message_count", strconv.Itoa(n), "", fmt.Sprintf("[900,%d][960,%d]", y, y+60)),
					node(pkg+":id/single_msg_tv", "preview of "+c, "", fmt.Sprintf("[200,%d][800,%d]", y+60, y+110)))
			}
			kids = append(kids, node(pkg+":id/contact_row_container", "", "", fmt.Sprintf("[0,%d][1080,%d]", y, y+140), row...))
		}
	case p.screen == "search":
		kids = append(kids, node(pkg+":id/search_input", p.query, "", "[100,100][900,200]"))
		for i, c := range p.chats {
			if strings.Contains(strings.ToLower(c), strings.ToLower(p.query)) {
				y := 300 + i*150
				kids = append(kids, node(pkg+":id/conversations_row_contact_name", c, "", fmt.Sprintf("[200,%d][800,%d]", y, y+60)))
			}
		}
	case p.screen == "chat":
		kids = append(kids, node(pkg+":id/conversation_contact_name", p.chatName, "", "[200,80][800,160]"))
		for i, m := range p.msgs {
			y := 300 + i*120
			b := "[40,%d][500,%d]"
			if m.out {
				b = "[580,%d][1040,%d]"
			}
			kids = append(kids, node(pkg+":id/message_text", m.text, "", fmt.Sprintf(b, y, y+80)))
		}
		kids = append(kids, node(pkg+":id/entry", p.composer, "", "[100,2200][900,2300]"))
		kids = append(kids, node(pkg+":id/send", "", "Send", "[920,2200][1040,2300]"))
	}
	return `<?xml version='1.0'?><hierarchy rotation="0">` + node("", "", "", "[0,0][1080,2400]", kids...) + `</hierarchy>`
}

var tapRe = regexp.MustCompile(`^shell input tap (\d+) (\d+)$`)

func (p *fakePhone) Run(_ context.Context, args ...string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cmd := strings.Join(args, " ")
	p.commands = append(p.commands, cmd)
	switch {
	case cmd == "get-state":
		if !p.connected {
			return "", fmt.Errorf("error: no devices/emulators found")
		}
		return "device\n", nil
	case strings.HasPrefix(cmd, "shell pm list packages"):
		if p.installed {
			return "package:" + pkg + "\n", nil
		}
		return "", nil
	case strings.HasPrefix(cmd, "shell input keyevent"), strings.HasPrefix(cmd, "shell uiautomator dump"), strings.HasPrefix(cmd, "shell wm dismiss-keyguard"):
		return "", nil
	case cmd == "exec-out cat /sdcard/griffin_ui.xml":
		return p.render(), nil
	case strings.HasPrefix(cmd, "shell monkey"):
		p.screen = "home"
		return "", nil
	case strings.HasPrefix(cmd, "shell am start"):
		u := args[len(args)-3] // ... -d 'URL' -p pkg
		u = strings.Trim(u, "'")
		p.urls = append(p.urls, u)
		parsed, _ := url.Parse(u)
		p.screen, p.chatName, p.composer = "chat", strings.TrimPrefix(parsed.Path, "/"), ""
		if t := parsed.Query().Get("text"); t != "" {
			p.composer = t
		}
		return "", nil
	case strings.HasPrefix(cmd, "shell input text"):
		txt := strings.ReplaceAll(strings.Trim(args[len(args)-1], "'"), "%s", " ")
		if p.screen == "search" {
			p.query += txt
		} else if p.screen == "chat" {
			p.composer += txt
		}
		return "", nil
	}
	if m := tapRe.FindStringSubmatch(cmd); m != nil {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		switch p.screen {
		case "home":
			if x >= 900 && y <= 200 {
				p.screen, p.query = "search", ""
			}
		case "search":
			for i, c := range p.chats {
				ry := 300 + i*150
				if y >= ry && y <= ry+60 && strings.Contains(strings.ToLower(c), strings.ToLower(p.query)) {
					p.screen, p.chatName, p.composer = "chat", c, ""
				}
			}
		case "chat":
			if x >= 920 && y >= 2200 { // send
				p.sendTaps++
				if p.sendWorks && p.composer != "" {
					p.msgs = append(p.msgs, fakeMsg{out: true, text: p.composer})
					p.composer = ""
				}
			}
		}
		return "", nil
	}
	return "", fmt.Errorf("fake phone: unhandled command %q", cmd)
}

func newAndroid(p *fakePhone) *Android {
	return NewAndroid(AndroidConfig{ADB: p, ConfirmTimeout: 300 * time.Millisecond, LoadTimeout: 300 * time.Millisecond, Interval: 5 * time.Millisecond})
}

func send(t *testing.T, contact, phone, text string) *domain.MessagingAction {
	t.Helper()
	a := &domain.MessagingAction{Op: domain.MessagingSend, Contact: contact, Phone: phone, Text: text}
	require.NoError(t, a.Validate())
	return a
}

func TestAndroid_SendByNumberCarriesTheExactText(t *testing.T) {
	p := newPhone()
	text := "Running late 🙏\nsee you at 6 & bring the docs?"
	res, err := newAndroid(p).Do(context.Background(), send(t, "", "919876543210", text))
	require.NoError(t, err)
	assert.True(t, res.Sent)
	require.Len(t, p.urls, 1)
	assert.Regexp(t, `^https://wa\.me/919876543210\?text=[A-Za-z0-9%._~-]+$`, p.urls[0], "the text travels percent-encoded, nothing raw reaches the phone's shell")
	assert.Equal(t, 1, p.sendTaps)
	require.Len(t, p.msgs, 1)
	assert.Equal(t, text, p.msgs[0].text)
}

func TestAndroid_AnUnconfirmedSendIsNeverRetried(t *testing.T) {
	p := newPhone()
	p.sendWorks = false
	_, err := newAndroid(p).Do(context.Background(), send(t, "", "919876543210", "hello"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnconfirmed)
	assert.NotErrorIs(t, err, ErrNotReady, "must not look like a safe-to-fall-back failure")
	assert.Equal(t, 1, p.sendTaps, "exactly one tap on send, no retry")
}

func TestAndroid_NotReadyBeforeTouchingAnything(t *testing.T) {
	for name, mutate := range map[string]func(*fakePhone){
		"no phone":      func(p *fakePhone) { p.connected = false },
		"not installed": func(p *fakePhone) { p.installed = false },
		"locked":        func(p *fakePhone) { p.locked = true },
	} {
		t.Run(name, func(t *testing.T) {
			p := newPhone()
			mutate(p)
			_, err := newAndroid(p).Do(context.Background(), send(t, "", "919876543210", "hi"))
			assert.ErrorIs(t, err, ErrNotReady)
			assert.Zero(t, p.sendTaps)
			assert.Empty(t, p.urls, "nothing was opened")
		})
	}
}

func TestAndroid_SendByExactName(t *testing.T) {
	p := newPhone()
	res, err := newAndroid(p).Do(context.Background(), send(t, "mom", "", "hello there"))
	require.NoError(t, err)
	assert.True(t, res.Sent)
	require.Len(t, p.msgs, 1)
	assert.Equal(t, "hello there", p.msgs[0].text)
	assert.Equal(t, "Mom", p.chatName)
}

func TestAndroid_NamesAreNeverGuessed(t *testing.T) {
	p := newPhone()
	// "Sam" would fuzzy-match two chats, and "Mo" is a prefix of Mom: neither may be used.
	for _, name := range []string{"Sam", "Mo", "Nobody"} {
		_, err := newAndroid(p).Do(context.Background(), send(t, name, "", "hi"))
		require.Error(t, err, name)
		assert.True(t, errorsIsAny(err, ErrContactNotFound, ErrAmbiguousContact), "%s: %v", name, err)
	}
	assert.Zero(t, p.sendTaps)
	assert.Empty(t, p.msgs)
}

func errorsIsAny(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

func TestAndroid_UnsafeNamesNeedANumber(t *testing.T) {
	p := newPhone()
	_, err := newAndroid(p).Do(context.Background(), send(t, "Mom; rm -rf /", "", "hi"))
	assert.ErrorIs(t, err, ErrNotReady)
	for _, c := range p.commands {
		assert.NotContains(t, c, "rm -rf", "the name never reached the phone's shell")
	}
}

func TestAndroid_ReadRecentTellsWhoSaidWhat(t *testing.T) {
	p := newPhone()
	p.msgs = []fakeMsg{{false, "are you coming?"}, {true, "yes, on my way"}, {false, "great"}}
	a := &domain.MessagingAction{Op: domain.MessagingReadRecent, Phone: "919876543210", Limit: 2}
	require.NoError(t, a.Validate())
	res, err := newAndroid(p).Do(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, []Message{{From: "me", Text: "yes, on my way"}, {From: "them", Text: "great"}}, res.Messages)
}

func TestAndroid_UnreadChats(t *testing.T) {
	p := newPhone()
	a := &domain.MessagingAction{Op: domain.MessagingUnread}
	require.NoError(t, a.Validate())
	res, err := newAndroid(p).Do(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, []Chat{{Name: "Dad", Preview: "preview of Dad", Unread: 3}}, res.Chats)
}

func TestAndroid_ARefusedLinkNeverReachesTheShell(t *testing.T) {
	p := newPhone()
	err := newAndroid(p).launchURL(context.Background(), "https://wa.me/1'; reboot; '")
	assert.ErrorIs(t, err, ErrNotReady)
	for _, c := range p.commands {
		assert.NotContains(t, c, "reboot")
	}
}
