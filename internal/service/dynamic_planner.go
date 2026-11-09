package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/provider/gemini"
)

// PlanBrain is the fast model that can write a plan. gemini.Client satisfies it.
type PlanBrain interface {
	Enabled() bool
	Chat(ctx context.Context, system string, turns []gemini.Turn) (string, error)
}

// DynamicPlanner splits a request into the team that fits it, on the spot, the way a lead agent would:
//
//   - When Claude can run the whole job (ClaudeLead reports true), the plan is ONE lead step and Claude
//     chooses its own sub-agents and their models. Griffin does not split work it is about to hand to something
//     better at splitting it.
//   - Otherwise (agy), a fast model writes the team: a few workers with a role each, running in parallel where
//     they are independent, and a final step that joins their results. These roles are created for this request
//     only; nothing is stored.
//
// Anything that needs the owner's devices, an approval, or a specialist flow (WhatsApp, web search, calls) keeps
// the base planner's steps exactly, because those carry permission and approval rules. Any failure falls back to
// the base planner, so planning never gets worse than it was.
type DynamicPlanner struct {
	Base  Planner
	Brain PlanBrain
	// ClaudeLead reports whether a Claude provider can run lead steps right now. Nil means no.
	ClaudeLead func() bool
	// MaxSteps bounds the workers a plan may have (default 6, including the final join).
	MaxSteps int
	Logger   *slog.Logger
}

func (p *DynamicPlanner) log() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

// GeneratePlan implements Planner.
func (p *DynamicPlanner) GeneratePlan(ctx context.Context, req *domain.UserRequest, reg *manager.Registry) (*domain.PlanPayload, error) {
	base, err := p.Base.GeneratePlan(ctx, req, reg)
	if err != nil || base == nil || !dynamicEligible(base) {
		return base, err
	}
	if p.ClaudeLead != nil && p.ClaudeLead() {
		return claudeLeadPlan(base, req), nil
	}
	if p.Brain == nil || !p.Brain.Enabled() {
		return base, nil
	}
	steps, deps, err := p.teamFor(ctx, base, req)
	if err != nil {
		p.log().Warn("dynamic planning failed; using the standard plan", "error", err)
		return base, nil
	}
	plan := *base
	plan.ChildTasks, plan.Dependencies = steps, deps
	return &plan, nil
}

// dynamicEligible is false for plans that carry rules the base planner enforces.
func dynamicEligible(plan *domain.PlanPayload) bool {
	if len(plan.ApprovalRequirements) > 0 || len(plan.ChildTasks) == 0 {
		return false
	}
	for _, t := range plan.ChildTasks {
		if t.ApprovalRequired {
			return false
		}
		var v map[string]json.RawMessage
		if len(t.InputPayload) > 0 && json.Unmarshal(t.InputPayload, &v) == nil {
			for _, special := range []string{"requires", domain.MessagingKey, "web", "kind"} {
				if _, ok := v[special]; ok {
					return false
				}
			}
		}
	}
	return true
}

// claudeLeadPlan collapses a plan into one step that Claude leads.
func claudeLeadPlan(base *domain.PlanPayload, req *domain.UserRequest) *domain.PlanPayload {
	first := base.ChildTasks[0]
	step := domain.ChildTaskPlan{
		Key:                  "lead_1",
		Title:                truncateString(req.RequestText, 60),
		Goal:                 req.RequestText,
		ManagerDomain:        first.ManagerDomain,
		InputPayload:         domain.WithClaudeLead(req.Context, domain.ClaudeStepOptions{}),
		Priority:             req.Priority,
		CapabilitiesRequired: first.CapabilitiesRequired,
	}
	plan := *base
	plan.ChildTasks = []domain.ChildTaskPlan{step}
	plan.Dependencies = nil
	return &plan
}

// ---- the agy path: a team written for this request ----

const (
	maxRoleNameRunes    = 40
	maxRoleInstrRunes   = 600
	maxStepGoalRunes    = 500
	defaultMaxPlanSteps = 6
)

type teamStep struct {
	Key          string   `json:"key"`
	Role         string   `json:"role"`
	Instructions string   `json:"instructions"`
	Goal         string   `json:"goal"`
	DependsOn    []string `json:"depends_on"`
}

