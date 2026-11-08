package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
)

// PlanExecutionResult captures structured output from an AI execution provider.
type PlanExecutionResult struct {
	StructuredOutput []byte
	TextOutput       string
}

// PlanExecutor defines the minimal execution contract required by the AIPlanner.
type PlanExecutor interface {
	ExecuteTask(ctx context.Context, task *domain.Task) (*PlanExecutionResult, error)
	IsHealthy(ctx context.Context) bool
}

// RuleBasedPlanner performs deterministic intent classification and task decomposition.
type RuleBasedPlanner struct{}

// NewRuleBasedPlanner constructs a new RuleBasedPlanner.
func NewRuleBasedPlanner() *RuleBasedPlanner {
	return &RuleBasedPlanner{}
}

// GeneratePlan classifies the user request into one of the 11 specialist manager domains and produces child tasks.
func (p *RuleBasedPlanner) GeneratePlan(ctx context.Context, req *domain.UserRequest, mgrReg *manager.Registry) (*domain.PlanPayload, error) {
	text := strings.ToLower(req.RequestText)
	managerID := classifyIntent(text)
	planKey := managerID
	if wantsWhatsApp(text) {
		managerID, planKey = "personal_ops", "whatsapp"
	} else if wantsWeb(req.RequestText) {
		managerID, planKey = "research", "web"
	}

	mgr, ok := mgrReg.Get(managerID)
	if !ok {
		managerID = "research" // Default fallback
		mgr, _ = mgrReg.Get(managerID)
	}

	def := mgr.Definition()
	taskType := "GENERAL_OBJECTIVE"
	if len(def.SupportedTaskTypes) > 0 {
		taskType = def.SupportedTaskTypes[0]
	}

	// Multi-step decomposition for complex workflows
	var childTasks []domain.ChildTaskPlan
	var dependencies []domain.PlanDependency
	var approvalReqs []domain.ApprovalRequirement

	switch planKey {
	case "whatsapp":
		childTasks, dependencies = whatsAppSteps(req)

	case "web":
		childTasks, dependencies = webSteps(req)

	case "engineering":
		childTasks = []domain.ChildTaskPlan{
			{
				Key:                  "eng_inspect",
				Title:                "Inspect repository and diagnose problem",
				Goal:                 fmt.Sprintf("Analyze issue: %s", req.RequestText),
				ManagerDomain:        "engineering",
				WorkerKey:            "bug_diagnostician",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityRepoRead},
			},
			{
				Key:                  "eng_implement",
				Title:                "Implement code modifications and test suite",
				Goal:                 "Author changes and verify with automated tests",
				ManagerDomain:        "engineering",
				WorkerKey:            "code_reviewer",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityRepoWrite},
				ApprovalRequired:     true,
				ActionType:           domain.ActionTypeRepoModify,
				RiskLevel:            domain.ApprovalRiskMedium,
			},
		}
		dependencies = []domain.PlanDependency{
			{TaskKey: "eng_implement", DependsOnKey: "eng_inspect"},
		}
		approvalReqs = []domain.ApprovalRequirement{
			{
				TaskKey:    "eng_implement",
				ActionType: domain.ActionTypeRepoModify,
				RiskLevel:  domain.ApprovalRiskMedium,
				Summary:    "Approval required before modifying codebase files",
			},
		}

	case "career":
		requiresSend := strings.Contains(text, "send") || strings.Contains(text, "apply")
		childTasks = []domain.ChildTaskPlan{
			{
				Key:                  "career_research",
				Title:                "Scout role requirements and company details",
				Goal:                 fmt.Sprintf("Research role for: %s", req.RequestText),
				ManagerDomain:        "career",
				WorkerKey:            "career_researcher",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityResearchRead, domain.CapabilityWebRead},
			},
			{
				Key:                  "career_tailor",
				Title:                "Tailor resume and draft application materials",
				Goal:                 "Format customized resume and cover letter",
				ManagerDomain:        "career",
				WorkerKey:            "resume_tailorer",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityEmailDraft},
				ApprovalRequired:     requiresSend,
				ActionType:           domain.ActionTypeJobApply,
				RiskLevel:            domain.ApprovalRiskHigh,
			},
		}
		dependencies = []domain.PlanDependency{
			{TaskKey: "career_tailor", DependsOnKey: "career_research"},
		}
		if requiresSend {
			approvalReqs = []domain.ApprovalRequirement{
				{
					TaskKey:    "career_tailor",
					ActionType: domain.ActionTypeJobApply,
					RiskLevel:  domain.ApprovalRiskHigh,
					Summary:    "Approval required before submitting application",
				},
			}
		}

	case "research":
		if !wantsSourcedResearch(text) {
			// Fast path: an ask that does not call for sourced research is answered in one step.
			// Splitting it into "gather" then "synthesize" only doubled the wait (two cold agent runs).
			childTasks, approvalReqs = singleStep(managerID, def, req)
			break
		}
		childTasks = []domain.ChildTaskPlan{
			{
				Key:                  "res_gather",
				Title:                "Gather primary sources and documentation",
				Goal:                 fmt.Sprintf("Collect technical facts for: %s", req.RequestText),
				ManagerDomain:        "research",
				WorkerKey:            "web_researcher",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityResearchRead, domain.CapabilityWebRead},
			},
			{
				Key:                  "res_synthesize",
				Title:                "Synthesize markdown report and evidence table",
				Goal:                 "Structure findings with citations and comparison tables",
				ManagerDomain:        "research",
				WorkerKey:            "synthesis_writer",
				InputPayload:         req.Context,
				Priority:             req.Priority,
				CapabilitiesRequired: []string{domain.CapabilityResearchRead},
			},
		}
		dependencies = []domain.PlanDependency{
			{TaskKey: "res_synthesize", DependsOnKey: "res_gather"},
		}

	default:
		// Standard single-step execution for other managers
		childTasks, approvalReqs = singleStep(managerID, def, req)
	}

	plan := &domain.PlanPayload{
		Objective:            req.RequestText,
		InterpretedRequest:   fmt.Sprintf("Handled by %s: %s", def.Name, req.RequestText),
		ManagerID:            managerID,
		TaskType:             taskType,
		Priority:             req.Priority,
		ContextRequirements:  def.RequiredContext,
		ChildTasks:           childTasks,
		Dependencies:         dependencies,
		ExpectedOutputs:      def.ExpectedOutputFormats,
		VerificationCriteria: def.VerificationRules,
		ApprovalRequirements: approvalReqs,
	}

	return plan, nil
}

