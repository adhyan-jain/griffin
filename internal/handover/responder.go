package handover

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/griffin/core/internal/hostagent"
)

// TaskPauser is the part of the host agent the responder needs.
type TaskPauser interface {
	Drain(reason string) int
	Resume()
	RunningTasks() []hostagent.RunningTask
}

// Responder does the work for each level. It is safe to call with the same level more than once.
type Responder struct {
	Host string
	// Repos returns the folders to protect each time it is asked (VS Code's open folders change).
	Repos  func() []string
	Agent  TaskPauser // may be nil
	Remote string
	Logger *slog.Logger
	Now    func() time.Time

	mu      sync.Mutex
	branch  string
	drained bool
}

func (r *Responder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Responder) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// sessionBranch is one branch name for the whole low-battery episode, so every snapshot continues the same
// branch and pushes fast-forward.
func (r *Responder) sessionBranch() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.branch == "" {
		r.branch = "griffin/handover-" + r.now().Format("20060102-1504")
	}
	return r.branch
}

// Handle is a Monitor's OnLevel.
func (r *Responder) Handle(ctx context.Context, level Level, rd Reading) []*SnapshotResult {
	r.log().Warn("laptop battery is low; protecting open work", "battery", rd.Percent, "stage", level.String())
	var tasks []string
	if level >= LevelPush && r.Agent != nil {
		for _, t := range r.Agent.RunningTasks() {
			tasks = append(tasks, t.Title)
		}
		sort.Strings(tasks)
		r.mu.Lock()
		first := !r.drained
		r.drained = true
		r.mu.Unlock()
		if first || level == LevelFinal {
			n := r.Agent.Drain(fmt.Sprintf("the laptop's battery is at %d%%", rd.Percent))
			r.log().Warn("running tasks handed back to the cloud", "count", n)
		}
	}
	return r.SaveAll(ctx, level, rd, tasks)
}

// Recover is a Monitor's OnRecover: charging again, so work may resume.
func (r *Responder) Recover(_ context.Context, rd Reading) {
	r.mu.Lock()
	r.drained = false
	r.branch = "" // the next low-battery episode starts a new branch
	r.mu.Unlock()
	if r.Agent != nil {
		r.Agent.Resume()
	}
	r.log().Info("battery recovered; resuming normal work", "battery", rd.Percent)
}

// SaveAll snapshots every repo, in parallel because time is short. The save stage stays local; later
// stages push.
func (r *Responder) SaveAll(ctx context.Context, level Level, rd Reading, tasks []string) []*SnapshotResult {
	var repos []string
	if r.Repos != nil {
		repos = r.Repos()
	}
	branch := r.sessionBranch()
	results := make([]*SnapshotResult, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := Snapshot(ctx, repo, SnapshotOptions{
				Branch: branch,
				Push:   level >= LevelPush,
				Remote: r.Remote,
				Doc: func(f SnapshotFacts) string {
					return BuildDoc(DocContext{Host: r.Host, Battery: rd.Percent, Level: level, At: r.now(), Tasks: tasks}, f)
				},
			})
			if err != nil {
				r.log().Error("could not save this project", "repo", repo, "error", err)
				results[i] = &SnapshotResult{Repo: repo, Notes: []string{err.Error()}}
				return
			}
			results[i] = res
			switch {
			case res.Clean:
				r.log().Info("nothing to save", "repo", res.Repo)
			case res.PushErr != "":
				r.log().Error("saved locally but could not push", "repo", res.Repo, "branch", res.Branch, "error", res.PushErr)
			default:
				r.log().Info("work saved", "repo", res.Repo, "branch", res.Branch, "files", res.Files, "pushed", res.Pushed, "skipped", len(res.Skipped))
			}
		}()
	}
	wg.Wait()
	return results
}
