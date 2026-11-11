package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DesktopKey is where a step's desktop action lives inside input_payload.
const DesktopKey = "desktop"

// DesktopOp is what to do on the desktop host.
type DesktopOp string

const (
	DesktopOpMedia  DesktopOp = "media"
	DesktopOpLaunch DesktopOp = "launch"
	DesktopOpEnv    DesktopOp = "env"
)

// DesktopAction is one desktop job. A step that carries one requires the "desktop" capability.
type DesktopAction struct {
	Op      DesktopOp `json:"op"`
	MediaOp string    `json:"media_op,omitempty"` // next, previous, play, pause, play-pause, status
	Player  string    `json:"player,omitempty"`   // spotify, chromium, etc.
	App     string    `json:"app,omitempty"`      // app name to launch
}

// ParseDesktopAction reads and validates the desktop action in a payload; (nil, nil) when there is none.
func ParseDesktopAction(payload json.RawMessage) (*DesktopAction, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(payload, &v) != nil {
		return nil, nil
	}
	raw, ok := v[DesktopKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var d DesktopAction
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%w: input_payload.desktop is not a valid action", ErrValidation)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Validate checks action properties.
func (d *DesktopAction) Validate() error {
	switch d.Op {
	case DesktopOpMedia:
		if strings.TrimSpace(d.MediaOp) == "" {
			d.MediaOp = "play-pause"
		}
		return nil
	case DesktopOpLaunch:
		if strings.TrimSpace(d.App) == "" {
			return fmt.Errorf("%w: desktop launch action needs app name", ErrValidation)
		}
		return nil
	case DesktopOpEnv:
		return nil
	default:
		return fmt.Errorf("%w: unknown desktop op %q", ErrValidation, d.Op)
	}
}