// AIPlanner leverages an AI PlanExecutor with JSON schemas for deep planning.
type AIPlanner struct {
	executor PlanExecutor
	fallback *RuleBasedPlanner
}

// NewAIPlanner constructs a new AIPlanner.
func NewAIPlanner(executor PlanExecutor) *AIPlanner {
	return &AIPlanner{
		executor: executor,
		fallback: NewRuleBasedPlanner(),
	}
}

// GeneratePlan calls the AI executor with a structured planning prompt and deserializes the output.
func (p *AIPlanner) GeneratePlan(ctx context.Context, req *domain.UserRequest, mgrReg *manager.Registry) (*domain.PlanPayload, error) {
	if p.executor == nil || !p.executor.IsHealthy(ctx) {
		return p.fallback.GeneratePlan(ctx, req, mgrReg)
	}

	prompt := fmt.Sprintf(
		"You are JJ, Chief of Staff and Top-Level Orchestrator of Griffin.\n"+
			"Decompose the following user request into a structured execution plan across the 11 specialist managers.\n\n"+
			"User Request: %s\n"+
			"Priority: %s\n\n"+
			"Output must adhere strictly to the JSON schema for PlanPayload.",
		req.RequestText, req.Priority,
	)

	task := &domain.Task{
		Title:        "JJ AI Plan Decomposition",
		Goal:         prompt,
		InputPayload: json.RawMessage(fmt.Sprintf(`{"prompt":%q}`, prompt)),
		Priority:     req.Priority,
	}

	result, err := p.executor.ExecuteTask(ctx, task)
	if err != nil {
		// Fallback gracefully on execution error
		return p.fallback.GeneratePlan(ctx, req, mgrReg)
	}

	// Attempt to deserialize plan from structured output
	var plan domain.PlanPayload
	if len(result.StructuredOutput) > 0 {
		if err := json.Unmarshal(result.StructuredOutput, &plan); err == nil && len(plan.ChildTasks) > 0 {
			return &plan, nil
		}
	}

	// If AI output was not valid plan, fall back to rule-based planner
	return p.fallback.GeneratePlan(ctx, req, mgrReg)
}

