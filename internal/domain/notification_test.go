package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestANotificationKeyIsTheSameForTheSameEvent(t *testing.T) {
	id := uuid.New()
	if TaskNotificationKey(id, TaskStatusCompleted) != TaskNotificationKey(id, TaskStatusCompleted) {
		t.Fatal("the same task finishing the same way must give the same key, so it is pushed once")
	}
	if TaskNotificationKey(id, TaskStatusCompleted) == TaskNotificationKey(id, TaskStatusFailed) {
		t.Fatal("a different outcome is a different event")
	}
	if ApprovalNotificationKey(id) == TaskNotificationKey(id, TaskStatusAwaitingApproval) {
		t.Fatal("an approval and a task never share a key")
	}
}

func TestTappingANotificationOpensThePageItIsAbout(t *testing.T) {
	app := "https://griffin.example/"
	plain := &Task{ID: uuid.New(), Title: "Compare laptops", Goal: "compare three laptops"}
	if got, want := TaskActionURL(app, plain), "https://griffin.example/tasks/"+plain.ID.String(); got != want {
		t.Errorf("an ordinary task opens its own page: got %s want %s", got, want)
	}
	tracker := &Task{ID: uuid.New(), Title: "Internship tracker", Goal: "update the internship tracker"}
	if got, want := TaskActionURL(app, tracker), "https://griffin.example/trackers?task="+tracker.ID.String(); got != want {
		t.Errorf("a tracker task opens its tracker: got %s want %s", got, want)
	}
	payload, _ := json.Marshal(map[string]any{"apply_for": map[string]string{"company": "Acme"}})
	apply := &Task{ID: uuid.New(), Title: "APPLY - Acme - SDE Intern", Goal: "internship tracker row", InputPayload: payload}
	if got := TaskActionURL(app, apply); got != "https://griffin.example/tasks/"+apply.ID.String() {
		t.Errorf("an application opens its own task page, not the list: %s", got)
	}
	a := uuid.New()
	if got, want := ApprovalActionURL(app, a), "https://griffin.example/approvals?id="+a.String(); got != want {
		t.Errorf("an approval opens that approval: got %s want %s", got, want)
	}
}
