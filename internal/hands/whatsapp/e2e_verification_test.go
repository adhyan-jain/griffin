package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

// TestE2E_WhatsAppBhaiMessageWassup_SentAndReadBackConfirmed tests the full flow:
// 1. Send "wassup" to "bhai".
// 2. Delivery is confirmed on-screen by the driver.
// 3. Read back recent messages from the chat with "bhai" to verify the message is present.
func TestE2E_WhatsAppBhaiMessageWassup_SentAndReadBackConfirmed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := newPhone()
	p.chats = append(p.chats, "bhai")
	driver := newAndroid(p)

	// Step 1: Send "wassup" to "bhai"
	sendAction := &domain.MessagingAction{
		Channel: domain.MessagingChannelWhatsApp,
		Op:      domain.MessagingSend,
		Contact: "bhai",
		Text:    "wassup",
	}
	sendRes, err := driver.Do(ctx, sendAction)
	require.NoError(t, err, "send must succeed")
	assert.True(t, sendRes.Sent, "send result must confirm message was sent")
	assert.Equal(t, 1, p.sendTaps, "send button must be tapped")

	// Step 2: Read back messages from the chat with "bhai" to verify delivery
	readAction := &domain.MessagingAction{
		Channel: domain.MessagingChannelWhatsApp,
		Op:      domain.MessagingReadRecent,
		Contact: "bhai",
		Limit:   5,
	}
	readRes, err := driver.Do(ctx, readAction)
	require.NoError(t, err, "read_recent must succeed")
	require.NotEmpty(t, readRes.Messages, "chat history must not be empty after send")

	// Verify "wassup" was read back from the chat
	foundSentText := false
	for _, m := range readRes.Messages {
		if m.Text == "wassup" && m.From == "me" {
			foundSentText = true
			break
		}
	}
	assert.True(t, foundSentText, "the message 'wassup' must be present in the chat history read back from bhai")
}

// TestE2E_LockedPhoneFallsBackSeamlesslyToWhatsAppWeb tests that when the phone is locked,
// the Android driver detects the keyguard, attempts dismissal, and if still locked, returns ErrNotReady.
// The Messenger automatically falls back to WhatsApp Web, ensuring the send succeeds without
// requiring the user to leave their phone screen unlocked.
func TestE2E_LockedPhoneFallsBackSeamlesslyToWhatsAppWeb(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Phone is locked in pocket
	lockedPhone := newPhone()
	lockedPhone.locked = true
	androidDriver := newAndroid(lockedPhone)

	// Mock web driver representing WhatsApp Web running in background on laptop
	webMock := &mockWebDriver{
		name:   "whatsapp-web",
		device: "laptop",
		chats:  map[string][]Message{},
	}

	messenger := NewMessenger(nil, androidDriver, webMock)

	act := &domain.MessagingAction{
		Channel: domain.MessagingChannelWhatsApp,
		Op:      domain.MessagingSend,
		Contact: "bhai",
		Text:    "wassup",
		Prefer:  "phone", // User or system preferred phone
	}

	res, err := messenger.Do(ctx, act)
	require.NoError(t, err, "message send must succeed through fallback")
	assert.Equal(t, "whatsapp-web", res.Via, "must fall back to WhatsApp Web because phone is locked")
	assert.True(t, res.Sent)
	assert.Equal(t, "wassup", webMock.lastSent)
	assert.Equal(t, "bhai", webMock.lastContact)
}

type mockWebDriver struct {
	name        string
	device      string
	lastSent    string
	lastContact string
	chats       map[string][]Message
}

func (m *mockWebDriver) Name() string   { return m.name }
func (m *mockWebDriver) Device() string { return m.device }
func (m *mockWebDriver) Do(_ context.Context, act *domain.MessagingAction) (*Result, error) {
	switch act.Op {
	case domain.MessagingSend:
		m.lastSent = act.Text
		m.lastContact = act.Contact
		m.chats[act.Contact] = append(m.chats[act.Contact], Message{From: "me", Text: act.Text})
		return &Result{Via: m.name, Sent: true}, nil
	case domain.MessagingReadRecent:
		return &Result{Via: m.name, Messages: m.chats[act.Contact]}, nil
	}
	return &Result{Via: m.name}, nil
}
