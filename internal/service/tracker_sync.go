package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// The routines whose trackers are written from the sites' own data, in code. A task is one of them when its title
// starts with the name ("HACKATHON TRACKER (daily 08:30)"); a task that only mentions the words is not.
const (
	HackathonTrackerTitle = "HACKATHON TRACKER"
	OpenSourceWatchTitle  = "OPEN-SOURCE WATCH"
	RepoHygieneTitle      = "REPO HYGIENE"
)

func titleStarts(title, name string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(title)), name)
}

// IsHackathonTracker reports whether a task is the hackathon routine.
func IsHackathonTracker(title string) bool { return titleStarts(title, HackathonTrackerTitle) }

// IsOpenSourceWatch reports whether a task is the open-source routine.
func IsOpenSourceWatch(title string) bool { return titleStarts(title, OpenSourceWatchTitle) }

// IsRepoHygiene reports whether a task is the repo hygiene routine.
func IsRepoHygiene(title string) bool { return titleStarts(title, RepoHygieneTitle) }

// replaceTrackerList replaces a tracker's shared list with text, creating the list when there is none.
func (w *TrackerLists) replaceTrackerList(ctx context.Context, userID uuid.UUID, tracker, title, text string) error {
	if list := w.list(ctx, userID, storage.ListKind, tracker); list != nil {
		_, err := w.Store.UpdateText(ctx, list.ID, userID, text, false, trackerReadLimit)
		return err
	}
	_, err := w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: userID, Title: title, Filename: tracker + "-tracker.md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body:     bytes.NewReader([]byte(text)),
		Metadata: domain.TrackerListMetadata(storage.ListKind, tracker),
	})
	return err
}

func problemsLine(problems []string) string {
	if len(problems) == 0 {
		return ""
	}
	return "\n\nCould not read: " + strings.Join(problems, "; ") + "."
}

// HackathonSync keeps the Hackathons tracker true to Devpost, Unstop and Devfolio. No model is involved.
type HackathonSync struct {
	// Get and Post fetch pages; nil means the network.
	Get  sources.Get
	Post sources.Post
	Now  func() time.Time
}

// HackathonSyncResult is what one sync read.
type HackathonSyncResult struct {
	Rows     []sources.Hackathon
	Problems []string
	Now      time.Time
}

// Run reads the sites and keeps the hackathons that fit the owner.
func (s *HackathonSync) Run(ctx context.Context) HackathonSyncResult {
	now := time.Now
	if s != nil && s.Now != nil {
		now = s.Now
	}
	res := HackathonSyncResult{Now: now()}
	var get sources.Get
	var post sources.Post
	if s != nil {
		get, post = s.Get, s.Post
	}
	all, problems := sources.ReadHackathons(ctx, get, post, res.Now)
	res.Rows, res.Problems = sources.FilterHackathons(all, res.Now), problems
	return res
}

// Summary is the owner-facing line for a sync.
func (r HackathonSyncResult) Summary() string {
	if len(r.Rows) == 0 {
		return "No open hackathon fits right now: online or in India, on AI, ML, agents, systems or devtools, with 3 or more days left." + problemsLine(r.Problems)
	}
	by := map[string]int{}
	for _, h := range r.Rows {
		by[h.Source]++
	}
	var from []string
	for _, site := range []string{sources.Devpost, sources.UnstopH, sources.Devfolio} {
		if by[site] > 0 {
			from = append(from, fmt.Sprintf("%s %d", site, by[site]))
		}
	}
	var soon []string
	for _, h := range r.Rows[:min(3, len(r.Rows))] {
		soon = append(soon, fmt.Sprintf("%s (%s, %d days left)", h.Name, h.Deadline.Format("2 Jan"), h.DaysLeft(r.Now)))
	}
	return fmt.Sprintf("%d open hackathons fit you (%s). Closing soonest: %s.", len(r.Rows), strings.Join(from, ", "), strings.Join(soon, "; ")) + problemsLine(r.Problems)
}

// SyncHackathons runs the hackathon routine in code. handled is false for any other task.
func (w *TrackerLists) SyncHackathons(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Store == nil || w.Hackathons == nil || task == nil || !IsHackathonTracker(task.Title) {
		return "", false
	}
	res := w.Hackathons.Run(ctx)
	if len(res.Rows) == 0 && len(res.Problems) > 0 {
		// Nothing was read and something failed: keep the list as it was rather than empty it.
		return "The hackathon sites did not answer this run, so the Hackathons list is unchanged." + problemsLine(res.Problems), true
	}
	if err := w.replaceTrackerList(ctx, task.UserID, "hackathons", "Hackathons tracker", sources.HackathonsListText(res.Rows, res.Now)); err != nil {
		return res.Summary() + "\n\nCould not refresh the Hackathons list: " + err.Error(), true
	}
	return res.Summary(), true
}

// OpenSourceSync keeps the Open Source tracker true to the open issues of the owner's chosen projects.
type OpenSourceSync struct {
	Get sources.Get
	Now func() time.Time
}

// OpenSourceSyncResult is what one sync read.
type OpenSourceSyncResult struct {
	Issues   []sources.Issue
	Problems []string
}

