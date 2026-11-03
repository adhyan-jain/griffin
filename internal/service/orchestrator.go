package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/repository"
)

// Planner defines the strategy for classifying intent and decomposing user requests into execution plans.
type Planner interface {
	GeneratePlan(ctx context.Context, req *domain.UserRequest, mgrReg *manager.Registry) (*domain.PlanPayload, error)
}

// JJOrchestrator coordinates end-to-end user request orchestration through specialist managers and workers.
type JJOrchestrator struct {
	engine   *TaskEngine
	orchRepo repository.OrchestrationRepository
	taskRepo repository.TaskRepository
	mgrReg   *manager.Registry
	factory  *AgentFactoryService
	planner  Planner
	logger   *slog.Logger
}

// NewJJOrchestrator constructs a new JJOrchestrator.
func NewJJOrchestrator(
	engine *TaskEngine,
	orchRepo repository.OrchestrationRepository,
	taskRepo repository.TaskRepository,
	mgrReg *manager.Registry,
	factory *AgentFactoryService,
	planner Planner,
	logger *slog.Logger,
) *JJOrchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	if planner == nil {
		planner = NewRuleBasedPlanner()
	}
	return &JJOrchestrator{
		engine:   engine,
		orchRepo: orchRepo,
		taskRepo: taskRepo,
		mgrReg:   mgrReg,
		factory:  factory,
		planner:  planner,
		logger:   logger,
	}
}

// SubmitRequest executes the REQUEST -> PLAN -> VALIDATE -> DELEGATE lifecycle.
func (o *JJOrchestrator) SubmitRequest(ctx context.Context, req *domain.UserRequest) (*domain.OrchestrationRecord, *domain.OrchestrationPlan, error) {
	if req == nil || strings.TrimSpace(req.RequestText) == "" {
		return nil, nil, fmt.Errorf("%w: request_text cannot be empty", domain.ErrValidation)
	}
	if err := domain.ValidatePriority(req.Priority); err != nil {
		return nil, nil, err
	}
	if req.Priority == "" {
		req.Priority = domain.TaskPriorityNormalP2
	}

	o.logger.Info("JJ received user request", "user_id", req.UserID, "text", req.RequestText)

	// 1. Create root task representing the overall orchestration in tasks table
	leasedBy := "JJ_ORCHESTRATOR"
	rootTask := &domain.Task{
		UserID:        req.UserID,
		WorkspaceID:   req.WorkspaceID,
		ManagerDomain: "JJ",
		Title:         fmt.Sprintf("JJ Orchestration: %s", truncateString(req.RequestText, 60)),
		Goal:          req.RequestText,
		InputPayload:  req.Context,
		Priority:      req.Priority,
		Status:        domain.TaskStatusRunning, // Root task is active while children execute
		LeasedBy:      &leasedBy,
	}
	createdRoot, err := o.engine.CreateTask(ctx, rootTask, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("jj: create root task: %w", err)
	}

	// 2. Persist initial Orchestration record in PLANNING status
	orchRecord := &domain.OrchestrationRecord{
		UserID:      req.UserID,
		WorkspaceID: req.WorkspaceID,
		RootTaskID:  createdRoot.ID,
		RequestText: req.RequestText,
		ManagerID:   "JJ", // Assigned to JJ during planning
		Status:      domain.OrchestrationStatusPlanning,
	}
	createdOrch, err := o.orchRepo.CreateOrchestration(ctx, orchRecord)
	if err != nil {
		return nil, nil, fmt.Errorf("jj: create orchestration record: %w", err)
	}

	// 3. PLAN: Generate structured plan
	planPayload, err := o.planner.GeneratePlan(ctx, req, o.mgrReg)
	if err != nil {
		o.failOrchestration(ctx, createdOrch.ID, createdRoot.ID, fmt.Sprintf("Planning failed: %v", err))
		return nil, nil, fmt.Errorf("jj: planning failed: %w", err)
	}

	// 4. VALIDATE: Validate structured plan against governance boundaries
	allowedManagers := make(map[string]bool)
	for _, m := range o.mgrReg.List() {
		allowedManagers[m.ID] = true
	}

	valErrors := domain.ValidatePlan(planPayload, allowedManagers)
	planStatus := domain.PlanStatusValidated
	if len(valErrors) > 0 {
		planStatus = domain.PlanStatusRejected
	}

	// Persist plan record
	planRecord := &domain.OrchestrationPlan{
		OrchestrationID:    createdOrch.ID,
		UserID:             req.UserID,
		Objective:          planPayload.Objective,
		InterpretedRequest: planPayload.InterpretedRequest,
		ManagerID:          planPayload.ManagerID,
		TaskType:           planPayload.TaskType,
		Priority:           string(planPayload.Priority),
		PlanPayload:        *planPayload,
		Status:             planStatus,
		ValidationErrors:   valErrors,
	}
	createdPlan, err := o.orchRepo.CreatePlan(ctx, planRecord)
	if err != nil {
		o.failOrchestration(ctx, createdOrch.ID, createdRoot.ID, fmt.Sprintf("Failed to persist plan: %v", err))
		return nil, nil, fmt.Errorf("jj: persist plan: %w", err)
	}

	if len(valErrors) > 0 {
		errMsg := fmt.Sprintf("Plan validation rejected: %s", strings.Join(valErrors, "; "))
		o.failOrchestration(ctx, createdOrch.ID, createdRoot.ID, errMsg)
		return createdOrch, createdPlan, fmt.Errorf("%w: %s", domain.ErrValidation, errMsg)
	}

	// Update manager assignment on orchestration
	createdOrch.ManagerID = planPayload.ManagerID

	// 5. DELEGATE: Break into child tasks and register in task engine
	delegationRecords, hasPendingApprovals, err := o.delegatePlan(ctx, createdOrch, createdRoot.ID, planPayload)
	if err != nil {
		o.failOrchestration(ctx, createdOrch.ID, createdRoot.ID, fmt.Sprintf("Delegation failed: %v", err))
		return nil, nil, fmt.Errorf("jj: delegation failed: %w", err)
	}

	// Update orchestration status based on delegations
	newStatus := domain.OrchestrationStatusRunning
	if hasPendingApprovals {
		newStatus = domain.OrchestrationStatusAwaitingApproval
	}
	_ = o.orchRepo.UpdateOrchestrationStatus(ctx, createdOrch.ID, newStatus, nil, nil)
	createdOrch.Status = newStatus

	// Mark plan as EXECUTED
	_ = o.orchRepo.UpdatePlanStatus(ctx, createdPlan.ID, domain.PlanStatusExecuted, nil)
	createdPlan.Status = domain.PlanStatusExecuted

	o.logger.Info("JJ successfully delegated request",
		"orchestration_id", createdOrch.ID,
		"manager_id", planPayload.ManagerID,
		"child_tasks", len(delegationRecords),
		"status", newStatus,
	)

	return createdOrch, createdPlan, nil
}

