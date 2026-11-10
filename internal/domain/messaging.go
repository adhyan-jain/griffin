package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// MessagingKey is where a step's messaging action lives inside input_payload (no schema change).
const MessagingKey = "messaging"

// HostActionApprovalPrefix starts the approval action type for an outward action a host wants to take.
// The rest is a digest of the exact action, so an approval covers that message to that contact and
// nothing else: a changed text or recipient needs a new approval.
const HostActionApprovalPrefix = "host_action:"

// MessagingOp is what to do in a chat app.
type MessagingOp string

const (
	MessagingSend       MessagingOp = "send"        // send Text to a contact
	MessagingReadRecent MessagingOp = "read_recent" // read the last messages of one chat
	MessagingUnread     MessagingOp = "unread"      // list chats with unread messages
)

// Supported messaging channels.
const (
	MessagingChannelWhatsApp  = "whatsapp"
	MessagingChannelInstagram = "instagram"
)

const (
	maxMessageText   = 4000
	maxContactName   = 100
	defaultReadLimit = 10
	maxReadLimit     = 30
	minPhoneDigits   = 7
	maxPhoneDigits   = 15
)

// MessagingAction is one thing to do in a chat app, as a plan step declares it. A step that carries one
// also requires the "messaging" capability.
type MessagingAction struct {
	Channel string      `json:"channel"`
	Op      MessagingOp `json:"op"`
	// Contact is the name as the owner says it. Phone is digits with the country code and no "+", and is
	// the more reliable way to open a chat. At least one is needed for send and read_recent.
	Contact string `json:"contact,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Text    string `json:"text,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	// Prefer says which device to try first when both can do the job: "laptop" (WhatsApp Web) or "phone"
	// (the WhatsApp app). Empty means the laptop first. It changes where the action runs, not what it does,
	// so it is not part of the digest an approval is bound to.
	Prefer string `json:"prefer,omitempty"`
}

// ParseMessagingAction reads and validates the messaging action in a payload. It returns (nil, nil)
// when the payload has none, and an ErrValidation error when one is present but unusable.
func ParseMessagingAction(payload json.RawMessage) (*MessagingAction, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, nil // not an object: not our concern here
	}
	raw, ok := v[MessagingKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var m MessagingAction
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: input_payload.messaging is not a valid action", ErrValidation)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate normalizes and checks the action. A bad action is refused at creation: guessing at a
// recipient or text is how a message goes to the wrong person.
func (m *MessagingAction) Validate() error {
	m.Channel = strings.ToLower(strings.TrimSpace(m.Channel))
	if m.Channel == "" {
		m.Channel = MessagingChannelWhatsApp
	}
	if m.Channel != MessagingChannelWhatsApp && m.Channel != MessagingChannelInstagram {
		return fmt.Errorf("%w: messaging channel %q is not supported (only whatsapp and instagram)", ErrValidation, m.Channel)
	}
	m.Op = MessagingOp(strings.ToLower(strings.TrimSpace(string(m.Op))))
	m.Prefer = strings.ToLower(strings.TrimSpace(m.Prefer))
	if m.Prefer != "" && m.Prefer != string(HostKindLaptop) && m.Prefer != string(HostKindPhone) {
		return fmt.Errorf("%w: prefer must be laptop or phone", ErrValidation)
	}
	m.Contact = strings.TrimSpace(m.Contact)
	if len([]rune(m.Contact)) > maxContactName || hasControl(m.Contact) {
		return fmt.Errorf("%w: contact name is too long or has control characters", ErrValidation)
	}
	phone, err := normalizePhone(m.Phone)
	if err != nil {
		return err
	}
	m.Phone = phone
	switch m.Op {
	case MessagingSend:
		if m.Contact == "" && m.Phone == "" {
			return fmt.Errorf("%w: a message needs a contact or a phone number", ErrValidation)
		}
		m.Text = strings.TrimSpace(m.Text)
		if m.Text == "" {
			return fmt.Errorf("%w: a message needs text", ErrValidation)
		}
		if len([]rune(m.Text)) > maxMessageText {
			return fmt.Errorf("%w: a message is at most %d characters", ErrValidation, maxMessageText)
		}
		m.Limit = 0
	case MessagingReadRecent:
		if m.Contact == "" && m.Phone == "" {
			return fmt.Errorf("%w: reading a chat needs a contact or a phone number", ErrValidation)
		}
		if m.Limit <= 0 {
			m.Limit = defaultReadLimit
		}
		if m.Limit > maxReadLimit {
			m.Limit = maxReadLimit
		}
		m.Text = ""
	case MessagingUnread:
		m.Text, m.Limit = "", 0
	default:
		return fmt.Errorf("%w: messaging op must be send, read_recent or unread", ErrValidation)
	}
	return nil
}

// Target is who the action is about, for the permission check and the approval prompt.
func (m *MessagingAction) Target() string {
	if m.Contact != "" {
		return m.Contact
	}
	return m.Phone
}

// Outward reports whether the action changes something outside Griffin (a sent message). Reading is not.
func (m *MessagingAction) Outward() bool { return m.Op == MessagingSend }

// Digest identifies the exact action. It covers the channel, op, recipient and text.
func (m *MessagingAction) Digest() string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		m.Channel, string(m.Op), strings.ToLower(m.Contact), m.Phone, m.Text, fmt.Sprint(m.Limit),
	}, "\x1f")))
	return hex.EncodeToString(h[:16])
}

// ApprovalActionType is the approval action type that grants exactly this action.
func (m *MessagingAction) ApprovalActionType() string { return HostActionApprovalPrefix + m.Digest() }

// Summary is the one-line description shown to the owner when approval is needed.
func (m *MessagingAction) Summary() string {
	switch m.Op {
	case MessagingSend:
		return fmt.Sprintf("Send a WhatsApp message to %s: %q", m.Target(), truncateRunes(m.Text, 200))
	case MessagingReadRecent:
		return fmt.Sprintf("Read the last %d WhatsApp messages with %s", m.Limit, m.Target())
	default:
		return "List WhatsApp chats with unread messages"
	}
}

// ValidateMessaging checks a task payload at creation: a messaging action must be valid and the step
// must require the messaging capability, otherwise a host without it could be handed the step.
func ValidateMessaging(payload json.RawMessage) error {
	m, err := ParseMessagingAction(payload)
	if err != nil || m == nil {
		return err
	}
	caps, err := parseRequires(payload)
	if err != nil {
		return err
	}
	for _, c := range caps {
		if c == HostCapMessaging {
			return nil
		}
	}
	return fmt.Errorf("%w: a step with a messaging action must require the %q capability", ErrValidation, HostCapMessaging)
}

func normalizePhone(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// formatting characters are dropped
		default:
			return "", fmt.Errorf("%w: phone number may contain only digits, spaces and + - ( ) .", ErrValidation)
		}
	}
	d := b.String()
	if len(d) < minPhoneDigits || len(d) > maxPhoneDigits {
		return "", fmt.Errorf("%w: phone number must have %d to %d digits including the country code", ErrValidation, minPhoneDigits, maxPhoneDigits)
	}
	return d, nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
