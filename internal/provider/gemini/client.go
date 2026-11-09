// Package gemini is the fast voice brain: a small client for Gemini's generateContent endpoint, used to
// answer chat in about a second while the worker (agy on the EC2) is still starting. It never holds
// tools and never acts: it only turns text into text. The API key stays on the server.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultModel is small and quick; the "latest" alias follows Google's current lite model.
const DefaultModel = "gemini-flash-lite-latest"

const defaultBaseURL = "https://generativelanguage.googleapis.com"

var (
	// ErrNotConfigured means there is no API key, so callers should use the normal worker path.
	ErrNotConfigured = errors.New("gemini: no API key configured")
	// ErrRateLimited means the key's quota is used up for now.
	ErrRateLimited = errors.New("gemini: rate limited")
	// ErrBlocked means the model declined to answer.
	ErrBlocked = errors.New("gemini: the model declined to answer")
)

// Turn is one message of a conversation. Role is "user" or "assistant".
type Turn struct {
	Role string
	Text string
}

// Client calls the Gemini API.
type Client struct {
	APIKey  string
	Model   string
	BaseURL string // empty means Google's endpoint; tests point it at a stand-in
	HTTP    *http.Client
	// MaxOutputTokens bounds one reply. Voice answers are short.
	MaxOutputTokens int
}

// Enabled reports whether the client can be used.
func (c *Client) Enabled() bool { return c != nil && strings.TrimSpace(c.APIKey) != "" }

// Chat answers the last user turn in the voice of system. Turns must end with a user turn.
func (c *Client) Chat(ctx context.Context, system string, turns []Turn) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	if len(turns) == 0 || turns[len(turns)-1].Role != "user" {
		return "", errors.New("gemini: the conversation must end with a user message")
	}
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		SystemInstruction content   `json:"systemInstruction"`
		Contents          []content `json:"contents"`
		GenerationConfig  struct {
			MaxOutputTokens int     `json:"maxOutputTokens"`
			Temperature     float64 `json:"temperature"`
		} `json:"generationConfig"`
	}{SystemInstruction: content{Parts: []part{{Text: system}}}}
	body.GenerationConfig.MaxOutputTokens = c.MaxOutputTokens
	if body.GenerationConfig.MaxOutputTokens <= 0 {
		body.GenerationConfig.MaxOutputTokens = 300
	}
	body.GenerationConfig.Temperature = 0.7
	for _, t := range turns {
		role := "user"
		if t.Role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: t.Text}}})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	endpoint := base + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey) // a header, so the key never appears in a URL or a log

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini: request failed: %w", scrub(err, c.APIKey))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("gemini: reading the reply: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("gemini: HTTP %d: %s", resp.StatusCode, apiMessage(data))
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("gemini: unreadable reply: %w", err)
	}
	if out.PromptFeedback.BlockReason != "" {
		return "", ErrBlocked
	}
	if len(out.Candidates) == 0 {
		return "", errors.New("gemini: no answer in the reply")
	}
	var b strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		if out.Candidates[0].FinishReason == "SAFETY" {
			return "", ErrBlocked
		}
		return "", errors.New("gemini: the answer was empty")
	}
	return text, nil
}

func apiMessage(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return truncate(e.Error.Message, 160)
	}
	return truncate(string(data), 160)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// scrub removes the key from an error, in case a transport error ever echoes the request.
func scrub(err error, key string) error {
	if key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "[key]"))
}
