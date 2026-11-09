package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
)

func done(goal, title, answer string) *domain.Task {
	t := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: title, Goal: goal, Status: domain.TaskStatusCompleted}
	if answer != "" {
		t.OutputPayload, _ = json.Marshal(map[string]string{"response": answer})
	}
	return t
}

func TestSpokenSummaryDependsOnTheCase(t *testing.T) {
	long := "**Recommendation:** the Acer Swift Go 14 is the best value. " + strings.Repeat("It has a bright OLED screen, a quiet fan and good battery life. ", 12) + "See [the review](https://example.com/review) and https://example.com/x for details.\n\n- Spec one\n- Spec two\n```go\nfmt.Println(1)\n```"
	reason := "the worker ran out of time while reading the report"
	failed := done("Summarise the report", "Summarise the report", "")
	failed.Status, failed.FailureReason = domain.TaskStatusFailed, &reason
	approval := done("Send the email", "Send the email to Sam", "")
	approval.Status = domain.TaskStatusAwaitingApproval
	running := done("x", "Long job", "")
	running.Status = domain.TaskStatusRunning

	cases := []struct {
		name string
		task *domain.Task
		want SpokenCase
		has  []string
		not  []string
	}{
		{"short answer is read as it is", done("q", "Q", "It is **4** o'clock in the afternoon."), CaseAnswer, []string{"It is 4 o'clock in the afternoon."}, []string{"*"}},
		{"long result without a model uses the first sentences", done("laptops", "Compare laptops", long), CaseSummary, []string{"Recommendation: the Acer Swift Go 14 is the best value."}, []string{"http", "**", "[", "```", "Spec one"}},
		{"no text", done("g", "Make the chart", ""), CaseEmpty, []string{"Make the chart is done", "files"}, nil},
		{"failed says why and what to do", failed, CaseFailed, []string{"did not finish", "ran out of time", "try again"}, nil},
		{"approval names the manager", approval, CaseApproval, []string{"Jarvis needs your approval", "Send the email to Sam"}, nil},
		{"still running", running, CaseRunning, []string{"Long job is still working"}, nil},
	}
	for _, c := range cases {
		got := (&Summarizer{}).Spoken(context.Background(), c.task, "Jarvis")
		if got.Case != c.want {
			t.Errorf("%s: case %s, want %s (%q)", c.name, got.Case, c.want, got.Text)
		}
		for _, h := range c.has {
			if !strings.Contains(got.Text, h) {
				t.Errorf("%s: %q should contain %q", c.name, got.Text, h)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got.Text, n) {
				t.Errorf("%s: %q must not contain %q", c.name, got.Text, n)
			}
		}
		if len([]rune(got.Text)) > 520 {
			t.Errorf("%s: a notification must be short, got %d characters", c.name, len([]rune(got.Text)))
		}
	}
}

func TestSpokenSummaryUsesTheFastModelForLongResultsAndFallsBackWhenItFails(t *testing.T) {
	long := done("laptops", "Compare laptops", strings.Repeat("A long detailed comparison sentence. ", 30))
	brain := &stubBrain{on: true, reply: "**Pick the Acer Swift Go 14**; I compared five laptops."}
	got := (&Summarizer{Brain: brain}).Spoken(context.Background(), long, "JJ")
	if got.Text != "Pick the Acer Swift Go 14; I compared five laptops." || got.Case != CaseSummary {
		t.Errorf("the model's words are used, cleaned for speech: %+v", got)
	}
	if !strings.Contains(brain.asked, "Compare laptops") {
		t.Error("the model is told what the task was")
	}
	// short answers never need a model
	brain.calls = 0
	(&Summarizer{Brain: brain}).Spoken(context.Background(), done("q", "Q", "Four."), "JJ")
	if brain.calls != 0 {
		t.Error("a short answer is read as it is, with no model call")
	}
	for name, b := range map[string]*stubBrain{"error": {on: true, err: errors.New("quota")}, "off": {on: false}, "empty": {on: true, reply: "  "}} {
		got := (&Summarizer{Brain: b}).Spoken(context.Background(), long, "JJ")
		if got.Case != CaseSummary || !strings.HasPrefix(got.Text, "A long detailed comparison sentence.") {
			t.Errorf("%s: must fall back to the answer's own first sentences, got %+v", name, got)
		}
	}
	_ = gemini.DefaultModel
}

type captureSender struct {
	mu   sync.Mutex
	msgs []notify.Message
	err  error
}

func (c *captureSender) Send(_ context.Context, m notify.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
	return c.err
}