// Run searches GitHub for open issues a newcomer can take.
func (s *OpenSourceSync) Run(ctx context.Context) OpenSourceSyncResult {
	now := time.Now
	var get sources.Get
	if s != nil {
		get = s.Get
		if s.Now != nil {
			now = s.Now
		}
	}
	issues, problems := sources.ReadIssues(ctx, get, now())
	return OpenSourceSyncResult{Issues: issues, Problems: problems}
}

// Summary is the owner-facing line for a sync.
func (r OpenSourceSyncResult) Summary() string {
	if len(r.Issues) == 0 {
		return "No open unassigned good-first-issue or help-wanted issue opened in the last year in the watched projects right now." + problemsLine(r.Problems)
	}
	repos := map[string]bool{}
	for _, is := range r.Issues {
		repos[is.Repo] = true
	}
	var top []string
	for _, is := range r.Issues[:min(3, len(r.Issues))] {
		top = append(top, fmt.Sprintf("%s #%d %s", is.Repo, is.Number, is.Title))
	}
	return fmt.Sprintf("%s across %s. Newest: %s.", countOf(len(r.Issues), "open unassigned issue"), countOf(len(repos), "project"), strings.Join(top, "; ")) + problemsLine(r.Problems)
}

// countOf writes "1 project" or "3 projects".
func countOf(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// SyncOpenSource runs the open-source routine in code. handled is false for any other task.
func (w *TrackerLists) SyncOpenSource(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Store == nil || w.OpenSource == nil || task == nil || !IsOpenSourceWatch(task.Title) {
		return "", false
	}
	res := w.OpenSource.Run(ctx)
	if len(res.Issues) == 0 && len(res.Problems) > 0 {
		return "GitHub did not answer this run, so the Open Source list is unchanged." + problemsLine(res.Problems), true
	}
	if err := w.replaceTrackerList(ctx, task.UserID, "opensource", "Open Source tracker", sources.IssuesListText(res.Issues)); err != nil {
		return res.Summary() + "\n\nCould not refresh the Open Source list: " + err.Error(), true
	}
	return res.Summary(), true
}

// HygieneSync keeps the Repo Hygiene tracker true to the owner's public GitHub repositories.
type HygieneSync struct {
	// Login is the owner's GitHub username (the GITHUB_LOGIN setting); empty leaves the routine off.
	Login string
	Get   sources.Get
	Now   func() time.Time
}

// HygieneSyncResult is what one sync read.
type HygieneSyncResult struct {
	Repos    []sources.Repo
	Problems []string
	Now      time.Time
}

// Run lists the repositories and checks each one.
func (s *HygieneSync) Run(ctx context.Context) HygieneSyncResult {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	res := HygieneSyncResult{Now: now()}
	res.Repos, res.Problems = sources.ReadRepos(ctx, s.Get, s.Login)
	return res
}

func (r HygieneSyncResult) checked() (n int) {
	for _, repo := range r.Repos {
		if repo.Checked {
			n++
		}
	}
	return n
}

// Summary is the owner-facing line for a sync.
func (r HygieneSyncResult) Summary() string {
	n := r.checked()
	if n == 0 {
		return "No public repository was checked." + problemsLine(r.Problems)
	}
	clean, needs := 0, []sources.Repo{}
	for _, repo := range r.Repos {
		switch {
		case !repo.Checked:
		case repo.FixNext(r.Now) == "Nothing: all checks pass":
			clean++
		default:
			needs = append(needs, repo)
		}
	}
	out := fmt.Sprintf("Checked %d public repos: %d pass every check, %d need a fix.", n, clean, len(needs))
	var top []string
	for _, repo := range needs[:min(3, len(needs))] {
		top = append(top, repo.Name+": "+repo.FixNext(r.Now))
	}
	if len(top) > 0 {
		out += " First: " + strings.Join(top, "; ") + "."
	}
	if n < len(r.Repos) && len(r.Problems) == 0 {
		out += fmt.Sprintf(" %d more repos wait for the next run (GitHub allows 60 requests an hour).", len(r.Repos)-n)
	}
	return out + problemsLine(r.Problems)
}

// SyncHygiene runs the repo hygiene routine in code. handled is false for any other task.
func (w *TrackerLists) SyncHygiene(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Store == nil || task == nil || !IsRepoHygiene(task.Title) {
		return "", false
	}
	if w.Hygiene == nil || strings.TrimSpace(w.Hygiene.Login) == "" {
		return "Repo hygiene needs GITHUB_LOGIN (your GitHub username) set on the server before it can check your repos. The Repo Hygiene list is unchanged.", true
	}
	res := w.Hygiene.Run(ctx)
	if res.checked() == 0 && len(res.Problems) > 0 {
		return "GitHub did not answer this run, so the Repo Hygiene list is unchanged." + problemsLine(res.Problems), true
	}
	if err := w.replaceTrackerList(ctx, task.UserID, "hygiene", "Repo Hygiene tracker", sources.HygieneListText(res.Repos, res.Now)); err != nil {
		return res.Summary() + "\n\nCould not refresh the Repo Hygiene list: " + err.Error(), true
	}
	return res.Summary(), true
}
