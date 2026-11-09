// Package notify sends a push notification to the owner's phone through ntfy (a free, open push service: the
// phone runs its app, subscribes to a private topic, and anything published to that topic arrives as a
// notification). Tapping it opens a Griffin page. The topic name is the secret: nobody can read or publish to
// a topic they do not know, and a self-hosted server or an access token can protect it further.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is one notification.
type Message struct {
	Title string
	Body  string
	// ClickURL opens when the notification is tapped, and is also offered as a "Listen" button.
	ClickURL string
	// Priority is ntfy's 1 (min) to 5 (max); 0 means default.
	Priority int
	Tags     []string
}

// Sender delivers notifications. Implementations must be safe to call from several goroutines.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Ntfy publishes to an ntfy server.
type Ntfy struct {
	Server string // default https://ntfy.sh
	Topic  string
	Token  string // optional access token for a protected topic
	HTTP   *http.Client
}

// Enabled reports whether a topic is configured.
func (n *Ntfy) Enabled() bool { return n != nil && strings.TrimSpace(n.Topic) != "" }

// Send publishes m. The token and topic never appear in an error.
func (n *Ntfy) Send(ctx context.Context, m Message) error {
	if !n.Enabled() {
		return errors.New("notify: no ntfy topic configured")
	}
	server := strings.TrimRight(strings.TrimSpace(n.Server), "/")
	if server == "" {
		server = "https://ntfy.sh"
	}
	body := map[string]any{"topic": n.Topic, "title": clip(m.Title, 120), "message": clip(m.Body, 500)}
	if m.ClickURL != "" {
		body["click"] = m.ClickURL
		body["actions"] = []map[string]any{{"action": "view", "label": "Listen", "url": m.ClickURL}}
	}
	if m.Priority >= 1 && m.Priority <= 5 {
		body["priority"] = m.Priority
	}
	if len(m.Tags) > 0 {
		body["tags"] = m.Tags
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}
	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: %w", scrub(err, n.Topic, n.Token))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("notify: ntfy answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func scrub(err error, secrets ...string) error {
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "[redacted]")
		}
	}
	return errors.New(msg)
}
