package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/griffin/core/internal/handover"
	"github.com/griffin/core/internal/hostagent"
)

// Low-battery handover (environment). Off unless HOST_HANDOVER is set, because it saves and pushes work.
//
//	HOST_HANDOVER       "1" to turn it on
//	HANDOVER_THRESHOLDS battery percentages for the save, push and final stages (default "15,5,2")
//	HANDOVER_VSCODE     "false" to ignore VS Code's open folders (default true)
//	HANDOVER_REPOS      extra project folders to protect, separated by ":"
//	HANDOVER_REMOTE     git remote to push to (default: the project's first remote)

type handoverSetup struct {
	thresholds handover.Thresholds
	battery    handover.Battery
	responder  *handover.Responder
}

func handoverEnabled() bool {
	v := strings.ToLower(os.Getenv("HOST_HANDOVER"))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func newHandover(logger *slog.Logger, agent handover.TaskPauser) (*handoverSetup, error) {
	th, ok := handover.ParseThresholds(os.Getenv("HANDOVER_THRESHOLDS"))
	if !ok {
		return nil, fmt.Errorf("HANDOVER_THRESHOLDS must be three falling percentages such as 15,5,2")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	vscode := strings.ToLower(envOr("HANDOVER_VSCODE", "true")) != "false"
	var extra []string
	for _, p := range strings.Split(os.Getenv("HANDOVER_REPOS"), ":") {
		if p = strings.TrimSpace(p); p != "" {
			extra = append(extra, filepath.Clean(p))
		}
	}
	repos := func() []string {
		seen := map[string]bool{}
		var out []string
		for _, p := range append(extra, openFoldersIf(vscode, home)...) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		return out
	}
	return &handoverSetup{
		thresholds: th,
		battery:    handover.SysfsBattery{},
		responder:  &handover.Responder{Host: host, Repos: repos, Agent: agent, Remote: os.Getenv("HANDOVER_REMOTE"), Logger: logger},
	}, nil
}

func openFoldersIf(on bool, home string) []string {
	if !on || home == "" {
		return nil
	}
	return handover.OpenFolders(home)
}

// start runs the monitor until ctx ends. A machine with no battery does nothing.
func (h *handoverSetup) start(ctx context.Context, logger *slog.Logger) {
	if _, ok := h.battery.Read(); !ok {
		logger.Info("low-battery handover is on, but this machine has no battery, so it will not trigger")
		return
	}
	logger.Info("low-battery handover is on", "save_at", h.thresholds.Save, "push_at", h.thresholds.Push, "final_at", h.thresholds.Final)
	m := &handover.Monitor{Battery: h.battery, Thresholds: h.thresholds,
		OnLevel:   func(c context.Context, l handover.Level, r handover.Reading) { h.responder.Handle(c, l, r) },
		OnRecover: h.responder.Recover}
	go m.Run(ctx)
}

// handoverCheck reports what would be protected, and changes nothing.
func handoverCheck(_ context.Context, logger *slog.Logger) error {
	h, err := newHandover(logger, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Stages: save at %d%%, push and hand tasks back at %d%%, final at %d%% (while discharging)\n", h.thresholds.Save, h.thresholds.Push, h.thresholds.Final)
	if r, ok := h.battery.Read(); ok {
		state := "on charge"
		if r.Discharging {
			state = "discharging"
		}
		fmt.Printf("Battery: %d%% (%s). Right now that is stage: %s\n", r.Percent, state, h.thresholds.LevelFor(r.Percent, r.Discharging))
	} else {
		fmt.Println("Battery: none found, so the handover will never trigger on this machine.")
	}
	repos := h.responder.Repos()
	if len(repos) == 0 {
		fmt.Println("Projects: none. Open a folder in VS Code, or set HANDOVER_REPOS=/path/one:/path/two.")
		return nil
	}
	fmt.Println("Projects that would be saved:")
	for _, r := range repos {
		fmt.Println("  ", r)
	}
	fmt.Println("Turn it on with HOST_HANDOVER=1. Try a safe local-only save now with: griffin-host handover-test")
	if !handoverEnabled() {
		fmt.Println("(HOST_HANDOVER is not set, so nothing happens automatically yet.)")
	}
	return nil
}

// handoverTest runs the first stage once: every project is saved to a new local branch and nothing is
// pushed or paused. It shows exactly what a real low-battery save would do.
func handoverTest(ctx context.Context, logger *slog.Logger) error {
	h, err := newHandover(logger, nil)
	if err != nil {
		return err
	}
	fmt.Println("Saving each project to a new local branch (nothing is pushed, nothing is paused).")
	results := h.responder.SaveAll(ctx, handover.LevelSave, handover.Reading{Percent: 100}, nil)
	if len(results) == 0 {
		fmt.Println("No projects found. Open a folder in VS Code, or set HANDOVER_REPOS.")
	}
	for _, r := range results {
		switch {
		case r == nil:
		case r.Branch == "": // the snapshot itself failed; the reason is in Notes
			fmt.Printf("%s: could not be saved: %s\n", r.Repo, strings.Join(r.Notes, "; "))
		case r.Clean:
			fmt.Printf("%s: nothing to save\n", r.Repo)
		default:
			fmt.Printf("%s: %d files saved on %s", r.Repo, r.Files, r.Branch)
			if len(r.Skipped) > 0 {
				fmt.Printf("; kept back for safety: %s", strings.Join(r.Skipped, ", "))
			}
			fmt.Printf("\n   look:   git -C %q log --oneline -2 %s\n   remove: git -C %q branch -D %s\n", r.Repo, r.Branch, r.Repo, r.Branch)
		}
	}
	return nil
}

var _ handover.TaskPauser = (*hostagent.Agent)(nil)
