package domain

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ArchiveAfter is how long a finished minor task stays in the task list before it is archived.
const ArchiveAfter = 72 * time.Hour

// ListingsSyncTitle is the daily routine that refreshes the verified internship listings, in code.
const ListingsSyncTitle = "LISTINGS SYNC"

// junkMaxChars is how short a finished answer must be to count as junk.
const junkMaxChars = 200

var (
	junkNothingFound = regexp.MustCompile(`(?i)\b(couldn'?t|could not|can'?t|cannot|unable to|did not|didn'?t|wasn'?t able to|was not able to)\s+(find|locate|retrieve|access|get)\b|\bno\s+(results?|listings?|matches|openings?|data)\b[^.]*\b(found|available)\b|\bnothing\s+(was\s+)?found\b|\bwaiting\b|\b(background\s+(process|task|job)|in the background)\b`)
	// A markdown table has a separator row of dashes between pipes.
	tableSeparator = regexp.MustCompile(`(?m)^\s*\|?\s*:?-{3,}:?\s*\|`)
)

// trackerCardWords are title words that make a task a tracker card on the web Trackers page.
var trackerCardWords = []string{"tracker", "internship", "hackathon", "open-source", "hygiene", "dsa coach", "brief", "scorecard", "audit", "thread alerts", "cold email"}

func isApplyPayload(input json.RawMessage) bool {
	var p struct {
		ApplyFor  json.RawMessage `json:"apply_for"`
		SubmitFor json.RawMessage `json:"submit_for"`
	}
	if json.Unmarshal(input, &p) != nil {
		return false
	}
	set := func(r json.RawMessage) bool { return len(r) > 0 && string(r) != "null" }
	return set(p.ApplyFor) || set(p.SubmitFor)
}

// IsRoutine reports whether a task is a routine or tracker run: it carries a cadence, or its title is a tracker or
// one of the routines that run in code. Routines are never archived by age.
func IsRoutine(t *Task) bool {
	var p struct {
		Cadence string `json:"cadence"`
	}
	if json.Unmarshal(t.InputPayload, &p) == nil && p.Cadence != "" {
		return true
	}
	upper := strings.ToUpper(t.Title)
	return strings.Contains(upper, "TRACKER") || IsAlumniDMBatch(t.Title) || strings.Contains(upper, ListingsSyncTitle)
}

// isTrackerCard mirrors the web Trackers page: the tasks it builds cards from.
func isTrackerCard(t *Task) bool {
	if isApplyPayload(t.InputPayload) {
		return false
	}
	if TrackerTypeOf(t.Title, t.Goal, t.InputPayload) != "" {
		return true
	}
	title := strings.ToLower(t.Title)
	for _, w := range trackerCardWords {
		if strings.Contains(title, w) {
			return true
		}
	}
	return false
}

func finishedAt(t *Task) time.Time {
	switch {
	case t.CompletedAt != nil:
		return *t.CompletedAt
	case !t.UpdatedAt.IsZero():
		return t.UpdatedAt
	}
	return t.CreatedAt
}

func hasOutput(t *Task) bool {
	s := strings.TrimSpace(string(t.OutputPayload))
	return s != "" && s != "{}" && s != "null"
}

// IsJunkResult reports whether a finished task produced nothing worth keeping: it failed with "returned no result",
// or it completed with a short answer, no table, that says it found nothing, is waiting, or launched a background
// process.
func IsJunkResult(t *Task) bool {
	switch t.Status {
	case TaskStatusFailed:
		return t.FailureReason != nil && strings.Contains(strings.ToLower(*t.FailureReason), "returned no result")
	case TaskStatusCompleted:
		text := ResultText(t.OutputPayload)
		if text == "" {
			return false
		}
		return len([]rune(text)) < junkMaxChars && !tableSeparator.MatchString(text) && junkNothingFound.MatchString(text)
	}
	return false
}

// AutoArchiveIDs decides which tasks to archive. tasks are the user's top-level, unarchived tasks (follow-ups
// included). A task is archived when it is finished and either its output is junk or it, and everything that
// continues it, finished at least ArchiveAfter ago. Never archived: a task still working, a routine or tracker
// run, a completed application (it is what marks a listing applied), and the runs the Trackers page builds its
// cards from (the latest of each tracker, and the latest that has an answer), with the root of the thread they
// belong to. Subtasks and follow-ups are archived with their root,
// so only roots are returned.
func AutoArchiveIDs(tasks []*Task, now time.Time) []uuid.UUID {
	byID := make(map[uuid.UUID]*Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	threads := map[uuid.UUID][]*Task{} // root id -> follow-ups
	rootOf := func(t *Task) *Task {
		if !IsFollowUp(t) {
			return nil
		}
		id, _ := ThreadOf(t)
		return byID[id]
	}
	for _, t := range tasks {
		if IsFollowUp(t) {
			id, _ := ThreadOf(t)
			threads[id] = append(threads[id], t)
		}
	}

	// The Trackers page shows one card per tracker: its latest run that has an answer, else its latest run.
	type best struct{ latest, withOutput *Task }
	cards := map[string]*best{}
	for _, t := range tasks {
		owner := t
		if r := rootOf(t); r != nil {
			owner = r
		}
		if isApplyPayload(t.InputPayload) || !isTrackerCard(owner) {
			continue
		}
		key := "title:" + strings.ToLower(strings.TrimSpace(owner.Title))
		if ty := TrackerTypeOf(owner.Title, owner.Goal, owner.InputPayload); ty != "" {
			key = "type:" + ty
		}
		b := cards[key]
		if b == nil {
			b = &best{}
			cards[key] = b
		}
		if b.latest == nil || !finishedAt(t).Before(finishedAt(b.latest)) {
			b.latest = t
		}
		if hasOutput(t) && (b.withOutput == nil || !finishedAt(t).Before(finishedAt(b.withOutput))) {
			b.withOutput = t
		}
	}
	protected := map[uuid.UUID]bool{}
	for _, b := range cards {
		for _, t := range []*Task{b.latest, b.withOutput} {
			if t == nil {
				continue
			}
			protected[t.ID] = true
			if r := rootOf(t); r != nil {
				protected[r.ID] = true
			}
		}
	}

	var ids []uuid.UUID
	for _, t := range tasks {
		if t.ParentID != nil || IsFollowUp(t) || !t.IsTerminal() || IsRoutine(t) || protected[t.ID] {
			continue
		}
		// The Trackers page marks a listing applied from its finished application task, so that task stays.
		if isApplyPayload(t.InputPayload) && t.Status == TaskStatusCompleted {
			continue
		}
		last, settled := finishedAt(t), true
		for _, f := range threads[t.ID] {
			if !f.IsTerminal() {
				settled = false
			}
			if at := finishedAt(f); at.After(last) {
				last = at
			}
		}
		if !settled {
			continue
		}
		if (len(threads[t.ID]) == 0 && IsJunkResult(t)) || now.Sub(last) >= ArchiveAfter {
			ids = append(ids, t.ID)
		}
	}
	return ids
}