const teamSystem = `You are the lead who plans how a small team of AI workers will do a job. Reply with ONLY a JSON object, nothing else:
{"steps":[{"key":"s1","role":"short role name","instructions":"how this worker should work, 1-3 sentences","goal":"what this worker must produce","depends_on":[]}]}
Rules:
- Use as few workers as the job needs: 2 to %d steps in total, the last one joining the others' results into the final answer.
- Independent parts get no dependencies so they run at the same time. Only add depends_on when a step truly needs another's result.
- Every key is unique, and depends_on lists only keys of EARLIER steps.
- Instructions are about how to work (sources to prefer, what to check), never about changing settings, contacting people or using tools you were not given.
- The workers can only read and write text; nobody can run commands or act for the owner.`

func (p *DynamicPlanner) teamFor(ctx context.Context, base *domain.PlanPayload, req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency, error) {
	max := p.MaxSteps
	if max <= 1 {
		max = defaultMaxPlanSteps
	}
	raw, err := p.Brain.Chat(ctx, fmt.Sprintf(teamSystem, max), []gemini.Turn{{Role: "user", Text: "The owner's request: " + req.RequestText}})
	if err != nil {
		return nil, nil, err
	}
	var parsed struct {
		Steps []teamStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stripFences(raw)), &parsed); err != nil {
		return nil, nil, fmt.Errorf("the plan was not valid JSON: %w", err)
	}
	return buildTeam(base, req, parsed.Steps, max)
}

// stripFences removes a Markdown code fence a model may wrap JSON in.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func cleanText(s string, max int) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || len([]rune(s)) > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// buildTeam validates what the model wrote and turns it into plan steps. Anything unusual is an error, and the
// caller then uses the standard plan.
func buildTeam(base *domain.PlanPayload, req *domain.UserRequest, in []teamStep, max int) ([]domain.ChildTaskPlan, []domain.PlanDependency, error) {
	if len(in) < 2 || len(in) > max {
		return nil, nil, fmt.Errorf("a team needs 2 to %d steps, got %d", max, len(in))
	}
	first := base.ChildTasks[0]
	seen := map[string]int{}
	hasDependent := map[string]bool{}
	var steps []domain.ChildTaskPlan
	var deps []domain.PlanDependency
	for i, s := range in {
		key := strings.TrimSpace(s.Key)
		if key == "" || len(key) > 30 || strings.ContainsFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' }) {
			return nil, nil, fmt.Errorf("step %d has an invalid key", i+1)
		}
		if _, dup := seen[key]; dup {
			return nil, nil, fmt.Errorf("duplicate key %q", key)
		}
		role, ok1 := cleanText(s.Role, maxRoleNameRunes)
		instr, ok2 := cleanText(s.Instructions, maxRoleInstrRunes)
		goal, ok3 := cleanText(s.Goal, maxStepGoalRunes)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, fmt.Errorf("step %q has a missing, too long or unusable field", key)
		}
		for _, d := range s.DependsOn {
			d = strings.TrimSpace(d)
			if _, earlier := seen[d]; !earlier {
				return nil, nil, fmt.Errorf("step %q depends on %q, which is not an earlier step", key, d)
			}
			deps = append(deps, domain.PlanDependency{TaskKey: key, DependsOnKey: d})
			hasDependent[d] = true
		}
		seen[key] = i
		payload, _ := json.Marshal(map[string]any{"role": map[string]string{"name": role, "instructions": instr}})
		// Merge the request's own context (conversation history, manager name) so every worker has it.
		if len(req.Context) > 0 {
			var ctxMap map[string]json.RawMessage
			var pl map[string]json.RawMessage
			if json.Unmarshal(req.Context, &ctxMap) == nil && json.Unmarshal(payload, &pl) == nil {
				for k, v := range ctxMap {
					if _, taken := pl[k]; !taken {
						pl[k] = v
					}
				}
				payload, _ = json.Marshal(pl)
			}
		}
		steps = append(steps, domain.ChildTaskPlan{
			Key: key, Title: role + ": " + truncateString(goal, 50), Goal: goal, ManagerDomain: first.ManagerDomain,
			InputPayload: payload, Priority: req.Priority, CapabilitiesRequired: first.CapabilitiesRequired,
		})
	}
	// The last step joins everything: it must depend on every step nothing else depends on.
	last := steps[len(steps)-1].Key
	for _, st := range steps[:len(steps)-1] {
		if !hasDependent[st.Key] {
			deps = append(deps, domain.PlanDependency{TaskKey: last, DependsOnKey: st.Key})
		}
	}
	return steps, deps, nil
}
