package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

type fakeJobQueue struct{ tasks []*domain.Task }

func (q *fakeJobQueue) LeaseNextAgentJob(_ context.Context, worker string, _ time.Duration, exclude []uuid.UUID) (*domain.Task, error) {
	for _, t := range q.tasks {
		skip := false
		for _, x := range exclude {
			skip = skip || x == t.ID
		}
		if !skip && t.Status == domain.TaskStatusQueued {
			t.Status = domain.TaskStatusRunning
			return t, nil
		}
	}
	return nil, nil
}

func (q *fakeJobQueue) ReleaseLeasedTask(_ context.Context, id uuid.UUID, _ string) (bool, error) {
	for _, t := range q.tasks {
		if t.ID == id && t.Status == domain.TaskStatusRunning {
			t.Status = domain.TaskStatusQueued
			return true, nil
		}
	}
	return false, nil
}

type fakeJobEngine struct {
	q       *fakeJobQueue
	outputs map[uuid.UUID]string
	failed  map[uuid.UUID]string
}

func (f *fakeJobEngine) set(id uuid.UUID, st domain.TaskStatus) {
	for _, t := range f.q.tasks {
		if t.ID == id {
			t.Status = st
		}
	}
}
func (f *fakeJobEngine) CompleteTask(_ context.Context, id uuid.UUID, _ string, out json.RawMessage) error {
	f.outputs[id] = string(out)
	f.set(id, domain.TaskStatusCompleted)
	return nil
}
func (f *fakeJobEngine) FailTask(_ context.Context, id uuid.UUID, _ string, reason string, _ bool) error {
	f.failed[id] = reason
	f.set(id, domain.TaskStatusFailed)
	return nil
}

// jobEnv is an env with a chat that already exists (the job's origin) and one queued job task.
type jobEnv struct {
	*env
	q      *fakeJobQueue
	eng    *fakeJobEngine
	task   *domain.Task
	jobCnv uuid.UUID
	runner *JobRunner
}

func newJobEnv(t *testing.T, replies ...gemini.ToolReply) *jobEnv {
	e := newEnv(t, append([]gemini.ToolReply{say("Noted.")}, replies...)...)
	if _, err := e.run("do a long job"); err != nil { // creates the origin conversation
		t.Fatal(err)
	}
	je := &jobEnv{env: e, jobCnv: uuid.New(), q: &fakeJobQueue{}}
	payload, _ := json.Marshal(domain.AgentJobPayload{Kind: "agent_job", Goal: "find interns", ConversationID: je.jobCnv.String(), OriginConversation: e.conv.String()})
	je.task = &domain.Task{ID: uuid.New(), UserID: e.user, Title: domain.AgentJobTitlePrefix + "find interns", Status: domain.TaskStatusQueued, InputPayload: payload}
	je.q.tasks = []*domain.Task{je.task}
	je.eng = &fakeJobEngine{q: je.q, outputs: map[uuid.UUID]string{}, failed: map[uuid.UUID]string{}}
	je.runner = &JobRunner{Queue: je.q, Engine: je.eng, Agent: e.agent, Budget: 20 * time.Second}
	return je
}

func (je *jobEnv) originTurns() []Turn {
	ts, _ := je.store.RecentTurns(context.Background(), je.user, je.conv, 50)
	return ts
}

func search(q string) gemini.ToolReply { return call("web_search", map[string]any{"query": q}) }

// slowModel makes the first reply take long enough that the tick's budget runs out after one round.
type slowModel struct {
	Model
	delay time.Duration
	n     int
}

func (s *slowModel) Generate(ctx context.Context, sys string, c []gemini.Content, d []gemini.FunctionDecl) (gemini.ToolReply, error) {
	s.n++
	if s.n == 1 {
		time.Sleep(s.delay)
	}
	return s.Model.Generate(ctx, sys, c, d)
}

func TestJobSpansTwoTicksResumesAndPostsToOrigin(t *testing.T) {
	je := newJobEnv(t, search("interns"), say("Found one listing: Intern at Acme."))
	je.agent.Model = &slowModel{Model: je.model, delay: 400 * time.Millisecond}
	je.runner.Budget = 3300 * time.Millisecond // a round needs 3s left: one round fits, a second does not

	if err := je.runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if je.task.Status != domain.TaskStatusQueued || len(je.eng.outputs) != 0 {
		t.Fatalf("after tick 1 the job must be released, not finished: %s %v", je.task.Status, je.eng.outputs)
	}
	conv, _ := je.store.GetConversation(context.Background(), je.user, je.jobCnv)
	if conv == nil || conv.WorkingSet.Job == nil || conv.WorkingSet.Job.Rounds != 1 || len(conv.WorkingSet.Job.Contents) != 3 {
		t.Fatalf("state after tick 1 = %+v", conv)
	}

	if err := je.runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if je.task.Status != domain.TaskStatusCompleted || !strings.Contains(je.eng.outputs[je.task.ID], "Intern at Acme") {
		t.Fatalf("after tick 2: %s %v", je.task.Status, je.eng.outputs)
	}
	// the second tick's model call saw the first tick's tool call and result
	last := je.model.seen[len(je.model.seen)-1]
	if len(last) != 3 || last[1].Parts[0].FunctionCall == nil || last[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("resumed contents = %+v", last)
	}
	turns := je.originTurns()
	got := turns[len(turns)-1]
	if got.Role != "assistant" || !strings.HasPrefix(got.Text, "Job done: ") || !strings.Contains(got.Text, "Intern at Acme") {
		t.Fatalf("origin turn = %+v", got)
	}
	// a third tick finds nothing to do and posts nothing more
	n := len(turns)
	_ = je.runner.Run(context.Background())
	if len(je.originTurns()) != n {
		t.Fatal("a finished job posted again")
	}
}