func TestTaskNotifierWordsEachCaseAndLinksToTheListenPage(t *testing.T) {
	s := &captureSender{}
	n := &TaskNotifier{Sender: s, Summ: &Summarizer{}, AppURL: "https://app.example/", ManagerName: func(context.Context, uuid.UUID) string { return "Jarvis" }}

	ok := done("Draft a cover letter", "Cover letter", "Dear team, I would love to join.")
	n.TaskFinished(context.Background(), ok)
	bad := done("Summarise", "Summarise", "")
	bad.Status = domain.TaskStatusFailed
	n.TaskFinished(context.Background(), bad)

	if len(s.msgs) != 2 {
		t.Fatalf("want two notifications, got %d", len(s.msgs))
	}
	if m := s.msgs[0]; m.Title != "Jarvis: done" || m.Body != "Dear team, I would love to join." || m.ClickURL != "https://app.example/listen/"+ok.ID.String() || m.Priority != 0 {
		t.Errorf("done: %+v", m)
	}
	if m := s.msgs[1]; m.Title != "Jarvis: needs attention" || m.Priority != 4 || !strings.Contains(m.Body, "did not finish") {
		t.Errorf("failed: %+v", m)
	}
	// a sender that fails never panics or blocks the caller
	n.Sender = &captureSender{err: errors.New("down")}
	n.TaskFinished(context.Background(), ok)
	(*TaskNotifier)(nil).TaskFinished(context.Background(), ok)
	(&TaskNotifier{}).TaskFinished(context.Background(), ok)
}

// repoFake implements only what the engine touches when a task finishes.
type repoFake struct {
	repository.TaskRepository
	tasks map[uuid.UUID]*domain.Task
}

func (r *repoFake) GetTaskByID(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	if t, ok := r.tasks[id]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}
func (r *repoFake) CompleteTask(_ context.Context, id uuid.UUID, _ string, out json.RawMessage) error {
	r.tasks[id].Status, r.tasks[id].OutputPayload = domain.TaskStatusCompleted, out
	return nil
}
func (r *repoFake) FailTask(_ context.Context, id uuid.UUID, _ string, reason string, retryable bool, _ time.Duration) error {
	t := r.tasks[id]
	if retryable && t.RetryCount+1 < t.MaxRetries {
		t.Status, t.RetryCount = domain.TaskStatusRetrying, t.RetryCount+1
		return nil
	}
	t.Status, t.FailureReason = domain.TaskStatusFailed, &reason
	return nil
}
func (r *repoFake) RecordEvent(context.Context, *domain.TaskEvent) error { return nil }
func (r *repoFake) GetDependentTasks(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *repoFake) UnblockTaskIfEligible(context.Context, uuid.UUID) (bool, error) { return false, nil }

func TestEngineAnnouncesOnlyFinishedTopLevelTasks(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Goal: "Plan the trip", Title: "Plan the trip", Status: domain.TaskStatusRunning, MaxRetries: 3}
	step := &domain.Task{ID: uuid.New(), UserID: root.UserID, Goal: "step", Title: "step", Status: domain.TaskStatusRunning, MaxRetries: 3, ParentID: &root.ID}
	flaky := &domain.Task{ID: uuid.New(), UserID: root.UserID, Goal: "flaky", Title: "flaky", Status: domain.TaskStatusRunning, MaxRetries: 3}
	repo := &repoFake{tasks: map[uuid.UUID]*domain.Task{root.ID: root, step.ID: step, flaky.ID: flaky}}
	s := &captureSender{}
	e := NewTaskEngine(repo, nil, nil)
	e.SetNotifier(&TaskNotifier{Sender: s, Summ: &Summarizer{}, AppURL: "https://a"})
	ctx := context.Background()

	if err := e.CompleteTask(ctx, step.ID, "w1", json.RawMessage(`{"response":"part"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.msgs) != 0 {
		t.Fatal("a step of a plan is not announced; the plan is")
	}
	if err := e.FailTask(ctx, flaky.ID, "w1", "temporary", true); err != nil {
		t.Fatal(err)
	}
	if len(s.msgs) != 0 {
		t.Fatal("a task that will be retried has not finished, so nothing is announced")
	}
	if err := e.CompleteTask(ctx, root.ID, "JJ_ORCHESTRATOR", json.RawMessage(`{"response":"The trip plan is ready."}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.msgs) != 1 || s.msgs[0].Body != "The trip plan is ready." {
		t.Fatalf("a finished top-level task is announced once: %+v", s.msgs)
	}
	flaky.RetryCount = 2
	if err := e.FailTask(ctx, flaky.ID, "w1", "gave up", true); err != nil {
		t.Fatal(err)
	}
	if len(s.msgs) != 2 || !strings.Contains(s.msgs[1].Body, "gave up") {
		t.Fatalf("a permanent failure is announced with its reason: %+v", s.msgs)
	}
}
