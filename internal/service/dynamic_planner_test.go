package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/provider/gemini"
)

type stubBase struct{ plan *domain.PlanPayload }

func (s stubBase) GeneratePlan(context.Context, *domain.UserRequest, *manager.Registry) (*domain.PlanPayload, error) {
	cp := *s.plan
	return &cp, nil
}

type stubBrain struct {
	on    bool
	reply string
	err   error
	calls int
	asked string
}

func (b *stubBrain) Enabled() bool { return b.on }
func (b *stubBrain) Chat(_ context.Context, _ string, turns []gemini.Turn) (string, error) {
	b.calls++
	b.asked = turns[len(turns)-1].Text
	return b.reply, b.err
}

func basePlan(payload string, approvals bool) *domain.PlanPayload {
	p := &domain.PlanPayload{ManagerID: "research", ChildTasks: []domain.ChildTaskPlan{{
		Key: "research_task_1", Title: "t", Goal: "g", ManagerDomain: "research", CapabilitiesRequired: []string{"WEB_SEARCH"},
		InputPayload: json.RawMessage(payload),
	}}}
	if approvals {
		p.ApprovalRequirements = []domain.ApprovalRequirement{{TaskKey: "research_task_1"}}
	}
	return p
}

var req = &domain.UserRequest{RequestText: "Compare the five best laptops under 60000 and recommend one", Priority: domain.TaskPriorityNormalP2,
	Context: json.RawMessage(`{"conversation_id":"c1","manager_name":"Jarvis"}`)}

const goodTeam = `{"steps":[
 {"key":"s1","role":"Spec finder","instructions":"Prefer manufacturer pages.","goal":"Collect specs for five laptops under 60000","depends_on":[]},
 {"key":"s2","role":"Review reader","instructions":"Prefer independent reviews.","goal":"Collect review verdicts for the same laptops","depends_on":[]},
 {"key":"s3","role":"Recommender","instructions":"Compare and pick one.","goal":"Recommend one laptop with reasons","depends_on":["s1","s2"]}]}`

func TestDynamicTeamIsBuiltOnTheSpotWithParallelWorkersAndAJoin(t *testing.T) {
	brain := &stubBrain{on: true, reply: "```json\n" + goodTeam + "\n```"}
	p := &DynamicPlanner{Base: stubBase{basePlan(`{}`, false)}, Brain: brain}
	plan, err := p.GeneratePlan(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChildTasks) != 3 || plan.ManagerID != "research" {
		t.Fatalf("want the three-worker team under the same manager: %+v", plan.ChildTasks)
	}
	if !strings.Contains(brain.asked, "Compare the five best laptops") {
		t.Errorf("the model must see the request: %q", brain.asked)
	}
	roles := map[string]string{}
	for _, s := range plan.ChildTasks {
		var pl struct {
			Role struct{ Name, Instructions string } `json:"role"`
			Mgr  string                              `json:"manager_name"`
			Conv string                              `json:"conversation_id"`
		}
		if err := json.Unmarshal(s.InputPayload, &pl); err != nil {
			t.Fatal(err)
		}
		roles[s.Key] = pl.Role.Name
		if pl.Mgr != "Jarvis" || pl.Conv != "c1" {
			t.Errorf("each worker must inherit the request's context (name, conversation): %s", s.InputPayload)
		}
		if s.ManagerDomain != "research" || len(s.CapabilitiesRequired) != 1 {
			t.Errorf("workers keep the manager and capabilities of the standard plan: %+v", s)
		}
	}
	if roles["s1"] != "Spec finder" || roles["s3"] != "Recommender" {
		t.Errorf("roles: %v", roles)
	}
	got := map[string]bool{}
	for _, d := range plan.Dependencies {
		got[d.TaskKey+"<-"+d.DependsOnKey] = true
	}
	if !got["s3<-s1"] || !got["s3<-s2"] || len(got) != 2 {
		t.Errorf("s1 and s2 run in parallel and s3 joins them: %v", got)
	}
}

func TestTheLastStepAlwaysJoinsEveryLooseEnd(t *testing.T) {
	team := `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g1","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g2","depends_on":[]},{"key":"c","role":"C","instructions":"x","goal":"g3","depends_on":[]}]}`
	plan, _ := (&DynamicPlanner{Base: stubBase{basePlan(`{}`, false)}, Brain: &stubBrain{on: true, reply: team}}).GeneratePlan(context.Background(), req, nil)
	joined := 0
	for _, d := range plan.Dependencies {
		if d.TaskKey == "c" {
			joined++
		}
	}
	if len(plan.ChildTasks) != 3 || joined != 2 {
		t.Fatalf("the final step must depend on the steps nothing else uses, got %+v", plan.Dependencies)
	}
}

