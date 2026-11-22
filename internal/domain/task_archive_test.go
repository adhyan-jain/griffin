package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

var archiveNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func finished(title string, status domain.TaskStatus, ago time.Duration, output string) *domain.Task {
	at := archiveNow.Add(-ago)
	t := &domain.Task{
		ID: uuid.New(), Title: title, Goal: "do it", Status: status, InputPayload: json.RawMessage(`{}`),
		CompletedAt: &at, CreatedAt: at.Add(-time.Minute), UpdatedAt: at,
	}
	if output != "" {
		b, _ := json.Marshal(map[string]string{"response": output})
		t.OutputPayload = b
	}
	return t
}

func archived(tasks ...*domain.Task) map[uuid.UUID]bool {
	m := map[uuid.UUID]bool{}
	for _, id := range domain.AutoArchiveIDs(tasks, archiveNow) {
		m[id] = true
	}
	return m
}

func inThread(t *domain.Task, root *domain.Task) {
	t.InputPayload, _ = json.Marshal(map[string]string{"thread_id": root.ID.String(), "follow_up_of": root.ID.String()})
}

func TestAutoArchiveByAge(t *testing.T) {
	long := strings.Repeat("A real answer with substance. ", 20)
	old := finished("Summarise the paper", domain.TaskStatusCompleted, 4*day, long)
	recent := finished("Summarise the other paper", domain.TaskStatusCompleted, 2*day, long)
	oldFailed := finished("Do a thing", domain.TaskStatusFailed, 5*day, "")
	oldCancelled := finished("Do another thing", domain.TaskStatusCancelled, 5*day, "")
	running := finished("Still going", domain.TaskStatusRunning, 9*day, "")
	got := archived(old, recent, oldFailed, oldCancelled, running)
	if !got[old.ID] || !got[oldFailed.ID] || !got[oldCancelled.ID] {
		t.Fatalf("old finished tasks must be archived: %v", got)
	}
	if got[recent.ID] || got[running.ID] {
		t.Fatalf("a task under 3 days old or still working must stay: %v", got)
	}
}

func TestAutoArchiveSkipsRoutinesAndSubtasks(t *testing.T) {
	long := strings.Repeat("Substance. ", 30)
	cadence := finished("Weekly review", domain.TaskStatusCompleted, 10*day, long)
	cadence.InputPayload = json.RawMessage(`{"cadence":"daily 09:00"}`)
	tracker := finished("INTERNSHIP TRACKER", domain.TaskStatusCompleted, 10*day, long)
	sync := finished("LISTINGS SYNC", domain.TaskStatusCompleted, 10*day, long)
	alumni := finished("ALUMNI DM BATCH", domain.TaskStatusCompleted, 10*day, long)
	pid := uuid.New()
	child := finished("Subtask", domain.TaskStatusCompleted, 10*day, long)
	child.ParentID = &pid
	if got := archived(cadence, tracker, sync, alumni, child); len(got) != 0 {
		t.Fatalf("routines, trackers and subtasks must not be archived on their own: %v", got)
	}
}

func TestAutoArchiveKeepsLatestRunOfATracker(t *testing.T) {
	long := strings.Repeat("| a | b |\n", 5)
	older := finished("Find hackathons", domain.TaskStatusCompleted, 20*day, long)
	latest := finished("Find hackathons", domain.TaskStatusCompleted, 10*day, long)
	newerEmpty := finished("Find hackathons", domain.TaskStatusFailed, 4*day, "")
	got := archived(older, latest, newerEmpty)
	if got[latest.ID] || got[newerEmpty.ID] {
		t.Fatalf("the latest run, and the latest with an answer, stay for the Trackers page: %v", got)
	}
	if !got[older.ID] {
		t.Fatalf("an older run of the tracker is archived: %v", got)
	}
}

func TestAutoArchiveTrackerTypeSharedAcrossTitles(t *testing.T) {
	// The Trackers page makes one card per type, so the latest internship task of any title is the card.
	a := finished("Internship search A", domain.TaskStatusCompleted, 20*day, "x")
	b := finished("Find SDE internships", domain.TaskStatusCompleted, 10*day, "x")
	got := archived(a, b)
	if got[b.ID] || !got[a.ID] {
		t.Fatalf("one card per tracker type: %v", got)
	}
}

