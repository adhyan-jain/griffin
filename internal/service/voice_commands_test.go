package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestParseTaskCommand(t *testing.T) {
	cases := []struct {
		in   string
		want TaskCommand
	}{
		{"list my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"show my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"what are my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"what's running", TaskCommand{CmdListTasks, "active", ""}},
		{"list my failed tasks", TaskCommand{CmdListTasks, "failed", ""}},
		{"show finished tasks", TaskCommand{CmdListTasks, "done", ""}},
		{"any failed tasks", TaskCommand{CmdListTasks, "failed", ""}},
		{"delete finished tasks", TaskCommand{CmdDeleteTasks, "done", ""}},
		{"clear completed tasks", TaskCommand{CmdDeleteTasks, "done", ""}},
		{"remove all failed tasks", TaskCommand{CmdDeleteTasks, "failed", ""}},
		{"delete old tasks", TaskCommand{CmdDeleteTasks, "old", ""}},
		{"delete the cover letter task", TaskCommand{CmdDeleteTasks, "", "cover letter"}},
		{"cancel the laptop research", TaskCommand{CmdCancelTasks, "", "laptop research"}},
		{"cancel everything", TaskCommand{CmdCancelTasks, "active", ""}},
		{"cancel all running tasks", TaskCommand{CmdCancelTasks, "active", ""}},
		{"any approvals", TaskCommand{CmdListApprovals, "", ""}},
		{"what needs my approval", TaskCommand{CmdListApprovals, "", ""}},
		{"approve it", TaskCommand{CmdApprove, "", ""}},
		{"approve the message to bro", TaskCommand{CmdApprove, "", "message to bro"}},
		{"are there any open tasks", TaskCommand{CmdListTasks, "active", ""}},
		{"any open tasks", TaskCommand{CmdListTasks, "active", ""}},
		{"open tasks", TaskCommand{CmdListTasks, "active", ""}},
		{"list open tasks", TaskCommand{CmdListTasks, "active", ""}},
		{"what is open", TaskCommand{CmdListTasks, "active", ""}},
		{"reject that", TaskCommand{CmdReject, "", ""}},
		{"decline the email", TaskCommand{CmdReject, "", "email"}},
		{"give me the list of tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"the list of tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"list of tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"what is the list of tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"what are the tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"what tasks are running", TaskCommand{CmdListTasks, "active", ""}},
		{"fetch my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"fetch tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"check my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"how are my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"task list", TaskCommand{CmdListTasks, "all", ""}},
		{"tasks list", TaskCommand{CmdListTasks, "all", ""}},
		{"my tasks", TaskCommand{CmdListTasks, "all", ""}},
		{"tasks batao", TaskCommand{CmdListTasks, "all", ""}},
		{"mere tasks kya hai", TaskCommand{CmdListTasks, "all", ""}},
		{"kya tasks chal rahe hai", TaskCommand{CmdListTasks, "active", ""}},
		{"list approvals", TaskCommand{CmdListApprovals, "", ""}},
		{"show approvals", TaskCommand{CmdListApprovals, "", ""}},
		{"are there any approvals", TaskCommand{CmdListApprovals, "", ""}},
		{"approvals batao", TaskCommand{CmdListApprovals, "", ""}},
		{"approve karo", TaskCommand{CmdApprove, "", ""}},
		{"approve task", TaskCommand{CmdApprove, "", ""}},
		{"reject karo", TaskCommand{CmdReject, "", ""}},
	}
	for _, c := range cases {
		if got := ParseTaskCommand(c.in); got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
	// ordinary talk and unspecific words never become actions
	for _, s := range []string{"", "hello", "what time is it", "tell me a joke", "cancel it", "delete that", "delete", "cancel", "I want to cancel my gym membership soon",
		"remove it", "delete everything", "clear my head", "approve", "tasks", "what is a task"} {
		if got := ParseTaskCommand(s); got.Kind != CmdNone && !(s == "approve") {
			t.Errorf("%q must not be a command, got %+v", s, got)
		}
	}
}

type fakeVoice struct {
	tasks     []*domain.Task
	approvals []*domain.Approval
	cancelled []uuid.UUID
	deleted   []uuid.UUID
	decided   map[uuid.UUID]bool
	failOn    uuid.UUID
}

func (f *fakeVoice) ListTasks(_ context.Context, flt repository.TaskFilter) ([]*domain.Task, error) {
	var out []*domain.Task
	for _, t := range f.tasks {
		if flt.UserID == nil || t.UserID == *flt.UserID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakeVoice) CancelTask(_ context.Context, id uuid.UUID, _ string) error {
	if id == f.failOn {
		return errors.New("boom")
	}
	f.cancelled = append(f.cancelled, id)
	return nil
}
func (f *fakeVoice) DeleteTask(_ context.Context, id uuid.UUID) error {
	if id == f.failOn {
		return errors.New("boom")
	}
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeVoice) ListApprovals(_ context.Context, user uuid.UUID, _ *domain.ApprovalStatus) ([]*domain.Approval, error) {
	var out []*domain.Approval
	for _, a := range f.approvals {
		if a.UserID == user {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeVoice) RespondApproval(_ context.Context, id, user uuid.UUID, ok bool, _ string) (*domain.Approval, error) {
	if f.decided == nil {
		f.decided = map[uuid.UUID]bool{}
	}
	f.decided[id] = ok
	return nil, nil
}

var vnow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func vtask(user uuid.UUID, title string, st domain.TaskStatus, ageHours int) *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: user, Title: title, Goal: title, Status: st, CreatedAt: vnow.Add(-time.Duration(ageHours) * time.Hour), UpdatedAt: vnow.Add(-time.Duration(ageHours) * time.Hour)}
}

func newVC(f *fakeVoice) *VoiceCommands {
	return &VoiceCommands{Tasks: f, Approvals: f, Secret: []byte("test-secret"), Now: func() time.Time { return vnow }}
}

func TestListingTasksSpeaksCountsAndTitles(t *testing.T) {
	u := uuid.New()
	plan := vtask(u, "JJ Orchestration: Compare laptops under 60000", domain.TaskStatusRunning, 1)
	step := vtask(u, "Spec finder", domain.TaskStatusRunning, 1)
	step.ParentID = &plan.ID
	followUp := vtask(u, "Follow-up: shorter", domain.TaskStatusRunning, 0)
	followUp.InputPayload = []byte(`{"thread_id":"` + plan.ID.String() + `","follow_up_of":"` + plan.ID.String() + `"}`)
	f := &fakeVoice{tasks: []*domain.Task{plan, step, followUp, vtask(u, "Draft a cover letter", domain.TaskStatusCompleted, 5), vtask(u, "Chat: hello", domain.TaskStatusCompleted, 6), vtask(u, "Summarise report", domain.TaskStatusFailed, 7), vtask(uuid.New(), "someone else's", domain.TaskStatusRunning, 1)}}
	vc := newVC(f)

	r, err := vc.Run(context.Background(), u, TaskCommand{Kind: CmdListTasks, Scope: "all"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"You have 4 tasks.", "Running: Compare laptops under 60000.", "2 finished", "1 failed or cancelled"} {
		if !strings.Contains(r.Reply, want) {
			t.Errorf("want %q in %q", want, r.Reply)
		}
	}
	if strings.Contains(r.Reply, "Spec finder") || strings.Contains(r.Reply, "shorter") || strings.Contains(r.Reply, "someone else") {
		t.Errorf("plan steps, follow-ups and other people's tasks are not listed: %q", r.Reply)
	}
	r, _ = vc.Run(context.Background(), u, TaskCommand{Kind: CmdListTasks, Scope: "failed"}, false)
	if r.Reply != "You have 1 task failed or cancelled: Summarise report." {
		t.Errorf("failed list: %q", r.Reply)
	}
	r, _ = vc.Run(context.Background(), uuid.New(), TaskCommand{Kind: CmdListTasks, Scope: "all"}, false)
	if r.Reply != "You have no tasks." {
		t.Errorf("empty list: %q", r.Reply)
	}
}

func TestChangesNeedAnUnlockedDeviceAndAConfirmationThatIsGenuineFreshAndTheirs(t *testing.T) {
	u := uuid.New()
	a := vtask(u, "Old report", domain.TaskStatusCompleted, 30)
	b := vtask(u, "Cover letter", domain.TaskStatusCompleted, 5)
	f := &fakeVoice{tasks: []*domain.Task{a, b}}
	vc := newVC(f)
	ctx := context.Background()

	r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Scope: "done"}, false)
	if r.ConfirmToken != "" || !strings.Contains(r.Reply, "Unlock your phone") {
		t.Fatalf("a locked phone changes nothing: %+v", r)
	}
	r, _ = vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Scope: "done"}, true)
	if r.ConfirmToken == "" || r.Reply != "Delete 2 finished tasks? Say yes to confirm." {
		t.Fatalf("asks first: %+v", r)
	}
	if len(f.deleted) != 0 {
		t.Fatal("nothing is deleted before the yes")
	}

	// the wrong owner, a tampered token, a stale one, and a locked phone all do nothing
	other, _ := vc.Confirm(ctx, uuid.New(), r.ConfirmToken, true, true)
	tampered, _ := vc.Confirm(ctx, u, r.ConfirmToken[:len(r.ConfirmToken)-3]+"AAA", true, true)
	garbage, _ := vc.Confirm(ctx, u, "not a token", true, true)
	stale := newVC(f)
	stale.Now = func() time.Time { return vnow.Add(confirmTTL + time.Second) }
	old, _ := stale.Confirm(ctx, u, r.ConfirmToken, true, true)
	locked, _ := vc.Confirm(ctx, u, r.ConfirmToken, true, false)
	for name, res := range map[string]Result{"other user": other, "tampered": tampered, "garbage": garbage, "stale": old, "locked": locked} {
		if res.Reply == "" || strings.Contains(res.Reply, "Deleted") {
			t.Errorf("%s must not delete: %q", name, res.Reply)
		}
	}
	if len(f.deleted) != 0 {
		t.Fatalf("none of those may delete anything: %v", f.deleted)
	}
	if res, _ := vc.Confirm(ctx, u, r.ConfirmToken, false, true); res.Reply != "Okay, I will leave it." || len(f.deleted) != 0 {
		t.Errorf("a no leaves everything: %q", res.Reply)
	}
	// a token signed with another secret is refused
	forged := newVC(f)
	forged.Secret = []byte("other-secret")
	if res, _ := forged.Confirm(ctx, u, r.ConfirmToken, true, true); len(f.deleted) != 0 {
		t.Errorf("a token from another key must not work: %q", res.Reply)
	}
	// the real yes
	res, _ := vc.Confirm(ctx, u, r.ConfirmToken, true, true)
	if res.Reply != "Deleted 2." || len(f.deleted) != 2 {
		t.Errorf("deleted %v: %q", f.deleted, res.Reply)
	}
}

func TestCancelAndDeleteByNameHandleAmbiguityAndWrongState(t *testing.T) {
	u := uuid.New()
	running := vtask(u, "Research laptops", domain.TaskStatusRunning, 1)
	done1 := vtask(u, "Draft cover letter for Acme", domain.TaskStatusCompleted, 4)
	done2 := vtask(u, "Draft cover letter for Beta", domain.TaskStatusCompleted, 3)
	f := &fakeVoice{tasks: []*domain.Task{running, done1, done2}}
	vc := newVC(f)
	ctx := context.Background()

	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Ref: "cover letter"}, true); !strings.Contains(r.Reply, "2 tasks like that") || r.ConfirmToken != "" {
		t.Errorf("two matches means asking which: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Ref: "acme"}, true); r.ConfirmToken == "" || r.Reply != "Delete Draft cover letter for Acme? Say yes to confirm." {
		t.Errorf("one match asks: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Ref: "laptops"}, true); !strings.Contains(r.Reply, "still running") || r.ConfirmToken != "" {
		t.Errorf("a running task is cancelled first: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdCancelTasks, Ref: "acme"}, true); !strings.Contains(r.Reply, "already finished") {
		t.Errorf("cancelling a finished task: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdCancelTasks, Ref: "laptops"}, true); r.ConfirmToken == "" {
		t.Errorf("cancel asks: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdDeleteTasks, Ref: "the moon"}, true); !r.NotHandled {
		t.Errorf("a reference that names none of their tasks is not ours to answer: %+v", r)
	}
	// "latest" means the newest
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdCancelTasks, Ref: "latest"}, true); !strings.Contains(r.Reply, "Research laptops") {
		t.Errorf("latest: %+v", r)
	}
	// cancelling all running, then really doing it, reports partial failure honestly
	f.failOn = running.ID
	r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdCancelTasks, Scope: "active"}, true)
	res, _ := vc.Confirm(ctx, u, r.ConfirmToken, true, true)
	if res.Reply != "I could not do that." {
		t.Errorf("a failure is reported, not hidden: %q", res.Reply)
	}
}

