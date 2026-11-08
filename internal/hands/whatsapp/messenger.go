// Package whatsapp drives WhatsApp for the owner on their own devices: WhatsApp Web in the laptop's
// browser first, then the WhatsApp app on their Android phone over adb. It never talks to WhatsApp's
// servers itself and holds no credentials: the browser profile and the phone already hold the login.
//
// The rule that matters most is in Messenger.Do: a second way of doing the job is tried only when the
// first provably did nothing. After a send that may have gone out, there is no fallback and no retry,
// because a second attempt could message the contact twice.
package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Message is one chat message.
type Message struct {
	// From is "me" or "them".
	From string `json:"from"`
	Text string `json:"text"`
	// Time is the sender's timestamp text when WhatsApp shows one; it is not parsed.
	Time string `json:"time,omitempty"`
}

// Chat is one row of the chat list.
type Chat struct {
	Name    string `json:"name"`
	Preview string `json:"preview,omitempty"`
	Unread  int    `json:"unread"`
}

// Result is what a driver reports for one action.
type Result struct {
	// Via names the driver that did the work, for example "whatsapp-web" or "android".
	Via      string    `json:"via"`
	Sent     bool      `json:"sent,omitempty"`
	Messages []Message `json:"messages,omitempty"`
	Chats    []Chat    `json:"chats,omitempty"`
	// Notes records how the job went, including drivers that were skipped and why.
	Notes []string `json:"notes,omitempty"`
}

// Errors drivers return. The first kind lets the Messenger move to the next driver; the others stop it.
var (
	// ErrNotReady means the driver could not start and touched nothing (not signed in, browser or phone
	// missing, phone locked). It is the only error that allows a fallback.
	ErrNotReady = errors.New("driver not ready")
	// ErrContactNotFound means no chat matched exactly. Another driver would see the same contacts, so this
	// stops the job. A fuzzy match is never used: a wrong recipient is worse than no message.
	ErrContactNotFound = errors.New("contact not found")
	// ErrAmbiguousContact means more than one chat matched the name.
	ErrAmbiguousContact = errors.New("more than one contact matches that name")
	// ErrUnconfirmed means a send was attempted and its delivery to WhatsApp could not be confirmed. The
	// message may or may not have gone out, so nothing is retried.
	ErrUnconfirmed = errors.New("could not confirm the message was sent")
)

// Driver is one way of operating WhatsApp.
type Driver interface {
	Name() string
	// Device is "laptop" or "phone": where the work happens.
	Device() string
	// Do performs the action. It must return ErrNotReady, and nothing else, for any failure that happens
	// before it has changed anything.
	Do(ctx context.Context, act *domain.MessagingAction) (*Result, error)
}

// Messenger tries its drivers in order.
type Messenger struct {
	drivers []Driver
	logger  *slog.Logger
	// AttemptTimeout bounds one driver's whole attempt. Default 90 s.
	AttemptTimeout time.Duration
}

// NewMessenger builds a Messenger. The order is the order of preference.
func NewMessenger(logger *slog.Logger, drivers ...Driver) *Messenger {
	if logger == nil {
		logger = slog.Default()
	}
	return &Messenger{drivers: drivers, logger: logger, AttemptTimeout: 90 * time.Second}
}

// Names lists the configured drivers in order.
func (m *Messenger) Names() []string {
	out := make([]string, 0, len(m.drivers))
	for _, d := range m.drivers {
		out = append(out, d.Name())
	}
	return out
}

// Do runs the action on the first driver that is ready. See the package comment for the fallback rule.
func (m *Messenger) Do(ctx context.Context, act *domain.MessagingAction) (*Result, error) {
	if len(m.drivers) == 0 {
		return nil, fmt.Errorf("%w: no way of reaching WhatsApp is configured on this device", ErrNotReady)
	}
	var skipped []string
	for _, d := range m.ordered(act.Prefer) {
		actx, cancel := context.WithTimeout(ctx, m.AttemptTimeout)
		res, err := d.Do(actx, act)
		cancel()
		if err == nil {
			if res == nil {
				res = &Result{}
			}
			res.Via = d.Name()
			res.Notes = append(skipped, res.Notes...)
			return res, nil
		}
		if errors.Is(err, ErrNotReady) {
			m.logger.Warn("whatsapp driver not ready; trying the next one", "driver", d.Name(), "reason", err.Error())
			skipped = append(skipped, fmt.Sprintf("%s was not available (%s)", d.Name(), trimReason(err)))
			continue
		}
		// Anything else may have changed something. Stop here and say which driver it was.
		return nil, fmt.Errorf("%s: %w", d.Name(), err)
	}
	return nil, fmt.Errorf("%w: %s", ErrNotReady, strings.Join(skipped, "; "))
}

// ordered puts the drivers on the preferred device first and keeps the rest as fallbacks, in their
// configured order. With no preference the configured order stands (laptop first).
func (m *Messenger) ordered(prefer string) []Driver {
	if prefer == "" {
		return m.drivers
	}
	out := make([]Driver, 0, len(m.drivers))
	for _, d := range m.drivers {
		if d.Device() == prefer {
			out = append(out, d)
		}
	}
	for _, d := range m.drivers {
		if d.Device() != prefer {
			out = append(out, d)
		}
	}
	return out
}

func trimReason(err error) string {
	s := err.Error()
	s = strings.TrimPrefix(s, ErrNotReady.Error()+": ")
	return s
}

// notReady wraps a reason so errors.Is(err, ErrNotReady) holds.
func notReady(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrNotReady, fmt.Sprintf(format, a...))
}

// normalizeName is how contact names are compared: trimmed, whitespace collapsed, case-insensitive.
func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
