package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// WebKey is where a step's web action lives inside input_payload.
const WebKey = "web"

// WebOp is what to do on the public web.
type WebOp string

const (
	WebSearch WebOp = "search" // search the web for Query
	WebFetch  WebOp = "fetch"  // read a page (URL), or the top results of the previous search step
)

const (
	maxQueryChars     = 300
	maxURLChars       = 2000
	defaultSearchHits = 5
	maxSearchHits     = 10
	defaultReadPages  = 3
	maxReadPages      = 5
)

// WebAction is one read-only web job. A step that carries one also requires the "web" capability.
type WebAction struct {
	Op    WebOp  `json:"op"`
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
	// Limit is how many results (search) or pages (fetch from a previous search) to use.
	Limit int `json:"limit,omitempty"`
	// FromSearch makes a fetch read the top results of the previous step instead of a fixed URL.
	FromSearch bool `json:"from_search,omitempty"`
}

// ParseWebAction reads and validates the web action in a payload; (nil, nil) when there is none.
func ParseWebAction(payload json.RawMessage) (*WebAction, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(payload, &v) != nil {
		return nil, nil
	}
	raw, ok := v[WebKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var w WebAction
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("%w: input_payload.web is not a valid action", ErrValidation)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// Validate normalizes and checks the action. Only plain http(s) addresses without embedded credentials are
// accepted; whether an address is safe to reach (not a private network) is checked again when it is fetched.
func (w *WebAction) Validate() error {
	w.Op = WebOp(strings.ToLower(strings.TrimSpace(string(w.Op))))
	switch w.Op {
	case WebSearch:
		w.Query = strings.Join(strings.Fields(w.Query), " ")
		if w.Query == "" || len([]rune(w.Query)) > maxQueryChars {
			return fmt.Errorf("%w: a search needs a query of up to %d characters", ErrValidation, maxQueryChars)
		}
		if w.Limit <= 0 {
			w.Limit = defaultSearchHits
		}
		if w.Limit > maxSearchHits {
			w.Limit = maxSearchHits
		}
		w.URL, w.FromSearch = "", false
	case WebFetch:
		if w.FromSearch {
			if w.Limit <= 0 {
				w.Limit = defaultReadPages
			}
			if w.Limit > maxReadPages {
				w.Limit = maxReadPages
			}
			w.URL, w.Query = "", ""
			return nil
		}
		w.URL = strings.TrimSpace(w.URL)
		if err := ValidateWebURL(w.URL); err != nil {
			return err
		}
		w.Limit, w.Query = 0, ""
	default:
		return fmt.Errorf("%w: web op must be search or fetch", ErrValidation)
	}
	return nil
}

// ValidateWebURL checks the shape of an address a step may fetch.
func ValidateWebURL(raw string) error {
	if raw == "" || len(raw) > maxURLChars {
		return fmt.Errorf("%w: a fetch needs a web address of up to %d characters", ErrValidation, maxURLChars)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%w: only http and https addresses can be fetched", ErrValidation)
	}
	if u.User != nil {
		return fmt.Errorf("%w: addresses with a user name or password are refused", ErrValidation)
	}
	return nil
}

// ValidateWeb checks a task payload at creation: a web action must be valid and the step must require the
// web capability.
func ValidateWeb(payload json.RawMessage) error {
	w, err := ParseWebAction(payload)
	if err != nil || w == nil {
		return err
	}
	caps, err := parseRequires(payload)
	if err != nil {
		return err
	}
	for _, c := range caps {
		if c == HostCapWeb {
			return nil
		}
	}
	return fmt.Errorf("%w: a step with a web action must require the %q capability", ErrValidation, HostCapWeb)
}

// Digest identifies the exact action, for an approval when the owner has set web access to "ask".
func (w *WebAction) Digest() string {
	h := sha256.Sum256([]byte(strings.Join([]string{string(w.Op), w.Query, w.URL, fmt.Sprint(w.Limit), fmt.Sprint(w.FromSearch)}, "\x1f")))
	return hex.EncodeToString(h[:16])
}

// ApprovalActionType is the approval action type that grants exactly this action.
func (w *WebAction) ApprovalActionType() string {
	return HostActionApprovalPrefix + "web:" + w.Digest()
}

// Summary is the one-line description shown to the owner.
func (w *WebAction) Summary() string {
	switch {
	case w.Op == WebSearch:
		return fmt.Sprintf("Search the web for %q", truncateRunes(w.Query, 200))
	case w.FromSearch:
		return fmt.Sprintf("Read the top %d pages from the previous search", w.Limit)
	default:
		return "Read the web page " + truncateRunes(w.URL, 200)
	}
}
