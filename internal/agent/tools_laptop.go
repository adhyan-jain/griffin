package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// LaptopTasks is the part of the task engine the laptop tools use (service.TaskEngine satisfies it).
type LaptopTasks interface {
	CreateTask(ctx context.Context, task *domain.Task, dependencyIDs []uuid.UUID) (*domain.Task, error)
	GetTaskForUser(ctx context.Context, taskID uuid.UUID, userID uuid.UUID) (*domain.Task, error)
}

// LaptopDeps are what the laptop tools need. Covers says whether one of the owner's live hosts has all the
// capabilities (the same presence check the host service uses); without it the tools are left out.
type LaptopDeps struct {
	Tasks  LaptopTasks
	Covers func(ctx context.Context, user uuid.UUID, required []string) bool
	// Wait is how long a call waits for the laptop's answer (default 15s); Poll is the check interval (default 500ms).
	Wait, Poll time.Duration
}

type noLaptopWaitKey struct{}

// withoutLaptopWait marks a context so a laptop tool only queues its task: used when an approved action runs
// from the approval request, which must not hang on the laptop. The owner is told when the task finishes.
func withoutLaptopWait(ctx context.Context) context.Context {
	return context.WithValue(ctx, noLaptopWaitKey{}, true)
}

// NewLaptopTools builds laptop_shell, laptop_terminal and laptop_app. They are external tools: the owner's own
// chat runs them at once, a scheduled run only holds an approval that shows the exact command, and laptop_shell
// and laptop_terminal are also held when untrusted text was read earlier in the same turn.
func NewLaptopTools(d LaptopDeps) []*Tool {
	if d.Tasks == nil || d.Covers == nil {
		return nil
	}
	return []*Tool{laptopShell(d), laptopTerminal(d), laptopApp(d)}
}

type laptopShellArgs struct {
	Command  string `json:"command"`
	Cwd      string `json:"cwd,omitempty"`
	TimeoutS int    `json:"timeout_s,omitempty"`
}

func laptopShell(d LaptopDeps) *Tool {
	return &Tool{
		Name: "laptop_shell",
		Description: "Run a shell command on the owner's laptop (bash, as the owner) and report its output and exit code. Use it when the owner says to run something on their laptop. " +
			"cwd is an absolute path or starts with ~. The laptop must be online. For a visible terminal window use laptop_terminal instead.",
		Effect:             EffectExternal,
		HoldAfterUntrusted: true,
		Schema: obj(map[string]any{"command": str("the exact command line to run"), "cwd": str("working directory, absolute or starting with ~"),
			"timeout_s": num("seconds before it is stopped, at most 300 (default 60)")}, "command"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a laptopShellArgs
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			act := domain.ShellAction{Op: domain.ShellOpRun, Command: a.Command, Cwd: strings.TrimSpace(a.Cwd), TimeoutS: a.TimeoutS}
			if err := act.Validate(); err != nil {
				return nil, "", err
			}
			b, _ := json.Marshal(act)
			summary := "Run on your laptop: " + truncate(act.Command, 1000)
			if act.Cwd != "" {
				summary += " (in " + truncate(act.Cwd, 200) + ")"
			}
			return b, summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			act, err := laptopAction(payload)
			if err != nil {
				return "", err
			}
			return runOnLaptop(ctx, d, user, "Laptop: run "+truncate(act.Command, 60), act)
		},
	}
}