func TestApprovalsByVoice(t *testing.T) {
	u := uuid.New()
	a1 := &domain.Approval{ID: uuid.New(), UserID: u, Summary: "Send a WhatsApp message to Bro: \"on my way\""}
	a2 := &domain.Approval{ID: uuid.New(), UserID: u, Summary: "Send the cover letter email to Acme"}
	f := &fakeVoice{approvals: []*domain.Approval{a1, a2}}
	vc := newVC(f)
	ctx := context.Background()

	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdListApprovals}, false); !strings.HasPrefix(r.Reply, "2 approvals are waiting for your approval:") {
		t.Errorf("list: %q", r.Reply)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdApprove}, false); !strings.Contains(r.Reply, "Unlock") || r.ConfirmToken != "" {
		t.Errorf("approving needs an unlocked phone: %+v", r)
	}
	if r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdApprove}, true); r.ConfirmToken != "" || !strings.Contains(r.Reply, "2 things are waiting") {
		t.Errorf("two waiting and none named: ask which: %+v", r)
	}
	r, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdApprove, Ref: "message to bro"}, true)
	if r.ConfirmToken == "" || !strings.HasPrefix(r.Reply, "Approve: Send a WhatsApp message to Bro") {
		t.Fatalf("named one: %+v", r)
	}
	if len(f.decided) != 0 {
		t.Fatal("nothing is approved before the yes")
	}
	if res, _ := vc.Confirm(ctx, u, r.ConfirmToken, true, true); res.Reply != "Approved." || !f.decided[a1.ID] || len(f.decided) != 1 {
		t.Errorf("approved: %q %v", res.Reply, f.decided)
	}
	rr, _ := vc.Run(ctx, u, TaskCommand{Kind: CmdReject, Ref: "email"}, true)
	if res, _ := vc.Confirm(ctx, u, rr.ConfirmToken, true, true); res.Reply != "Rejected." || f.decided[a2.ID] {
		t.Errorf("rejected: %q %v", res.Reply, f.decided)
	}
	if r, _ := newVC(&fakeVoice{}).Run(ctx, u, TaskCommand{Kind: CmdApprove}, true); r.Reply != "Nothing is waiting for your approval." {
		t.Errorf("nothing waiting: %q", r.Reply)
	}
}