func TestAutoArchiveKeepsCompletedApplications(t *testing.T) {
	// The Trackers page derives "applied" from finished application tasks, so they must stay.
	done := finished("APPLY - Acme - SDE", domain.TaskStatusCompleted, 10*day, "drafted")
	done.InputPayload = json.RawMessage(`{"apply_for":{"company":"Acme"}}`)
	failed := finished("APPLY - Beta - SDE", domain.TaskStatusFailed, 10*day, "")
	failed.InputPayload = json.RawMessage(`{"apply_for":{"company":"Beta"}}`)
	got := archived(done, failed)
	if got[done.ID] {
		t.Fatal("a completed application is what marks a listing applied; it must stay")
	}
	if !got[failed.ID] {
		t.Fatal("a failed application does not mark anything, so it ages out like any other task")
	}
}

func TestAutoArchiveJunkIsImmediate(t *testing.T) {
	noResult := finished("Look something up", domain.TaskStatusFailed, time.Hour, "")
	reason := "claude exited 0 but returned no result"
	noResult.FailureReason = &reason
	otherFail := finished("Another", domain.TaskStatusFailed, time.Hour, "")
	other := "network down"
	otherFail.FailureReason = &other
	notFound := finished("Find a thing", domain.TaskStatusCompleted, time.Hour, "I couldn't find anything about that.")
	waiting := finished("Do later", domain.TaskStatusCompleted, time.Hour, "Waiting for the host to respond.")
	bg := finished("Run it", domain.TaskStatusCompleted, time.Hour, "Launched a background process to do this.")
	table := finished("Find rows", domain.TaskStatusCompleted, time.Hour, "| a | b |\n|---|---|\n| couldn't find | x |")
	real := finished("Short but real", domain.TaskStatusCompleted, time.Hour, "Done: the invoice is paid.")
	longMiss := finished("Long", domain.TaskStatusCompleted, time.Hour, "I couldn't find it. "+strings.Repeat("Here is context. ", 20))
	got := archived(noResult, otherFail, notFound, waiting, bg, table, real, longMiss)
	for _, j := range []*domain.Task{noResult, notFound, waiting, bg} {
		if !got[j.ID] {
			t.Fatalf("junk %q must be archived at once: %v", j.Title, got)
		}
	}
	for _, k := range []*domain.Task{otherFail, table, real, longMiss} {
		if got[k.ID] {
			t.Fatalf("%q is not junk and is too recent: %v", k.Title, got)
		}
	}
}

func TestAutoArchiveJunkTrackerRunStays(t *testing.T) {
	latest := finished("Find hackathons", domain.TaskStatusCompleted, time.Hour, "I couldn't find anything.")
	if archived(latest)[latest.ID] {
		t.Fatal("the latest run of a tracker is never archived, even when it found nothing")
	}
}

func TestAutoArchiveThreads(t *testing.T) {
	long := strings.Repeat("Substance. ", 30)
	root := finished("Draft a letter", domain.TaskStatusCompleted, 10*day, long)
	fresh := finished("Make it shorter", domain.TaskStatusCompleted, 1*day, long)
	inThread(fresh, root)
	if got := archived(root, fresh); len(got) != 0 {
		t.Fatalf("a thread ages by its newest message: %v", got)
	}
	stale := finished("Make it shorter", domain.TaskStatusCompleted, 4*day, long)
	inThread(stale, root)
	if got := archived(root, stale); !got[root.ID] || got[stale.ID] {
		t.Fatalf("an old thread is archived through its root only: %v", got)
	}
	running := finished("Make it shorter", domain.TaskStatusRunning, 4*day, "")
	inThread(running, root)
	if archived(root, running)[root.ID] {
		t.Fatal("a thread with a follow-up still working stays")
	}
}

func TestAutoArchiveTrackerFollowUpProtectsItsRoot(t *testing.T) {
	long := strings.Repeat("| a | b |\n", 5)
	root := finished("Hackathon tracker run", domain.TaskStatusCompleted, 30*day, long)
	follow := finished("also add ICPC", domain.TaskStatusCompleted, 20*day, long)
	inThread(follow, root)
	if got := archived(root, follow); len(got) != 0 {
		t.Fatalf("a tracker's winning follow-up keeps its root: %v", got)
	}
}

func TestIsJunkResultNeedsAFinishedTask(t *testing.T) {
	if domain.IsJunkResult(finished("x", domain.TaskStatusRunning, 0, "I couldn't find anything")) {
		t.Fatal("only finished tasks can be junk")
	}
}