type laptopTerminalArgs struct {
	Session string `json:"session"`
	Action  string `json:"action"`
	Text    string `json:"text,omitempty"`
	Command string `json:"command,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Enter   *bool  `json:"enter,omitempty"`
	Lines   int    `json:"lines,omitempty"`
}

func laptopTerminal(d LaptopDeps) *Tool {
	return &Tool{
		Name: "laptop_terminal",
		Description: "Drive a visible terminal window on the owner's laptop (a named tmux session shown in their terminal app). action open: create the session if needed, open a window attached to it, " +
			"optionally in cwd and optionally typing 'command' first (for example open a terminal, cd to a folder, run claude). action send: type 'text' into the session and press Enter (enter=false to skip Enter), " +
			"for example to type /rc into a running claude. action read: return the last lines of the screen so you can report what it shows. 'session' is a short name of letters, digits, - or _.",
		Effect:             EffectExternal,
		HoldAfterUntrusted: true,
		Schema: obj(map[string]any{"session": str("short terminal name"), "action": str("open, send or read"), "text": str("text to type; for send"),
			"command": str("first command to type; for open"), "cwd": str("working directory; for open, absolute or starting with ~"),
			"enter": flag("press Enter after the text (default true); for send"), "lines": num("how many lines to read, at most 200 (default 50); for read")}, "session", "action"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a laptopTerminalArgs
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			act := domain.ShellAction{Session: strings.TrimSpace(a.Session)}
			var summary string
			switch strings.ToLower(strings.TrimSpace(a.Action)) {
			case "open":
				act.Op, act.Command, act.Cwd = domain.ShellOpTerminalOpen, strings.TrimSpace(a.Command), strings.TrimSpace(a.Cwd)
				summary = "Open terminal " + act.Session + " on your laptop"
				if act.Cwd != "" {
					summary += " in " + truncate(act.Cwd, 200)
				}
				if act.Command != "" {
					summary += " and type: " + truncate(act.Command, 1000)
				}
			case "send":
				act.Op, act.Text = domain.ShellOpTerminalSend, a.Text
				act.NoEnter = a.Enter != nil && !*a.Enter
				summary = "Type into terminal " + act.Session + " on your laptop: " + truncate(act.Text, 1000)
				if act.NoEnter {
					summary += " (without Enter)"
				}
			case "read":
				act.Op, act.Lines = domain.ShellOpTerminalRead, a.Lines
				summary = "Read terminal " + act.Session + " on your laptop"
			default:
				return nil, "", errors.New("action must be open, send or read")
			}
			if err := act.Validate(); err != nil {
				return nil, "", err
			}
			b, _ := json.Marshal(act)
			return b, summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			act, err := laptopAction(payload)
			if err != nil {
				return "", err
			}
			return runOnLaptop(ctx, d, user, fmt.Sprintf("Laptop terminal %s: %s", act.Session, strings.TrimPrefix(string(act.Op), "terminal_")), act)
		},
	}
}

type laptopAppArgs struct {
	Action string `json:"action"`
	Name   string `json:"name,omitempty"`
}

func laptopApp(d LaptopDeps) *Tool {
	return &Tool{
		Name: "laptop_app",
		Description: "Open a desktop app on the owner's laptop by name (action open), or search the installed apps (action list; name is the search text, empty lists some). " +
			"Banking and payment apps are refused. To drive something inside an app, use laptop_terminal for terminal programs.",
		Effect: EffectExternal,
		Schema: obj(map[string]any{"action": str("open or list"), "name": str("app name, or search text for list")}, "action"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a laptopAppArgs
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			act := domain.ShellAction{Name: strings.TrimSpace(a.Name)}
			var summary string
			switch strings.ToLower(strings.TrimSpace(a.Action)) {
			case "open":
				act.Op = domain.ShellOpAppOpen
				summary = "Open " + truncate(act.Name, 80) + " on your laptop"
			case "list":
				act.Op = domain.ShellOpAppList
				summary = "List apps on your laptop matching " + truncate(act.Name, 80)
			default:
				return nil, "", errors.New("action must be open or list")
			}
			if err := act.Validate(); err != nil {
				return nil, "", err
			}
			b, _ := json.Marshal(act)
			return b, summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			act, err := laptopAction(payload)
			if err != nil {
				return "", err
			}
			return runOnLaptop(ctx, d, user, "Laptop app: "+string(act.Op)+" "+truncate(act.Name, 40), act)
		},
	}
}

func laptopAction(payload json.RawMessage) (*domain.ShellAction, error) {
	var act domain.ShellAction
	if err := json.Unmarshal(payload, &act); err != nil {
		return nil, err
	}
	if err := act.Validate(); err != nil {
		return nil, err
	}
	return &act, nil
}

// runOnLaptop queues the task for a live laptop and waits a short while for the answer, so the reply can carry
// the output. When it is not done in time the task keeps running and the owner is told when it finishes: the task
// is a top-level task, which the finish notifier reports.
func runOnLaptop(ctx context.Context, d LaptopDeps, user uuid.UUID, title string, act *domain.ShellAction) (string, error) {
	need := act.Op.RequiredCapability()
	if !d.Covers(ctx, user, []string{need}) {
		return "", fmt.Errorf("your laptop is offline or has not been given the %q permission, so nothing was run. Check that griffin-host is running and that %s is allowed for it in Devices", need, need)
	}
	task, err := d.Tasks.CreateTask(ctx, &domain.Task{UserID: user, ManagerDomain: "personal_ops", Title: title, Goal: title,
		Priority: domain.TaskPriorityHighP1, InputPayload: domain.ShellPayload(*act)}, nil)
	if err != nil {
		return "", err
	}
	wait := d.Wait
	if wait <= 0 {
		wait = 15 * time.Second
	}
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - 2*time.Second; left < wait {
			wait = left
		}
	}
	if ctx.Value(noLaptopWaitKey{}) != nil {
		wait = 0
	}
	poll := d.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	for deadline := time.Now().Add(wait); ; {
		t, err := d.Tasks.GetTaskForUser(ctx, task.ID, user)
		if err == nil && t != nil {
			switch t.Status {
			case domain.TaskStatusCompleted:
				if txt := strings.TrimSpace(domain.OutputText(t.OutputPayload)); txt != "" {
					return txt, nil
				}
				return "Done on your laptop (no output).", nil
			case domain.TaskStatusFailed, domain.TaskStatusCancelled:
				reason := "no reason given"
				if t.FailureReason != nil && strings.TrimSpace(*t.FailureReason) != "" {
					reason = *t.FailureReason
				}
				return "", fmt.Errorf("the laptop could not do it: %s", reason)
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(poll):
		}
		if ctx.Err() != nil {
			break
		}
	}
	return fmt.Sprintf("Running on your laptop, I'll tell you when it's done (task %s).", task.ID), nil
}