// ReconcileOrchestration executes the MONITOR -> COLLECT -> VERIFY -> RESPOND lifecycle.
func (o *JJOrchestrator) ReconcileOrchestration(ctx context.Context, orchestrationID uuid.UUID) (*domain.OrchestrationRecord, error) {
	orch, err := o.orchRepo.GetOrchestrationByID(ctx, orchestrationID, uuid.Nil)
	if err != nil {
		return nil, fmt.Errorf("jj: get orchestration: %w", err)
	}

	if orch.Status == domain.OrchestrationStatusCompleted ||
		orch.Status == domain.OrchestrationStatusFailed ||
		orch.Status == domain.OrchestrationStatusCancelled {
		return orch, nil // Already in terminal state
	}

	delegations, err := o.orchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, uuid.Nil)
	if err != nil {
		return nil, fmt.Errorf("jj: get delegations: %w", err)
	}

	allCompleted := true
	hasFailed := false
	hasAwaitingApproval := false
	var failureReason string
	var completedOutputs []string

	mgr, _ := o.mgrReg.Get(orch.ManagerID)

	for _, del := range delegations {
		task, err := o.taskRepo.GetTaskByID(ctx, del.ChildTaskID)
		if err != nil {
			continue
		}

		// Sync delegation record status
		if del.Status != task.Status {
			del.Status = task.Status
			var summary *string
			if len(task.OutputPayload) > 0 {
				s := string(task.OutputPayload)
				summary = &s
			}
			_ = o.orchRepo.UpdateDelegationStatus(ctx, del.ChildTaskID, task.Status, summary)
		}

		switch task.Status {
		case domain.TaskStatusCompleted:
			if mgr != nil && len(task.OutputPayload) > 0 {
				// VERIFY child deliverable
				verified, verifyMsg, _ := mgr.VerifyDeliverable(ctx, task, task.OutputPayload)
				if !verified {
					o.logger.Warn("Child task deliverable failed domain verification",
						"child_task_id", task.ID, "msg", verifyMsg)
				}
			}
			completedOutputs = append(completedOutputs, fmt.Sprintf("- [%s]: %s", task.Title, string(task.OutputPayload)))

		case domain.TaskStatusFailed, domain.TaskStatusCancelled:
			allCompleted = false
			hasFailed = true
			if task.FailureReason != nil {
				failureReason = *task.FailureReason
			} else {
				failureReason = "Child task execution failed"
			}

		case domain.TaskStatusAwaitingApproval:
			allCompleted = false
			hasAwaitingApproval = true

		default:
			allCompleted = false
		}
	}

	if hasFailed {
		errMsg := fmt.Sprintf("Orchestration aborted due to child task failure: %s", failureReason)
		_ = o.orchRepo.UpdateOrchestrationStatus(ctx, orch.ID, domain.OrchestrationStatusFailed, nil, &errMsg)
		if err := o.engine.FailTask(ctx, orch.RootTaskID, "JJ_ORCHESTRATOR", errMsg, false); err != nil {
			o.logger.Warn("Failed to fail root task", "root_id", orch.RootTaskID, "err", err)
		}
		orch.Status = domain.OrchestrationStatusFailed
		orch.ErrorMessage = &errMsg
		return orch, nil
	}

	if hasAwaitingApproval {
		_ = o.orchRepo.UpdateOrchestrationStatus(ctx, orch.ID, domain.OrchestrationStatusAwaitingApproval, nil, nil)
		orch.Status = domain.OrchestrationStatusAwaitingApproval
		return orch, nil
	}

	if allCompleted && len(delegations) > 0 {
		// RESPOND: Synthesize final response
		finalResp := fmt.Sprintf("Objective achieved successfully by Manager %s.\n\nSummary of Deliverables:\n%s",
			orch.ManagerID, strings.Join(completedOutputs, "\n"))

		_ = o.orchRepo.UpdateOrchestrationStatus(ctx, orch.ID, domain.OrchestrationStatusCompleted, &finalResp, nil)
		if err := o.engine.CompleteTask(ctx, orch.RootTaskID, "JJ_ORCHESTRATOR", json.RawMessage(fmt.Sprintf(`{"summary":%q}`, finalResp))); err != nil {
			o.logger.Warn("Failed to complete root task", "root_id", orch.RootTaskID, "err", err)
		}

		orch.Status = domain.OrchestrationStatusCompleted
		orch.FinalResponse = &finalResp
		o.logger.Info("JJ completed orchestration", "orchestration_id", orch.ID)
		return orch, nil
	}

	// Still in flight
	_ = o.orchRepo.UpdateOrchestrationStatus(ctx, orch.ID, domain.OrchestrationStatusRunning, nil, nil)
	orch.Status = domain.OrchestrationStatusRunning
	return orch, nil
}

