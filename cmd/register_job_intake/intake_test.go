package main

import (
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/scheduler"
)

func TestIntakeTasksFeedTheInternshipsTracker(t *testing.T) {
	tasks := intakeTasks("daily 10:00 IST", "daily 07:30 IST")
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if !strings.Contains(task.Title, "INTERNSHIP TRACKER") {
			t.Errorf("title %q would not join the Internships tracker", task.Title)
		}
		if !strings.Contains(task.Goal, "Never apply") {
			t.Errorf("%q must forbid applying without approval", task.Title)
		}
		if task.Payload["tracker_type"] != "internships" {
			t.Errorf("%q must target the internships ledger", task.Title)
		}
		if _, err := scheduler.NextOccurrence(task.Cadence, time.Now()); err != nil {
			t.Errorf("cadence %q is not understood by the scheduler: %v", task.Cadence, err)
		}
	}
	if !strings.Contains(tasks[0].Goal, "10 minutes") || tasks[0].Payload["device"] != "phone" {
		t.Error("the LinkedIn task must be a 10 minute phone scroll")
	}
}
