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

// Image holds base64-encoded image data and its MIME type for multimodal inference.
type Image struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded data
}

// Turn is one message of a conversation. Role is "user" or "assistant".
type Turn struct {
	Role   string  `json:"role"`
	Text   string  `json:"text"`
	Images []Image `json:"images,omitempty"`
}

// Client calls the Gemini API.
type Client struct {
	APIKey  string
	Model   string
	BaseURL string // empty means Google's endpoint; tests point it at a stand-in
	HTTP    *http.Client
	// MaxOutputTokens bounds one reply. Voice answers are short.
	MaxOutputTokens int
	// Temperature, when set, replaces the conversational default (0.7); a classifier sets 0.
	Temperature *float64
	// JSONOutput asks for a JSON reply (responseMimeType application/json). The caller still validates it.
	JSONOutput bool
	// Fallback is the model asked once when Model is overloaded (503/5xx), rate limited (429) or retired (404).
	// Empty means DefaultFallbackModel; "none" turns the fallback off.
	Fallback string
}

// DefaultFallbackModel answers when the main model is overloaded: Google's lighter flash model.
const DefaultFallbackModel = "gemini-flash-lite-latest"

// Enabled reports whether the client can be used.
func (c *Client) Enabled() bool { return c != nil && strings.TrimSpace(c.APIKey) != "" }

// Chat answers the last user turn in the voice of system. Turns must end with a user turn. When the model is
// overloaded, rate limited or retired, the fallback model is asked once.
func (c *Client) Chat(ctx context.Context, system string, turns []Turn) (string, error) {
	out, err := c.chat(ctx, system, turns)
	if err == nil || !unavailable(err) || ctx.Err() != nil {
		return out, err
	}
	fb := c.Fallback
	if fb == "" {
		fb = DefaultFallbackModel
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	if fb == "none" || fb == model {
		return out, err
	}
	alt := *c
	alt.Model, alt.Fallback = fb, "none"
	return alt.chat(ctx, system, turns)
}

// unavailable reports whether err means the model could not answer right now (or no longer exists), so another
// model may.
func unavailable(err error) bool {
	if errors.Is(err, ErrRateLimited) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "gemini: HTTP 5") || strings.Contains(msg, "gemini: HTTP 404")
}

func (c *Client) chat(ctx context.Context, system string, turns []Turn) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	if len(turns) == 0 || turns[len(turns)-1].Role != "user" {
		return "", errors.New("gemini: the conversation must end with a user message")
	}
	type inlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inlineData,omitempty"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		SystemInstruction content   `json:"systemInstruction"`
		Contents          []content `json:"contents"`
		GenerationConfig  struct {
			MaxOutputTokens  int     `json:"maxOutputTokens"`
			Temperature      float64 `json:"temperature"`
			ResponseMIMEType string  `json:"responseMimeType,omitempty"`
		} `json:"generationConfig"`
	}{SystemInstruction: content{Parts: []part{{Text: system}}}}
	body.GenerationConfig.MaxOutputTokens = c.MaxOutputTokens
	if body.GenerationConfig.MaxOutputTokens <= 0 {
		body.GenerationConfig.MaxOutputTokens = 300
	}
	body.GenerationConfig.Temperature = 0.7
	if c.Temperature != nil {
		body.GenerationConfig.Temperature = *c.Temperature
	}
	if c.JSONOutput {
		body.GenerationConfig.ResponseMIMEType = "application/json"
	}
	for _, t := range turns {
		role := "user"
		if t.Role == "assistant" {
			role = "model"
		}
		var parts []part
		if t.Text != "" {
			parts = append(parts, part{Text: t.Text})
		}
		for _, img := range t.Images {
			if strings.TrimSpace(img.Data) != "" {
				mime := img.MimeType
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, part{InlineData: &inlineData{
					MimeType: mime,
					Data:     img.Data,
				}})
			}
		}
		if len(parts) == 0 {
			parts = append(parts, part{Text: " "})
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: parts})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	data, err := c.post(ctx, raw)
	if err != nil {
		return "", err
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

// post sends one generateContent request body to c.Model with the retry loop shared by Chat and ChatWithTools.
func (c *Client) post(ctx context.Context, raw []byte) ([]byte, error) {
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	endpoint := base + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	var lastErr error
	var data []byte
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 1200 * time.Millisecond):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", c.APIKey) // a header, so the key never appears in a URL or a log

		client := c.HTTP
		if client == nil {
			client = &http.Client{Timeout: 15 * time.Second}
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("gemini: request failed: %w", scrub(err, c.APIKey))
			continue
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("gemini: reading the reply: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if resp.StatusCode == http.StatusTooManyRequests {
				lastErr = ErrRateLimited
			} else {
				lastErr = fmt.Errorf("gemini: HTTP %d: %s", resp.StatusCode, apiMessage(data))
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gemini: HTTP %d: %s", resp.StatusCode, apiMessage(data))
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return data, nil

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