// ReconcileInFlight recovers and reconciles all active orchestrations across system restarts.
func (o *JJOrchestrator) ReconcileInFlight(ctx context.Context) error {
	active, err := o.orchRepo.ListActiveOrchestrations(ctx)
	if err != nil {
		return fmt.Errorf("jj: list active orchestrations: %w", err)
	}

	o.logger.Info("Reconciling active in-flight orchestrations on startup", "count", len(active))
	for _, orch := range active {
		if _, err := o.ReconcileOrchestration(ctx, orch.ID); err != nil {
			o.logger.Error("Failed to reconcile active orchestration", "id", orch.ID, "err", err)
		}
	}
	return nil
}

// GetOrchestration retrieves orchestration details and child delegation records.
func (o *JJOrchestrator) GetOrchestration(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.OrchestrationRecord, []*domain.DelegationRecord, error) {
	orch, err := o.orchRepo.GetOrchestrationByID(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	dels, err := o.orchRepo.GetDelegationsByOrchestrationID(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	return orch, dels, nil
}

// RunnableChildCount returns how many child tasks of an orchestration are currently QUEUED
// (i.e. a runner must pick them up).
func (o *JJOrchestrator) RunnableChildCount(ctx context.Context, orchID uuid.UUID) (int, error) {
	dels, err := o.orchRepo.GetDelegationsByOrchestrationID(ctx, orchID, uuid.Nil)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range dels {
		task, err := o.taskRepo.GetTaskByID(ctx, d.ChildTaskID)
		if err != nil {
			return 0, err
		}
		if task.Status == domain.TaskStatusQueued {
			n++
		}
	}
	return n, nil
}

// GetPlan retrieves an execution plan by ID.
func (o *JJOrchestrator) GetPlan(ctx context.Context, planID uuid.UUID, userID uuid.UUID) (*domain.OrchestrationPlan, error) {
	return o.orchRepo.GetPlanByID(ctx, planID, userID)
}

func (o *JJOrchestrator) delegatePlan(
	ctx context.Context,
	orch *domain.OrchestrationRecord,
	rootTaskID uuid.UUID,
	plan *domain.PlanPayload,
) ([]*domain.DelegationRecord, bool, error) {
	hasPendingApproval := false
	taskKeyToID := make(map[string]uuid.UUID)
	var createdTasks []*domain.Task
	var taskPlanByKey = make(map[string]domain.ChildTaskPlan)

	// Step 1: Pre-generate IDs for all child tasks
	for _, ct := range plan.ChildTasks {
		taskID := uuid.New()
		taskKeyToID[ct.Key] = taskID
		taskPlanByKey[ct.Key] = ct
	}

	// Step 2: Build dependency map: childKey -> []prerequisiteUUID
	depMap := make(map[string][]uuid.UUID)
	for _, dep := range plan.Dependencies {
		prereqID := taskKeyToID[dep.DependsOnKey]
		depMap[dep.TaskKey] = append(depMap[dep.TaskKey], prereqID)
	}

	// Step 3: Instantiate and persist child tasks through TaskEngine
	var delegationRecords []*domain.DelegationRecord

	for _, ct := range plan.ChildTasks {
		assignedID := taskKeyToID[ct.Key]

		var task *domain.Task
		// Check if specialist worker is specified and exists in AgentFactory
		if ct.WorkerKey != "" && o.factory != nil {
			def, err := o.factory.GetWorkerDefinitionByKey(ctx, orch.UserID, ct.WorkerKey)
			if err == nil && def != nil && def.IsActive {
				task, err = o.factory.InstantiateTask(ctx, def, &rootTaskID, &rootTaskID, ct.Title, ct.Goal, ct.InputPayload, ct.Priority)
			}
		}

		if task == nil {
			task = &domain.Task{
				UserID:        orch.UserID,
				WorkspaceID:   orch.WorkspaceID,
				ParentID:      &rootTaskID,
				RootID:        &rootTaskID,
				ManagerDomain: ct.ManagerDomain,
				Title:         ct.Title,
				Goal:          ct.Goal,
				InputPayload:  ct.InputPayload,
				Priority:      ct.Priority,
			}
		}

		task.ID = assignedID
		prereqUUIDs := depMap[ct.Key]

		createdTask, err := o.engine.CreateTask(ctx, task, prereqUUIDs)
		if err != nil {
			return nil, false, fmt.Errorf("create child task '%s': %w", ct.Key, err)
		}
		createdTasks = append(createdTasks, createdTask)

		// Check if human approval is required
		if ct.ApprovalRequired {
			hasPendingApproval = true
			actionPayload := ct.InputPayload
			if len(actionPayload) == 0 {
				actionPayload = json.RawMessage("{}")
			}
			approval := &domain.Approval{
				UserID:        orch.UserID,
				TaskID:        createdTask.ID,
				ActionType:    ct.ActionType,
				RiskLevel:     ct.RiskLevel,
				Summary:       fmt.Sprintf("Approval required for '%s': %s", ct.Title, ct.Goal),
				ActionPayload: actionPayload,
			}
			if err := o.engine.RequestApproval(ctx, createdTask.ID, approval); err != nil {
				return nil, false, fmt.Errorf("request approval for child task '%s': %w", ct.Key, err)
			}
		}

		delRecord := &domain.DelegationRecord{
			OrchestrationID: orch.ID,
			UserID:          orch.UserID,
			ParentTaskID:    rootTaskID,
			ChildTaskID:     createdTask.ID,
			TaskKey:         ct.Key,
			ManagerID:       plan.ManagerID,
			WorkerKey:       ct.WorkerKey,
			Status:          createdTask.Status,
		}
		delegationRecords = append(delegationRecords, delRecord)
	}

	// Persist delegation records
	if err := o.orchRepo.CreateDelegations(ctx, delegationRecords); err != nil {
		return nil, false, fmt.Errorf("create delegations: %w", err)
	}

	return delegationRecords, hasPendingApproval, nil
}

func (o *JJOrchestrator) failOrchestration(ctx context.Context, orchID uuid.UUID, rootTaskID uuid.UUID, reason string) {
	_ = o.orchRepo.UpdateOrchestrationStatus(ctx, orchID, domain.OrchestrationStatusFailed, nil, &reason)
	_ = o.engine.FailTask(ctx, rootTaskID, "JJ_ORCHESTRATOR", reason, false)
}

func truncateString(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
