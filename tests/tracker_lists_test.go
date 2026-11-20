package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/griffin/core/internal/storage"
)

// A task run on one of the owner's devices starts from the shared tracker list, and the list it hands back
// replaces it, so "remove the passed ones" really removes them.
func TestDeviceTasksReadAndUpdateTheSharedTrackerList(t *testing.T) {
	e := setupHostEnv(t)
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")

	three := "| Company | Role | Deadline |\n|---|---|---|\n| Acme | SDE Intern | 30 Dec 2099 |\n| Old Co | SDE Intern | 10 Nov 2020 |\n| Beta | ML Intern | 5 Jan 2099 |"
	one := "| Company | Role | Deadline |\n|---|---|---|\n| Acme | SDE Intern | 30 Dec 2099 |\n| Beta | ML Intern | 5 Jan 2099 |"

	run := func(title, answer string) string {
		task := e.newTask(t, title)
		code, out := e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
		if code != http.StatusOK {
			t.Fatalf("claim: %d %v", code, out)
		}
		goal, _ := unwrap(out)["task"].(map[string]any)["goal"].(string)
		if code, out := e.do(t, "POST", "/api/v1/host/tasks/"+task.ID.String()+"/complete", laptop,
			map[string]any{"output": map[string]any{"response": answer}}); code != http.StatusOK {
			t.Fatalf("complete: %d %v", code, out)
		}
		return goal
	}
	listText := func() string {
		arts, err := e.StorageService.ListByKind(context.Background(), e.userID, storage.ListKind)
		if err != nil || len(arts) != 1 {
			t.Fatalf("want exactly one shared list, got %d (%v)", len(arts), err)
		}
		text, err := e.StorageService.ReadText(context.Background(), e.userID, arts[0].ID, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		return text
	}

	run("INTERNSHIP TRACKER (daily 08:00)", "Here is the list:\n\n"+three)
	if !strings.Contains(listText(), "Old Co") {
		t.Fatal("the first table must start the shared list")
	}

	goal := run("remove the passed internships, i havent applied on any of them", "Removed Old Co.\n\n## Tracker\n\n"+one)
	for _, want := range []string{"Old Co", "Today is", "## Tracker"} {
		if !strings.Contains(goal, want) {
			t.Fatalf("the device must be shown the list and the date; missing %q in %q", want, goal)
		}
	}
	got := listText()
	if strings.Contains(got, "Old Co") || !strings.Contains(got, "Beta") {
		t.Fatalf("the removal must stick: %q", got)
	}
}
