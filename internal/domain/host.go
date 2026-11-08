package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HostKind is the kind of device a host runs on.
type HostKind string

const (
	HostKindLaptop HostKind = "laptop"
	HostKindPhone  HostKind = "phone"
)

// Host capabilities. A step declares what it requires; a host advertises what it can do; the owner
// grants what it may do. Only the intersection of granted and advertised is ever used.
const (
	// HostCapAgent means the host runs an agent session (agy or Claude Code) and can be the "brain".
	// A step that declares no requirement needs exactly this.
	HostCapAgent     = "agent"
	HostCapBrowser   = "browser"
	HostCapFiles     = "files"
	HostCapShell     = "shell"
	HostCapPhoneApps = "phone_apps"
	HostCapSMS       = "sms"
	HostCapCalls     = "calls"
	HostCapMessaging = "messaging"
	// HostCapWeb is read-only access to the public web: search and fetching pages.
	HostCapWeb = "web"
)

// AllHostCapabilities is the set of recognized host capabilities.
var AllHostCapabilities = map[string]bool{
	HostCapAgent:     true,
	HostCapBrowser:   true,
	HostCapFiles:     true,
	HostCapShell:     true,
	HostCapPhoneApps: true,
	HostCapSMS:       true,
	HostCapCalls:     true,
	HostCapMessaging: true,
	HostCapWeb:       true,
}

const (
	// RequiresKey is where a step's required capabilities live inside input_payload (no schema change).
	RequiresKey = "requires"

	// HostLiveWindow is how recent a host's last contact must be for it to count as online.
	HostLiveWindow = 45 * time.Second

	// HostWorkerPrefix prefixes the lease owner recorded on tasks a host runs. The cloud runner never
	// uses this prefix, so it is how host-run work is told apart from runner-run work.
	HostWorkerPrefix = "host:"
)

// ValidHostKind reports whether k is a known kind.
func ValidHostKind(k HostKind) bool { return k == HostKindLaptop || k == HostKindPhone }

// Host is a registered device that connects out to Griffin and runs work.
type Host struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	Name         string     `json:"name"`
	Kind         HostKind   `json:"kind"`
	Capabilities []string   `json:"capabilities"` // granted by the owner
	Advertised   []string   `json:"advertised"`   // reported by the host at its last contact
	Version      string     `json:"version"`
	Paused       bool       `json:"paused"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// WorkerID is the lease owner string used for tasks this host runs.
func (h *Host) WorkerID() string { return HostWorkerPrefix + h.ID.String() }

// EffectiveCapabilities is what the host may do right now: granted by the owner AND reported by the
// host. A host cannot grant itself anything.
func (h *Host) EffectiveCapabilities() []string {
	have := make(map[string]bool, len(h.Advertised))
	for _, c := range h.Advertised {
		have[c] = true
	}
	out := make([]string, 0, len(h.Capabilities))
	for _, c := range h.Capabilities {
		if have[c] {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// IsLive reports whether the host is online and allowed to take work: seen within HostLiveWindow,
// not revoked and not paused.
func (h *Host) IsLive(now time.Time) bool {
	if h.RevokedAt != nil || h.Paused || h.LastSeenAt == nil {
		return false
	}
	return now.Sub(*h.LastSeenAt) <= HostLiveWindow
}

// Covers reports whether the host's effective capabilities satisfy required. A step that requires
// nothing needs an agent, so only a host with the agent capability covers it.
func (h *Host) Covers(required []string) bool {
	have := make(map[string]bool)
	for _, c := range h.EffectiveCapabilities() {
		have[c] = true
	}
	if len(required) == 0 {
		return have[HostCapAgent]
	}
	for _, r := range required {
		if !have[r] {
			return false
		}
	}
	return true
}

// NormalizeHostCapabilities trims, lowercases, de-duplicates, sorts and validates capabilities.
// An unknown capability is an error: silently dropping one would turn a step that needs a browser
// into one that runs as plain text.
func NormalizeHostCapabilities(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if !AllHostCapabilities[c] {
			return nil, fmt.Errorf("%w: unknown host capability %q", ErrValidation, c)
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out, nil
}

type requiresView struct {
	Requires json.RawMessage `json:"requires"`
}

// RequiredCapabilities returns the capabilities a task's input_payload declares it needs, normalized.
// It returns nil for a payload that declares none (or is not a JSON object).
func RequiredCapabilities(payload json.RawMessage) []string {
	caps, err := parseRequires(payload)
	if err != nil {
		return nil
	}
	return caps
}

// ValidateRequires rejects a payload whose `requires` is malformed or names an unknown capability.
func ValidateRequires(payload json.RawMessage) error {
	_, err := parseRequires(payload)
	return err
}

func parseRequires(payload json.RawMessage) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v requiresView
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, nil // not an object: not our concern here
	}
	if len(v.Requires) == 0 || string(v.Requires) == "null" {
		return nil, nil
	}
	var raw []string
	if err := json.Unmarshal(v.Requires, &raw); err != nil {
		return nil, fmt.Errorf("%w: input_payload.requires must be an array of capability names", ErrValidation)
	}
	caps, err := NormalizeHostCapabilities(raw)
	if err != nil {
		return nil, err
	}
	if len(caps) == 0 {
		return nil, nil
	}
	return caps, nil
}

// HostMode says where the agent session (the "brain") runs right now.
type HostMode string

const (
	// ModeLaptop: a live laptop host with the agent capability is the brain and the hands.
	ModeLaptop HostMode = "laptop"
	// ModePhone: no live laptop agent, but a live phone host; the cloud runner is a text-only brain.
	ModePhone HostMode = "phone"
	// ModeNone: no live host. Plain text steps run on the cloud runner; steps that need hands wait.
	ModeNone HostMode = "none"
)

// ElectMode picks the mode from host presence. Laptop wins whenever a live laptop can run an agent.
func ElectMode(hosts []*Host, now time.Time) HostMode {
	phone := false
	for _, h := range hosts {
		if !h.IsLive(now) {
			continue
		}
		switch h.Kind {
		case HostKindLaptop:
			if h.Covers(nil) {
				return ModeLaptop
			}
		case HostKindPhone:
			phone = true
		}
	}
	if phone {
		return ModePhone
	}
	return ModeNone
}

// AnyHostCovers reports whether some live host can take a step that declares required.
func AnyHostCovers(hosts []*Host, required []string, now time.Time) bool {
	for _, h := range hosts {
		if h.IsLive(now) && h.Covers(required) {
			return true
		}
	}
	return false
}

// ErrHostRevoked is returned for a token whose host has been revoked.
var ErrHostRevoked = errors.New("host revoked")