func TestBadTeamsFallBackToTheStandardPlan(t *testing.T) {
	long := strings.Repeat("x", 700)
	bad := map[string]string{
		"not json":              "here is my plan: ...",
		"one step":              `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g","depends_on":[]}]}`,
		"seven steps":           `{"steps":[` + strings.TrimSuffix(strings.Repeat(`{"key":"k","role":"A","instructions":"x","goal":"g","depends_on":[]},`, 7), ",") + `]}`,
		"duplicate keys":        `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g","depends_on":[]},{"key":"a","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"forward dependency":    `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g","depends_on":["b"]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"unknown dependency":    `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":["zzz"]}]}`,
		"self dependency":       `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"g","depends_on":["a"]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"bad key":               `{"steps":[{"key":"a b;","role":"A","instructions":"x","goal":"g","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"too long instructions": `{"steps":[{"key":"a","role":"A","instructions":"` + long + `","goal":"g","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"control characters":    `{"steps":[{"key":"a","role":"A\u0000","instructions":"x","goal":"g","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
		"empty goal":            `{"steps":[{"key":"a","role":"A","instructions":"x","goal":"","depends_on":[]},{"key":"b","role":"B","instructions":"x","goal":"g","depends_on":[]}]}`,
	}
	base := basePlan(`{}`, false)
	for name, reply := range bad {
		plan, err := (&DynamicPlanner{Base: stubBase{base}, Brain: &stubBrain{on: true, reply: reply}}).GeneratePlan(context.Background(), req, nil)
		if err != nil || len(plan.ChildTasks) != 1 || plan.ChildTasks[0].Key != "research_task_1" {
			t.Errorf("%s: must fall back to the standard plan, got %+v %v", name, plan.ChildTasks, err)
		}
	}
	plan, _ := (&DynamicPlanner{Base: stubBase{base}, Brain: &stubBrain{on: true, err: errors.New("quota")}}).GeneratePlan(context.Background(), req, nil)
	if len(plan.ChildTasks) != 1 {
		t.Error("a brain error must fall back to the standard plan")
	}
	plan, _ = (&DynamicPlanner{Base: stubBase{base}, Brain: &stubBrain{on: false}}).GeneratePlan(context.Background(), req, nil)
	if len(plan.ChildTasks) != 1 {
		t.Error("no key, no dynamic plan")
	}
}

func TestPlansWithRulesAreNeverRewritten(t *testing.T) {
	brain := &stubBrain{on: true, reply: goodTeam}
	for name, base := range map[string]*domain.PlanPayload{
		"needs approval": basePlan(`{}`, true),
		"needs a device": basePlan(`{"requires":["messaging"]}`, false),
		"a web step":     basePlan(`{"web":{"op":"search","query":"x"}}`, false),
		"messaging step": basePlan(`{"messaging":{"op":"unread"}}`, false),
	} {
		plan, _ := (&DynamicPlanner{Base: stubBase{base}, Brain: brain, ClaudeLead: func() bool { return true }}).GeneratePlan(context.Background(), req, nil)
		if len(plan.ChildTasks) != 1 || plan.ChildTasks[0].Key != "research_task_1" {
			t.Errorf("%s: the standard plan carries permission and approval rules and must be kept: %+v", name, plan.ChildTasks)
		}
	}
	if brain.calls != 0 {
		t.Errorf("no model call is wasted on plans that cannot change, made %d", brain.calls)
	}
}

func TestWhenClaudeCanLeadTheWholeJobIsOneLeadStep(t *testing.T) {
	brain := &stubBrain{on: true, reply: goodTeam}
	base := basePlan(`{}`, false)
	base.ChildTasks = append(base.ChildTasks, domain.ChildTaskPlan{Key: "research_task_2", Goal: "second", ManagerDomain: "research"})
	base.Dependencies = []domain.PlanDependency{{TaskKey: "research_task_2", DependsOnKey: "research_task_1"}}
	plan, err := (&DynamicPlanner{Base: stubBase{base}, Brain: brain, ClaudeLead: func() bool { return true }}).GeneratePlan(context.Background(), req, nil)
	if err != nil || len(plan.ChildTasks) != 1 || len(plan.Dependencies) != 0 {
		t.Fatalf("one lead step, no Griffin-made workers: %+v %v", plan, err)
	}
	step := plan.ChildTasks[0]
	opts := domain.ParseClaudeOptions(step.InputPayload)
	if !opts.Lead || domain.ProviderHintFor(&domain.Task{InputPayload: step.InputPayload}) != domain.ProviderClaude {
		t.Errorf("the step must be a Claude lead step: %s", step.InputPayload)
	}
	if step.Goal != req.RequestText || domain.ManagerNameFrom(step.InputPayload) != "Jarvis" {
		t.Errorf("the lead gets the whole request and its context: %+v", step)
	}
	if brain.calls != 0 {
		t.Error("Claude splits the work itself, so the fast model is not asked")
	}
	// and when Claude is not available, the agy team is written instead
	plan, _ = (&DynamicPlanner{Base: stubBase{base}, Brain: &stubBrain{on: true, reply: goodTeam}, ClaudeLead: func() bool { return false }}).GeneratePlan(context.Background(), req, nil)
	if len(plan.ChildTasks) != 3 {
		t.Errorf("without Claude the on-the-spot team is used: %d steps", len(plan.ChildTasks))
	}
}
