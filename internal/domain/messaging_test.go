package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func payload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestParseMessagingAction_AbsentIsNotAnError(t *testing.T) {
	for _, p := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"messaging":null}`), json.RawMessage(`[1]`)} {
		m, err := ParseMessagingAction(p)
		assert.NoError(t, err)
		assert.Nil(t, m)
	}
}

func TestParseMessagingAction_SendNormalizes(t *testing.T) {
	m, err := ParseMessagingAction(payload(t, map[string]any{"messaging": map[string]any{
		"op": "SEND", "contact": "  Mom ", "phone": "+91 98765-43210", "text": " running late ",
	}}))
	require.NoError(t, err)
	assert.Equal(t, MessagingChannelWhatsApp, m.Channel, "channel defaults to whatsapp")
	assert.Equal(t, MessagingSend, m.Op)
	assert.Equal(t, "Mom", m.Contact)
	assert.Equal(t, "919876543210", m.Phone)
	assert.Equal(t, "running late", m.Text)
	assert.True(t, m.Outward())
	assert.Equal(t, "Mom", m.Target())

	mInsta, err := ParseMessagingAction(payload(t, map[string]any{"messaging": map[string]any{
		"channel": "instagram", "op": "send", "contact": "sam_lee", "text": "hey",
	}}))
	require.NoError(t, err)
	assert.Equal(t, MessagingChannelInstagram, mInsta.Channel)
	assert.Equal(t, "sam_lee", mInsta.Contact)
}

func TestMessagingAction_Rejects(t *testing.T) {
	long := strings.Repeat("a", maxMessageText+1)
	cases := map[string]MessagingAction{
		"no recipient":     {Op: MessagingSend, Text: "hi"},
		"no text":          {Op: MessagingSend, Contact: "Mom"},
		"text too long":    {Op: MessagingSend, Contact: "Mom", Text: long},
		"unknown op":       {Op: "delete", Contact: "Mom"},
		"unknown channel":  {Channel: "telegram", Op: MessagingUnread},
		"read without who": {Op: MessagingReadRecent},
		"bad phone chars":  {Op: MessagingSend, Phone: "98765abc43210", Text: "hi"},
		"phone too short":  {Op: MessagingSend, Phone: "12345", Text: "hi"},
		"newline in name":  {Op: MessagingSend, Contact: "Mo\nm", Text: "hi"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			m := m
			err := m.Validate()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrValidation))
		})
	}
}

func TestMessagingAction_ReadDefaultsAndClamps(t *testing.T) {
	m := MessagingAction{Op: MessagingReadRecent, Contact: "Mom", Text: "ignored"}
	require.NoError(t, m.Validate())
	assert.Equal(t, defaultReadLimit, m.Limit)
	assert.Empty(t, m.Text, "a read carries no text")
	assert.False(t, m.Outward())
	m = MessagingAction{Op: MessagingReadRecent, Contact: "Mom", Limit: 500}
	require.NoError(t, m.Validate())
	assert.Equal(t, maxReadLimit, m.Limit)
}

// An approval must cover exactly one action: changing the text or the recipient changes the digest.
func TestMessagingAction_DigestBindsTheExactAction(t *testing.T) {
	base := MessagingAction{Op: MessagingSend, Contact: "Mom", Text: "hi"}
	require.NoError(t, base.Validate())
	same := MessagingAction{Op: MessagingSend, Contact: "mom", Text: "hi"}
	require.NoError(t, same.Validate())
	assert.Equal(t, base.Digest(), same.Digest(), "contact case does not matter")

	for name, other := range map[string]MessagingAction{
		"other text":   {Op: MessagingSend, Contact: "Mom", Text: "hi!"},
		"other person": {Op: MessagingSend, Contact: "Dad", Text: "hi"},
		"with phone":   {Op: MessagingSend, Contact: "Mom", Phone: "919876543210", Text: "hi"},
	} {
		o := other
		require.NoError(t, o.Validate(), name)
		assert.NotEqual(t, base.Digest(), o.Digest(), name)
	}
	assert.True(t, strings.HasPrefix(base.ApprovalActionType(), HostActionApprovalPrefix))
	assert.LessOrEqual(t, len(base.ApprovalActionType()), 100, "fits approvals.action_type VARCHAR(100)")
}

func TestValidateMessaging_StepMustRequireTheCapability(t *testing.T) {
	act := map[string]any{"op": "send", "contact": "Mom", "text": "hi"}
	assert.Error(t, ValidateMessaging(payload(t, map[string]any{"messaging": act})), "no requires")
	assert.Error(t, ValidateMessaging(payload(t, map[string]any{"messaging": act, "requires": []string{"browser"}})))
	assert.NoError(t, ValidateMessaging(payload(t, map[string]any{"messaging": act, "requires": []string{"messaging"}})))
	assert.NoError(t, ValidateMessaging(payload(t, map[string]any{"requires": []string{"browser"}})), "no action, nothing to check")
}

func TestSummary_NeverExceedsAReasonableLength(t *testing.T) {
	m := MessagingAction{Op: MessagingSend, Contact: "Mom", Text: strings.Repeat("x", 3000)}
	require.NoError(t, m.Validate())
	assert.Less(t, len(m.Summary()), 400)
}