// intentRule routes a request to a manager when any of its keywords appears as a whole word.
// Matching whole words (with simple inflections) matters: substring matching routed "report"
// to engineering because it contains "repo", and "latest" because it contains "test".
type intentRule struct {
	manager string
	re      *regexp.Regexp
}

// intentRules are evaluated in order; the first match wins. Requests that match nothing go to
// the general research manager.
var intentRules = []intentRule{
	{"career", regexp.MustCompile(`\b(jobs?|resumes?|interview\w*|recruiters?|career\w*)\b`)},
	{"engineering", regexp.MustCompile(`\b(code|codes|coding|codebase|bugs?|bugfix\w*|debug\w*|refactor\w*|tests?|testing|git|github|repos?|repositor(y|ies))\b`)},
	{"hackathon", regexp.MustCompile(`\b(hackathons?|bount(y|ies)|grants?)\b`)},
	{"freelance", regexp.MustCompile(`\b(freelanc\w*|gigs?|proposals?|contracts?)\b`)},
	{"networking", regexp.MustCompile(`\b(networking|crm|outreach|meetings?)\b`)},
	{"tech_intelligence", regexp.MustCompile(`\b(news|ai releases?|models?|benchmarks?)\b`)},
	{"learning", regexp.MustCompile(`\b(learn\w*|stud(y|ies|ying)|curricul(um|a)|quiz(zes)?)\b`)},
	{"personal_ops", regexp.MustCompile(`\b(calendars?|agenda|schedul\w*|travel\w*)\b`)},
	{"finance", regexp.MustCompile(`\b(credits?|aws bills?|financ\w*|subscriptions?)\b`)},
	{"product_ideas", regexp.MustCompile(`\b(prds?|features?|ideas?|products?)\b`)},
}

func classifyIntent(text string) string {
	for _, rule := range intentRules {
		if rule.re.MatchString(text) {
			return rule.manager
		}
	}
	return "research"
}

// sourcedResearch matches requests that explicitly ask for research with sources, the only case where
// a separate "gather" step before a "synthesize" step is worth a second agent run.
var sourcedResearch = regexp.MustCompile(`\b(research|sources?|cite|citations?|evidence|literature|investigate|deep[- ]dive|look up|fact[- ]check|survey of|state of the art)\b`)

func wantsSourcedResearch(lowerText string) bool { return sourcedResearch.MatchString(lowerText) }

// singleStep is one child task that does the whole request, with an approval when the manager's
// capabilities require one.
func singleStep(managerID string, def manager.ManagerDefinition, req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.ApprovalRequirement) {
	workerKey := ""
	if len(def.AvailableWorkers) > 0 {
		workerKey = def.AvailableWorkers[0]
	}
	needsApproval, risk := domain.RequiresApproval(def.AllowedCapabilities, "")
	actionType := ""
	if needsApproval && len(def.ApprovalOperations) > 0 {
		actionType = def.ApprovalOperations[0]
	}
	key := fmt.Sprintf("%s_task_1", managerID)
	steps := []domain.ChildTaskPlan{{
		Key:                  key,
		Title:                truncateString(req.RequestText, 60),
		Goal:                 req.RequestText,
		ManagerDomain:        managerID,
		WorkerKey:            workerKey,
		InputPayload:         req.Context,
		Priority:             req.Priority,
		CapabilitiesRequired: def.AllowedCapabilities,
		ApprovalRequired:     needsApproval,
		ActionType:           actionType,
		RiskLevel:            risk,
	}}
	var approvals []domain.ApprovalRequirement
	if needsApproval {
		approvals = append(approvals, domain.ApprovalRequirement{
			TaskKey:    key,
			ActionType: actionType,
			RiskLevel:  risk,
			Summary:    fmt.Sprintf("Approval required for sensitive %s action", actionType),
		})
	}
	return steps, approvals
}
