package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ShellKey is where a step's laptop shell, terminal or app action lives inside input_payload.
const ShellKey = "shell"

// ShellOp is what to do on the laptop host.
type ShellOp string

const (
	ShellOpRun          ShellOp = "shell_run"
	ShellOpTerminalOpen ShellOp = "terminal_open"
	ShellOpTerminalSend ShellOp = "terminal_send"
	ShellOpTerminalRead ShellOp = "terminal_read"
	ShellOpAppOpen      ShellOp = "app_open"
	ShellOpAppList      ShellOp = "app_list"
)

const (
	// ShellMaxTimeoutS is the longest a shell_run may take.
	ShellMaxTimeoutS = 300
	// ShellDefaultTimeoutS applies when a shell_run names no timeout.
	ShellDefaultTimeoutS = 60
	// ShellMaxReadLines is the most terminal lines one terminal_read returns.
	ShellMaxReadLines = 200
)

var shellSessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// ShellAction is one laptop job. Terminal and shell ops need the "shell" capability, app ops the "desktop" one.
type ShellAction struct {
	Op       ShellOp `json:"op"`
	Command  string  `json:"command,omitempty"`   // shell_run command; optional first command for terminal_open
	Cwd      string  `json:"cwd,omitempty"`       // working directory
	TimeoutS int     `json:"timeout_s,omitempty"` // shell_run, at most ShellMaxTimeoutS
	Session  string  `json:"session,omitempty"`   // terminal name; the tmux session is griffin-<session>
	Text     string  `json:"text,omitempty"`      // terminal_send: literal text typed into the terminal
	NoEnter  bool    `json:"no_enter,omitempty"`  // terminal_send: do not press Enter after the text
	Lines    int     `json:"lines,omitempty"`     // terminal_read, at most ShellMaxReadLines
	Name     string  `json:"name,omitempty"`      // app_open: app name; app_list: search text
}

// RequiredCapability is the host capability this op needs.
func (s ShellOp) RequiredCapability() string {
	if s == ShellOpAppOpen || s == ShellOpAppList {
		return HostCapDesktop
	}
	return HostCapShell
}

// ParseShellAction reads and validates the shell action in a payload; (nil, nil) when there is none.
func ParseShellAction(payload json.RawMessage) (*ShellAction, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(payload, &v) != nil {
		return nil, nil
	}
	raw, ok := v[ShellKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var a ShellAction
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("%w: input_payload.shell is not a valid action", ErrValidation)
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// Validate checks the action and fills defaults.
func (a *ShellAction) Validate() error {
	needSession := func() error {
		if !shellSessionRe.MatchString(a.Session) {
			return fmt.Errorf("%w: shell session must be 1-40 letters, digits, - or _", ErrValidation)
		}
		return nil
	}
	switch a.Op {
	case ShellOpRun:
		if strings.TrimSpace(a.Command) == "" {
			return fmt.Errorf("%w: shell_run needs a command", ErrValidation)
		}
		if a.TimeoutS <= 0 {
			a.TimeoutS = ShellDefaultTimeoutS
		}
		if a.TimeoutS > ShellMaxTimeoutS {
			a.TimeoutS = ShellMaxTimeoutS
		}
		return nil
	case ShellOpTerminalOpen:
		return needSession()
	case ShellOpTerminalSend:
		if err := needSession(); err != nil {
			return err
		}
		if a.Text == "" {
			return fmt.Errorf("%w: terminal_send needs text", ErrValidation)
		}
		return nil
	case ShellOpTerminalRead:
		if err := needSession(); err != nil {
			return err
		}
		if a.Lines <= 0 {
			a.Lines = 50
		}
		if a.Lines > ShellMaxReadLines {
			a.Lines = ShellMaxReadLines
		}
		return nil
	case ShellOpAppOpen:
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("%w: app_open needs an app name", ErrValidation)
		}
		return nil
	case ShellOpAppList:
		return nil
	default:
		return fmt.Errorf("%w: unknown shell op %q", ErrValidation, a.Op)
	}
}

// ShellPayload builds the task payload for an action, with the capability it requires.
func ShellPayload(a ShellAction) json.RawMessage {
	b, _ := json.Marshal(map[string]any{RequiresKey: []string{a.Op.RequiredCapability()}, ShellKey: a})
	return b
}
