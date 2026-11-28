package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FunctionCall is the model asking for one tool to run.
type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// FunctionResponse is what a tool returned, sent back as the next turn.
type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Part is one piece of a Content: text, a function call or a function response. ThoughtSignature is opaque; it is
// sent back exactly as received, which newer models require between a function call and its response.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

// Content is one turn of a tool conversation. Role is "user" or "model"; function responses go in a "user" turn.
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// FunctionDecl declares one tool. Parameters is an OpenAPI-style object schema; nil means no arguments.
type FunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolReply is the model's answer: text, or tool calls to run. Raw is the model's turn, to append to the
// conversation unchanged before the function responses.
type ToolReply struct {
	Text  string
	Calls []FunctionCall
	Raw   Content
}

// ChatWithTools sends the conversation with the declared tools and returns the model's text or its tool calls.
// Like Chat, it asks the fallback model once when the main model is overloaded, rate limited or retired.
func (c *Client) ChatWithTools(ctx context.Context, system string, contents []Content, decls []FunctionDecl) (ToolReply, error) {
	out, err := c.chatTools(ctx, system, contents, decls)
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
	return alt.chatTools(ctx, system, contents, decls)
}

func (c *Client) chatTools(ctx context.Context, system string, contents []Content, decls []FunctionDecl) (ToolReply, error) {
	if !c.Enabled() {
		return ToolReply{}, ErrNotConfigured
	}
	if len(contents) == 0 {
		return ToolReply{}, errors.New("gemini: no conversation to send")
	}
	body := map[string]any{
		"systemInstruction": Content{Parts: []Part{{Text: system}}},
		"contents":          contents,
	}
	if len(decls) > 0 {
		body["tools"] = []any{map[string]any{"functionDeclarations": decls}}
	}
	maxOut := c.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = 1500
	}
	temp := 0.2
	if c.Temperature != nil {
		temp = *c.Temperature
	}
	body["generationConfig"] = map[string]any{"maxOutputTokens": maxOut, "temperature": temp}
	raw, err := json.Marshal(body)
	if err != nil {
		return ToolReply{}, err
	}
	data, err := c.post(ctx, raw)
	if err != nil {
		return ToolReply{}, err
	}
	var out struct {
		Candidates []struct {
			Content      Content `json:"content"`
			FinishReason string  `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return ToolReply{}, fmt.Errorf("gemini: unreadable reply: %w", err)
	}
	if out.PromptFeedback.BlockReason != "" {
		return ToolReply{}, ErrBlocked
	}
	if len(out.Candidates) == 0 {
		return ToolReply{}, errors.New("gemini: no answer in the reply")
	}
	cand := out.Candidates[0]
	reply := ToolReply{Raw: cand.Content}
	reply.Raw.Role = "model"
	var b strings.Builder
	for _, p := range cand.Content.Parts {
		b.WriteString(p.Text)
		if p.FunctionCall != nil {
			reply.Calls = append(reply.Calls, *p.FunctionCall)
		}
	}
	reply.Text = strings.TrimSpace(b.String())
	if reply.Text == "" && len(reply.Calls) == 0 {
		if cand.FinishReason == "SAFETY" {
			return ToolReply{}, ErrBlocked
		}
		return ToolReply{}, errors.New("gemini: the answer was empty")
	}
	return reply, nil
}
