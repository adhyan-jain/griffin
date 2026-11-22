package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestParseSelfCommand_Positive(t *testing.T) {
	cases := []struct {
		in   string
		want SelfCommand
	}{
		// approve all
		{"approve all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all pending", SelfCommand{SelfApproveAllPending, ""}},
		{"Approve all", SelfCommand{SelfApproveAllPending, ""}},
		{"approve everything", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all of them", SelfCommand{SelfApproveAllPending, ""}},
		{"approve them all", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all the pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all waiting actions", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all pending approvals and send them", SelfCommand{SelfApproveAllPending, ""}},
		{"please approve all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all pending approvals please", SelfCommand{SelfApproveAllPending, ""}},
		{"ok approve everything now", SelfCommand{SelfApproveAllPending, ""}},
		{"yes, approve all pending!", SelfCommand{SelfApproveAllPending, ""}},
		{"go ahead and approve all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"can you approve all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"could you please approve everything?", SelfCommand{SelfApproveAllPending, ""}},
		{"I want you to approve all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"let's approve all pending", SelfCommand{SelfApproveAllPending, ""}},
		{"accept all pending approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"allow everything pending", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all approvals", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all pending approvals right now", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all karo", SelfCommand{SelfApproveAllPending, ""}},
		{"approve everything karo", SelfCommand{SelfApproveAllPending, ""}},
		{"sab approve karo", SelfCommand{SelfApproveAllPending, ""}},
		{"approve all for me", SelfCommand{SelfApproveAllPending, ""}},
		// approve one
		{"approve the Coy one", SelfCommand{SelfApprove, "coy"}},
		{"approve Coy", SelfCommand{SelfApprove, "coy"}},
		{"approve the approval for Coy", SelfCommand{SelfApprove, "coy"}},
		{"approve the message to Acme Corp", SelfCommand{SelfApprove, "acme corp"}},
		{"approve Coy's message", SelfCommand{SelfApprove, "coy"}},
		{"please approve the Coy approval", SelfCommand{SelfApprove, "coy"}},
		{"approve the linkedin dm to Priya Sharma", SelfCommand{SelfApprove, "linkedin priya sharma"}},
		{"approve only the coy one", SelfCommand{SelfApprove, "coy"}},
		// reject
		{"reject approval Coy", SelfCommand{SelfReject, "coy"}},
		{"reject the Coy one", SelfCommand{SelfReject, "coy"}},
		{"decline the Acme request", SelfCommand{SelfReject, "acme"}},
		{"deny the approval from Acme", SelfCommand{SelfReject, "acme"}},
		{"reject the cold email to Zed", SelfCommand{SelfReject, "cold email zed"}},
		{"dismiss the Coy approval", SelfCommand{SelfReject, "coy"}},
		// cancel task
		{"cancel task Coy", SelfCommand{SelfCancelTask, "coy"}},
		{"cancel the cover letter task", SelfCommand{SelfCancelTask, "cover letter"}},
		{"cancel the laptop research", SelfCommand{SelfCancelTask, "laptop research"}},
		{"please cancel my Acme task", SelfCommand{SelfCancelTask, "acme"}},
		{"stop the laptop research task", SelfCommand{SelfCancelTask, "laptop research"}},
		{"abort the linkedin scan", SelfCommand{SelfCancelTask, "linkedin scan"}},
		// retry task
		{"retry task Coy", SelfCommand{SelfRetryTask, "coy"}},
		{"retry the cover letter", SelfCommand{SelfRetryTask, "cover letter"}},
		{"rerun the laptop research task", SelfCommand{SelfRetryTask, "laptop research"}},
		{"re-run the linkedin scan", SelfCommand{SelfRetryTask, "linkedin scan"}},
		{"try the cover letter again", SelfCommand{SelfRetryTask, "cover letter"}},
		{"run the laptop research task again", SelfCommand{SelfRetryTask, "laptop research"}},
		{"resume the cover letter task", SelfCommand{SelfRetryTask, "cover letter"}},
		{"redo the Acme task", SelfCommand{SelfRetryTask, "acme"}},
		// routines
		{"pause routine daily scan", SelfCommand{SelfPauseRoutine, "daily scan"}},
		{"pause the daily scan routine", SelfCommand{SelfPauseRoutine, "daily scan"}},
		{"please pause my gmail routine", SelfCommand{SelfPauseRoutine, "gmail"}},
		{"stop the alumni routine", SelfCommand{SelfPauseRoutine, "alumni"}},
		{"turn off the alumni routine", SelfCommand{SelfPauseRoutine, "alumni"}},
		{"disable the internship schedule", SelfCommand{SelfPauseRoutine, "internship"}},
		{"resume routine daily scan", SelfCommand{SelfResumeRoutine, "daily scan"}},
		{"resume the gmail routine", SelfCommand{SelfResumeRoutine, "gmail"}},
		{"turn on the alumni routine", SelfCommand{SelfResumeRoutine, "alumni"}},
		{"unpause the daily scan routine", SelfCommand{SelfResumeRoutine, "daily scan"}},
		{"restart the internship schedule", SelfCommand{SelfResumeRoutine, "internship"}},
	}
	for _, c := range cases {
		if got := ParseSelfCommand(c.in); got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseSelfCommand_Negative(t *testing.T) {
	for _, s := range []string{
		"",
		"hello",
		// questions and explanations
		"what is approval",
		"what is an approval?",
		"should I approve all?",
		"should I approve all pending approvals",
		"why can't I approve all pending approvals",
		"how do I approve everything",
		"is it safe to approve all pending approvals",
		"do you approve all pending approvals?",
		"which approvals are pending?",
		"did you approve all pending",
		"approve all pending approvals?",
		"approve the Coy one?",
		// negation and conditions
		"don't approve all pending approvals",
		"do not approve anything",
		"please don't approve everything",
		"never approve all pending approvals",
		"approve all except Coy",
		"approve all but the Acme one",
		"approve all pending approvals if they look fine",
		"approve everything when I say so",
		"approve all after I check them",
		"approve nothing",
		"cannot approve all",
		// not supported or too vague: left to the normal path
		"approve",
		"approve it",
		"approve that",
		"approve the pending approval",
		"approve karo",
		"reject all",
		"reject everything",
		"reject it",
		"reject",
		"cancel all tasks",
		"cancel everything",
		"cancel it",
		"cancel",
		"cancel the approval for Coy",
		"retry",
		"retry all",
		"retry everything",
		"approve all the Coy ones",
		"pause",
		"pause routine",
		"pause all routines",
		"resume all routines",
		// ordinary talk
		"pause the music",
		"resume my career",
		"I think we should approve all pending approvals tomorrow after lunch when the team is back in the office",
		"tell me about approvals",
		"list approvals",
		"what needs my approval",
		"approvals",
		"the approval was rejected yesterday",
		"I approved all of them yesterday",
		"remind me to cancel my gym membership",
		"write a cover letter",
		"try again",
	} {
		if got := ParseSelfCommand(s); got.Kind != SelfNone {
			t.Errorf("%q must not be a self command, got %+v", s, got)
		}
	}
}

// ---- fakes ----

type fakeSelfTasks struct {
	tasks     []*domain.Task
	cancelled []uuid.UUID
	resumed   []uuid.UUID
	resumeErr error
}

func (f *fakeSelfTasks) ListTasks(_ context.Context, flt repository.TaskFilter) ([]*domain.Task, error) {
	var out []*domain.Task
	for _, t := range f.tasks {
		if flt.UserID == nil || t.UserID == *flt.UserID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakeSelfTasks) CancelTask(_ context.Context, id uuid.UUID, _ string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}
func (f *fakeSelfTasks) ResumeTask(_ context.Context, id uuid.UUID) error {
	if f.resumeErr != nil {
		return f.resumeErr
	}
	f.resumed = append(f.resumed, id)
	return nil
}

type fakeSelfApprovals struct {
	list    []*domain.Approval
	decided map[uuid.UUID]bool
	reasons []string
	failOn  uuid.UUID
}

func (f *fakeSelfApprovals) ListApprovals(_ context.Context, user uuid.UUID, st *domain.ApprovalStatus) ([]*domain.Approval, error) {
	var out []*domain.Approval
	for _, a := range f.list {
		if a.UserID == user && (st == nil || a.Status == *st) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeSelfApprovals) RespondApproval(_ context.Context, id, user uuid.UUID, ok bool, reason string) (*domain.Approval, error) {
	if id == f.failOn {
		return nil, errors.New("boom")
	}
	for _, a := range f.list {
		if a.ID == id && a.UserID == user {
			if f.decided == nil {
				f.decided = map[uuid.UUID]bool{}
			}
			f.decided[id] = ok
			f.reasons = append(f.reasons, reason)
			return a, nil
		}
	}
	return nil, domain.ErrNotFound
}

type fakeSelfRoutines struct {
	list   []*domain.Schedule
	paused map[uuid.UUID]bool
}

func (f *fakeSelfRoutines) ListSchedules(_ context.Context, user uuid.UUID) ([]*domain.Schedule, error) {
	var out []*domain.Schedule
	for _, r := range f.list {
		if r.UserID == user {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeSelfRoutines) SetPaused(_ context.Context, id, user uuid.UUID, p bool) error {
	if f.paused == nil {
		f.paused = map[uuid.UUID]bool{}
	}
	f.paused[id] = p
	return nil
}

func pendingApproval(user uuid.UUID, summary string, payload string) *domain.Approval {
	return &domain.Approval{ID: uuid.New(), UserID: user, TaskID: uuid.New(), ActionType: "SEND_MESSAGE", Summary: summary,
		ActionPayload: json.RawMessage(payload), Status: domain.ApprovalStatusPending}
}

func selfTask(user uuid.UUID, title string, st domain.TaskStatus) *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: user, Title: title, Status: st}
}

type selfEnv struct {
	user, other uuid.UUID
	tasks       *fakeSelfTasks
	approvals   *fakeSelfApprovals
	routines    *fakeSelfRoutines
	audit       []string
	cmds        *SelfCommands
}

func newSelfEnv() *selfEnv {
	e := &selfEnv{user: uuid.New(), other: uuid.New(), tasks: &fakeSelfTasks{}, approvals: &fakeSelfApprovals{}, routines: &fakeSelfRoutines{}}
	e.cmds = &SelfCommands{Tasks: e.tasks, Approvals: e.approvals, Routines: e.routines,
		Audit: func(_ context.Context, _ uuid.UUID, m string) { e.audit = append(e.audit, m) }}
	return e
}

func (e *selfEnv) say(t *testing.T, text string) SelfResult {
	t.Helper()
	c := ParseSelfCommand(text)
	if c.Kind == SelfNone {
		t.Fatalf("%q did not parse", text)
	}
	res, err := e.cmds.Run(context.Background(), e.user, c)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return res
}

// ---- approvals ----

func TestSelfApproveAll_ActsOnEveryPendingOfTheOwnerOnly(t *testing.T) {
	e := newSelfEnv()
	a1 := pendingApproval(e.user, "Message Coy on LinkedIn", `{}`)
	a2 := pendingApproval(e.user, "Email Acme recruiter", `{}`)
	theirs := pendingApproval(e.other, "Message Zed", `{}`)
	done := pendingApproval(e.user, "Old one", `{}`)
	done.Status = domain.ApprovalStatusApproved
	e.approvals.list = []*domain.Approval{a1, a2, theirs, done}

	res := e.say(t, "approve all pending approvals")
	if !res.Handled || !res.Wake {
		t.Fatalf("want handled and wake, got %+v", res)
	}
	if !e.approvals.decided[a1.ID] || !e.approvals.decided[a2.ID] {
		t.Fatalf("both of the owner's pending approvals must be approved: %v", e.approvals.decided)
	}
	if _, touched := e.approvals.decided[theirs.ID]; touched {
		t.Fatal("another user's approval must never be touched")
	}
	if _, touched := e.approvals.decided[done.ID]; touched {
		t.Fatal("an already decided approval must not be touched")
	}
	for _, want := range []string{"Approved 2 pending approvals", "Message Coy on LinkedIn", "Email Acme recruiter"} {
		if !strings.Contains(res.Reply, want) {
			t.Errorf("reply %q misses %q", res.Reply, want)
		}
	}
	if len(e.audit) != 2 || !strings.Contains(e.audit[0], "approve_all_pending") {
		t.Errorf("each approval needs an audit line, got %v", e.audit)
	}
}

func TestSelfApproveAll_ReportsFailuresAndEmpty(t *testing.T) {
	e := newSelfEnv()
	res := e.say(t, "approve everything")
	if !res.Handled || res.Wake || !strings.Contains(res.Reply, "Nothing is waiting") {
		t.Fatalf("empty: %+v", res)
	}
	a1 := pendingApproval(e.user, "Message Coy", `{}`)
	a2 := pendingApproval(e.user, "Email Acme", `{}`)
	e.approvals.list = []*domain.Approval{a1, a2}
	e.approvals.failOn = a2.ID
	res = e.say(t, "approve all")
	if !strings.Contains(res.Reply, "Approved 1 pending approval: Message Coy") || !strings.Contains(res.Reply, "1 could not be approved: Email Acme") {
		t.Fatalf("partial failure reply: %q", res.Reply)
	}
}

func TestSelfApprove_ByNameAndPayload(t *testing.T) {
	e := newSelfEnv()
	coy := pendingApproval(e.user, "LinkedIn message ready", `{"recipient":"Coy Hart","company":"Initech"}`)
	acme := pendingApproval(e.user, "Email to Acme recruiter", `{}`)
	coyote := pendingApproval(e.user, "Message to Coyote Labs", `{}`)
	e.approvals.list = []*domain.Approval{coy, acme, coyote}

	res := e.say(t, "approve the Coy one")
	if !res.Handled || !e.approvals.decided[coy.ID] || len(e.approvals.decided) != 1 {
		t.Fatalf("only the Coy approval (matched through its payload, and not Coyote) must be approved: %+v %v", res, e.approvals.decided)
	}
	if !strings.Contains(res.Reply, "Approved: LinkedIn message ready") {
		t.Errorf("reply: %q", res.Reply)
	}
	if len(e.audit) != 1 {
		t.Errorf("audit: %v", e.audit)
	}
}

func TestSelfApprove_AmbiguousChangesNothing(t *testing.T) {
	e := newSelfEnv()
	a := pendingApproval(e.user, "Email to Acme recruiter", `{}`)
	b := pendingApproval(e.user, "Message to Acme founder", `{}`)
	e.approvals.list = []*domain.Approval{a, b}
	res := e.say(t, "approve the Acme one")
	if len(e.approvals.decided) != 0 || res.Wake {
		t.Fatal("an ambiguous match must change nothing")
	}
	for _, want := range []string{"2 pending approvals match", "Email to Acme recruiter", "Message to Acme founder", "changed nothing"} {
		if !strings.Contains(res.Reply, want) {
			t.Errorf("reply %q misses %q", res.Reply, want)
		}
	}
}

func TestSelfApproveReject_NoMatchAndReject(t *testing.T) {
	e := newSelfEnv()
	a := pendingApproval(e.user, "Email to Acme recruiter", `{}`)
	e.approvals.list = []*domain.Approval{a}
	res := e.say(t, "approve the Zed one")
	if len(e.approvals.decided) != 0 || !strings.Contains(res.Reply, "No pending approval matches") || !strings.Contains(res.Reply, "Email to Acme recruiter") {
		t.Fatalf("no match: %q", res.Reply)
	}
	res = e.say(t, "reject approval acme")
	if ok, there := e.approvals.decided[a.ID]; !there || ok {
		t.Fatalf("must be rejected: %v", e.approvals.decided)
	}
	if res.Wake || !strings.Contains(res.Reply, "Rejected: Email to Acme recruiter") {
		t.Errorf("reject reply/wake: %+v", res)
	}
}

// ---- tasks ----

func TestSelfCancelTask(t *testing.T) {
	e := newSelfEnv()
	coy := selfTask(e.user, "Message Coy on LinkedIn", domain.TaskStatusRunning)
	theirs := selfTask(e.other, "Message Coy on LinkedIn", domain.TaskStatusRunning)
	e.tasks.tasks = []*domain.Task{coy, theirs}
	res := e.say(t, "cancel the Coy task")
	if len(e.tasks.cancelled) != 1 || e.tasks.cancelled[0] != coy.ID {
		t.Fatalf("only the owner's task is cancelled: %v", e.tasks.cancelled)
	}
	if !strings.Contains(res.Reply, "Cancelled task: Message Coy on LinkedIn") || len(e.audit) != 1 {
		t.Errorf("reply %q audit %v", res.Reply, e.audit)
	}
}

func TestSelfCancelTask_AmbiguousFinishedAndUnknown(t *testing.T) {
	e := newSelfEnv()
	e.tasks.tasks = []*domain.Task{
		selfTask(e.user, "Laptop research for work", domain.TaskStatusRunning),
		selfTask(e.user, "Laptop research notes", domain.TaskStatusQueued),
		selfTask(e.user, "Cover letter", domain.TaskStatusCompleted),
	}
	res := e.say(t, "cancel the laptop research")
	if len(e.tasks.cancelled) != 0 || !strings.Contains(res.Reply, "2 tasks match") || !strings.Contains(res.Reply, "Laptop research for work") {
		t.Fatalf("ambiguous: %q cancelled %v", res.Reply, e.tasks.cancelled)
	}
	res = e.say(t, "cancel the cover letter task")
	if len(e.tasks.cancelled) != 0 || !strings.Contains(res.Reply, "already finished") {
		t.Fatalf("finished: %q", res.Reply)
	}
	res = e.say(t, "cancel my gym membership")
	if res.Handled {
		t.Fatalf("a target that names none of the owner's tasks must fall through to chat: %+v", res)
	}
}

func TestSelfRetryTask(t *testing.T) {
	e := newSelfEnv()
	failed := selfTask(e.user, "Apply to Initech", domain.TaskStatusFailed)
	running := selfTask(e.user, "Research laptops", domain.TaskStatusRunning)
	e.tasks.tasks = []*domain.Task{failed, running}
	res := e.say(t, "retry the Initech task")
	if len(e.tasks.resumed) != 1 || e.tasks.resumed[0] != failed.ID || !res.Wake {
		t.Fatalf("resume: %v %+v", e.tasks.resumed, res)
	}
	if !strings.Contains(res.Reply, "Retrying task: Apply to Initech") || len(e.audit) != 1 {
		t.Errorf("reply %q audit %v", res.Reply, e.audit)
	}
	res = e.say(t, "retry the laptops task")
	if len(e.tasks.resumed) != 1 || !strings.Contains(res.Reply, "nothing to retry") {
		t.Fatalf("a running task is not retried: %q", res.Reply)
	}
	e.tasks.resumeErr = domain.ErrConflict
	res = e.say(t, "retry the Initech task")
	if !res.Handled || res.Wake || !strings.Contains(res.Reply, "could not retry") {
		t.Fatalf("conflict: %+v", res)
	}
}

// ---- routines ----

func TestSelfRoutines(t *testing.T) {
	e := newSelfEnv()
	scan := &domain.Schedule{ID: uuid.New(), UserID: e.user, Name: "Daily LinkedIn scan"}
	mail := &domain.Schedule{ID: uuid.New(), UserID: e.user, Name: "Gmail check", IsPaused: true}
	theirs := &domain.Schedule{ID: uuid.New(), UserID: e.other, Name: "Daily LinkedIn scan"}
	e.routines.list = []*domain.Schedule{scan, mail, theirs}

	res := e.say(t, "pause the daily scan routine")
	if !e.routines.paused[scan.ID] || len(e.routines.paused) != 1 || !strings.Contains(res.Reply, "Paused routine: Daily LinkedIn scan") {
		t.Fatalf("pause: %v %q", e.routines.paused, res.Reply)
	}
	res = e.say(t, "resume the gmail routine")
	if p, ok := e.routines.paused[mail.ID]; !ok || p {
		t.Fatalf("resume: %v", e.routines.paused)
	}
	if !strings.Contains(res.Reply, "Resumed routine: Gmail check") {
		t.Errorf("reply %q", res.Reply)
	}
	res = e.say(t, "pause the gmail routine")
	if !e.routines.paused[mail.ID] == false && !strings.Contains(res.Reply, "Paused") {
		t.Errorf("pausing a running routine: %q", res.Reply)
	}
	res = e.say(t, "pause the nonsense routine")
	if !res.Handled || !strings.Contains(res.Reply, "No routine matches") || !strings.Contains(res.Reply, "Gmail check") {
		t.Fatalf("unknown routine: %q", res.Reply)
	}
}

func TestSelfRoutines_AmbiguousAndAlreadyAndOff(t *testing.T) {
	e := newSelfEnv()
	a := &domain.Schedule{ID: uuid.New(), UserID: e.user, Name: "Daily scan morning"}
	b := &domain.Schedule{ID: uuid.New(), UserID: e.user, Name: "Daily scan evening"}
	c := &domain.Schedule{ID: uuid.New(), UserID: e.user, Name: "Alumni batch", IsPaused: true}
	e.routines.list = []*domain.Schedule{a, b, c}
	res := e.say(t, "pause the daily scan routine")
	if len(e.routines.paused) != 0 || !strings.Contains(res.Reply, "2 routines match") {
		t.Fatalf("ambiguous: %q", res.Reply)
	}
	res = e.say(t, "pause the alumni routine")
	if len(e.routines.paused) != 0 || !strings.Contains(res.Reply, "already paused") {
		t.Fatalf("already: %q", res.Reply)
	}
	e.cmds.Routines = nil
	if res := e.say(t, "pause the alumni routine"); res.Handled {
		t.Fatalf("routine commands off must fall through: %+v", res)
	}
}