func TestJobRoundCapFailsWithReason(t *testing.T) {
	je := newJobEnv(t)
	je.model.replies = nil
	l := search("again")
	je.model.loop = &l
	je.runner.MaxRounds = 3
	if err := je.runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if je.task.Status != domain.TaskStatusFailed || !strings.Contains(je.eng.failed[je.task.ID], "3 steps") {
		t.Fatalf("status=%s failed=%v", je.task.Status, je.eng.failed)
	}
	turns := je.originTurns()
	if !strings.HasPrefix(turns[len(turns)-1].Text, "Job failed: ") {
		t.Fatalf("origin = %+v", turns[len(turns)-1])
	}
}

func TestJobStopsAfterMaxAge(t *testing.T) {
	je := newJobEnv(t)
	in := JobInput{UserID: je.user, ConversationID: je.jobCnv, Goal: "find interns", Deadline: time.Now(), MaxAge: time.Hour}
	if res, err := je.agent.RunJob(context.Background(), in); err != nil || res.Done || res.Failed != "" {
		t.Fatalf("first slice = %+v %v", res, err)
	}
	je.now = je.now.Add(3 * time.Hour)
	in.Deadline = time.Now().Add(time.Minute)
	res, err := je.agent.RunJob(context.Background(), in)
	if err != nil || !strings.Contains(res.Failed, "1h0m0s") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestJobOutwardToolMakesApprovalAndExecutesNothing(t *testing.T) {
	je := newJobEnv(t,
		call("send_test_message", map[string]any{"to": "Asha", "text": "hello"}),
		search("more"),
		say("Looked for more listings. The message to Asha is waiting for your approval; nothing was sent."))
	if err := je.runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(je.e().sent) != 0 || len(je.apps.pending) != 1 || je.apps.pending[0].ActionType != "send_test_message" {
		t.Fatalf("sent=%v pending=%d", je.e().sent, len(je.apps.pending))
	}
	if je.task.Status != domain.TaskStatusCompleted {
		t.Fatalf("the job must finish while the approval waits: %s", je.task.Status)
	}
	out := je.eng.outputs[je.task.ID]
	if !strings.Contains(out, jobPendingHeading) || !strings.Contains(out, "Message to Asha") {
		t.Fatalf("report = %s", out)
	}
	turns := je.originTurns()
	got := turns[len(turns)-1]
	if len(got.Actions) != 1 || !got.Actions[0].Pending {
		t.Fatalf("origin actions = %+v", got.Actions)
	}
}

func (je *jobEnv) e() *env { return je.env }

func TestStartJobCreatesTaskAndConversation(t *testing.T) {
	e := newEnv(t, call("start_job", map[string]any{"goal": "check every listing link"}), say("Started the job."))
	tools := NewTools(Deps{Tasks: e.tasks, Store: e.store, Jobs: true})
	reg, err := NewRegistry(tools...)
	if err != nil {
		t.Fatal(err)
	}
	e.agent.Registry = reg
	res, err := e.run("check every listing link")
	if err != nil || res.Handoff != nil || len(res.Actions) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(e.tasks.created) != 1 {
		t.Fatalf("created = %d", len(e.tasks.created))
	}
	task := e.tasks.created[0]
	var p domain.AgentJobPayload
	_ = json.Unmarshal(task.InputPayload, &p)
	jc, _ := uuid.Parse(p.ConversationID)
	if !domain.IsAgentJob(task.Title) || task.ManagerDomain != "JJ" || p.Kind != "agent_job" || p.Goal != "check every listing link" || p.OriginConversation != e.conv.String() {
		t.Fatalf("task = %+v payload = %+v", task, p)
	}
	if c, _ := e.store.GetConversation(context.Background(), e.user, jc); c == nil {
		t.Fatal("the job conversation was not created")
	}
	if !strings.Contains(res.Actions[0].Summary, task.ID.String()) || res.Actions[0].UndoID == "" {
		t.Fatalf("action = %+v", res.Actions[0])
	}
	// a scheduled run cannot start a job
	e2 := newEnv(t, call("start_job", map[string]any{"goal": "x"}), say("I could not start it."))
	e2.agent.Registry = reg
	if _, err := e2.runAs("x", OriginScheduled); err != nil || len(e.tasks.created) != 1 {
		t.Fatalf("err=%v created=%d", err, len(e.tasks.created))
	}
}
