package domain

import (
	"encoding/json"
	"strings"
)

// ClaudeOptionsKey is where a step's Claude settings live in its input_payload (no schema change).
const ClaudeOptionsKey = "claude"

// ClaudeStepOptions tell the Claude provider how to run one step. A "lead" step hands the whole job to
// Claude, which splits it among its own sub-agents (each on the cheapest model that can do its part)
// instead of Griffin splitting it into separate workers. Every value is checked here: a payload that asks
// for something unknown gets the owner's default, never a pass-through to the command line.
type ClaudeStepOptions struct {
	Lead   bool    `json:"lead"`
	Model  string  `json:"model,omitempty"`  // haiku, sonnet, opus or fable
	Effort string  `json:"effort,omitempty"` // low, medium, high, xhigh or max
	Budget float64 `json:"budget_usd,omitempty"`
}

var (
	claudeModels  = map[string]bool{"haiku": true, "sonnet": true, "opus": true, "fable": true}
	claudeEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}
)

// ParseClaudeOptions reads and sanitises a step's Claude options. Unknown models and efforts become ""
// (meaning: use the provider's default) and a negative or absurd budget becomes 0.
func ParseClaudeOptions(payload json.RawMessage) ClaudeStepOptions {
	var v struct {
		Claude map[string]any `json:"claude"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &v) != nil || v.Claude == nil {
		return ClaudeStepOptions{}
	}
	// Each field is read on its own, so one malformed value cannot discard the others.
	var o ClaudeStepOptions
	o.Lead, _ = v.Claude["lead"].(bool)
	if m, ok := v.Claude["model"].(string); ok {
		if m = strings.ToLower(strings.TrimSpace(m)); claudeModels[m] {
			o.Model = m
		}
	}
	if e, ok := v.Claude["effort"].(string); ok {
		if e = strings.ToLower(strings.TrimSpace(e)); claudeEfforts[e] {
			o.Effort = e
		}
	}
	if b, ok := v.Claude["budget_usd"].(float64); ok && b > 0 && b <= 100 {
		o.Budget = b
	}
	return o
}

// WithClaudeLead marks a step payload as a Claude lead step. A payload that is not a JSON object is
// returned unchanged.
func WithClaudeLead(payload json.RawMessage, opts ClaudeStepOptions) json.RawMessage {
	var m map[string]json.RawMessage
	if len(payload) == 0 {
		m = map[string]json.RawMessage{}
	} else if json.Unmarshal(payload, &m) != nil || m == nil {
		return payload
	}
	opts.Lead = true
	enc, _ := json.Marshal(opts)
	m[ClaudeOptionsKey] = enc
	hint, _ := json.Marshal(ProviderClaude)
	m[ProviderHintKey] = hint
	out, err := json.Marshal(m)
	if err != nil {
		return payload
	}
	return out
}
