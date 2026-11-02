package service

import (
	"context"
	"encoding/json"
	"fmt"
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

	switch managerID {
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
		workerKey := ""
		if len(def.AvailableWorkers) > 0 {
			workerKey = def.AvailableWorkers[0]
		}
		needsApproval, risk := domain.RequiresApproval(def.AllowedCapabilities, "")
		actionType := ""
		if needsApproval && len(def.ApprovalOperations) > 0 {
			actionType = def.ApprovalOperations[0]
		}

		childTasks = []domain.ChildTaskPlan{
			{
				Key:                  fmt.Sprintf("%s_task_1", managerID),
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
			},
		}
		if needsApproval {
			approvalReqs = append(approvalReqs, domain.ApprovalRequirement{
				TaskKey:    fmt.Sprintf("%s_task_1", managerID),
				ActionType: actionType,
				RiskLevel:  risk,
				Summary:    fmt.Sprintf("Approval required for sensitive %s action", actionType),
			})
		}
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

func classifyIntent(text string) string {
	switch {
	case strings.Contains(text, "job") || strings.Contains(text, "resume") || strings.Contains(text, "interview") || strings.Contains(text, "recruiter") || strings.Contains(text, "career"):
		return "career"
	case strings.Contains(text, "code") || strings.Contains(text, "bug") || strings.Contains(text, "refactor") || strings.Contains(text, "test") || strings.Contains(text, "git") || strings.Contains(text, "repo"):
		return "engineering"
	case strings.Contains(text, "hackathon") || strings.Contains(text, "bounty") || strings.Contains(text, "grant"):
		return "hackathon"
	case strings.Contains(text, "freelance") || strings.Contains(text, "gig") || strings.Contains(text, "proposal") || strings.Contains(text, "contract"):
		return "freelance"
	case strings.Contains(text, "networking") || strings.Contains(text, "crm") || strings.Contains(text, "outreach") || strings.Contains(text, "meeting"):
		return "networking"
	case strings.Contains(text, "news") || strings.Contains(text, "ai release") || strings.Contains(text, "model") || strings.Contains(text, "benchmark"):
		return "tech_intelligence"
	case strings.Contains(text, "learn") || strings.Contains(text, "study") || strings.Contains(text, "curriculum") || strings.Contains(text, "quiz"):
		return "learning"
	case strings.Contains(text, "calendar") || strings.Contains(text, "agenda") || strings.Contains(text, "schedule") || strings.Contains(text, "travel"):
		return "personal_ops"
	case strings.Contains(text, "credit") || strings.Contains(text, "aws bill") || strings.Contains(text, "finance") || strings.Contains(text, "subscription"):
		return "finance"
	case strings.Contains(text, "prd") || strings.Contains(text, "feature") || strings.Contains(text, "idea") || strings.Contains(text, "product"):
		return "product_ideas"
	default:
		return "research"
	}
}
