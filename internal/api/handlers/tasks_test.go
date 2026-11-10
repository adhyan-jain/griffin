package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type mockTaskRepoForTasksTest struct {
	tasks map[uuid.UUID]*domain.Task
}

func (r *mockTaskRepoForTasksTest) CreateTask(_ context.Context, t *domain.Task) (*domain.Task, error) {
	c := *t
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if r.tasks == nil {
		r.tasks = map[uuid.UUID]*domain.Task{}
	}
	r.tasks[c.ID] = &c
	return &c, nil
}

func (r *mockTaskRepoForTasksTest) GetTaskByIDAndUser(_ context.Context, id, _ uuid.UUID) (*domain.Task, error) {
	if t, ok := r.tasks[id]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

func (r *mockTaskRepoForTasksTest) GetTaskByID(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	if t, ok := r.tasks[id]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

func (r *mockTaskRepoForTasksTest) UpdateTask(context.Context, *domain.Task) error { return nil }
func (r *mockTaskRepoForTasksTest) ClaimNextTask(context.Context, string, time.Duration) (*domain.Task, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) ClaimNextTaskForHost(context.Context, uuid.UUID, string, []string, time.Duration) (*domain.Task, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) ListTasksNeedingHosts(context.Context, uuid.UUID, int) ([]*domain.Task, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) RenewLease(context.Context, uuid.UUID, string, time.Duration) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) CompleteTask(context.Context, uuid.UUID, string, json.RawMessage) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) FailTask(context.Context, uuid.UUID, string, string, bool, time.Duration) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (r *mockTaskRepoForTasksTest) ResumeTask(context.Context, uuid.UUID) error         { return nil }
func (r *mockTaskRepoForTasksTest) DeleteTaskTree(context.Context, uuid.UUID) error     { return nil }
func (r *mockTaskRepoForTasksTest) ReapExpiredLeases(context.Context) (int64, error)    { return 0, nil }
func (r *mockTaskRepoForTasksTest) TransitionToApproval(context.Context, uuid.UUID, *domain.Approval) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) CountRunnableTasks(context.Context) (int64, error) {
	return 0, nil
}
func (r *mockTaskRepoForTasksTest) AddDependency(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) GetDependencies(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) CheckDependenciesCompleted(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}
func (r *mockTaskRepoForTasksTest) GetDependencyOutputs(context.Context, uuid.UUID) ([]repository.DependencyOutput, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) GetDependentTasks(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) GetTaskByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Task, error) {
	return nil, domain.ErrNotFound
}
func (r *mockTaskRepoForTasksTest) RecordEvent(context.Context, *domain.TaskEvent) error { return nil }
func (r *mockTaskRepoForTasksTest) RecordAttempt(context.Context, *domain.TaskAttempt) error {
	return nil
}
func (r *mockTaskRepoForTasksTest) GetAttempts(context.Context, uuid.UUID) ([]*domain.TaskAttempt, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) GetEvents(context.Context, uuid.UUID) ([]*domain.TaskEvent, error) {
	return nil, nil
}
func (r *mockTaskRepoForTasksTest) UnblockTaskIfEligible(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *mockTaskRepoForTasksTest) CascadeCancelDownstream(context.Context, uuid.UUID, string) ([]uuid.UUID, error) {
	return nil, nil
}

type mockObjectiveSubmitter struct {
	calls      int
	last       *domain.UserRequest
	orchRecord *domain.OrchestrationRecord
	plan       *domain.OrchestrationPlan
	err        error
}

func (m *mockObjectiveSubmitter) SubmitRequest(_ context.Context, req *domain.UserRequest) (*domain.OrchestrationRecord, *domain.OrchestrationPlan, error) {
	m.calls++
	m.last = req
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.orchRecord, m.plan, nil
}

func (m *mockObjectiveSubmitter) RunnableChildCount(context.Context, uuid.UUID) (int, error) {
	return 1, nil
}

func TestCreateTask_RoutesJJObjectiveToOrchestrator(t *testing.T) {
	userID := uuid.New()
	rootID := uuid.New()
	orchID := uuid.New()

	repo := &mockTaskRepoForTasksTest{
		tasks: map[uuid.UUID]*domain.Task{
			rootID: {
				ID:            rootID,
				UserID:        userID,
				ManagerDomain: "JJ",
				Title:         "JJ Orchestration: WhatsApp message",
				Goal:          "sned a whatsapp message to bhai saying hello",
				Status:        domain.TaskStatusRunning,
			},
		},
	}
	engine := service.NewTaskEngine(repo, nil, slog.Default())

	objectives := &mockObjectiveSubmitter{
		orchRecord: &domain.OrchestrationRecord{
			ID:         orchID,
			RootTaskID: rootID,
			UserID:     userID,
		},
		plan: &domain.OrchestrationPlan{ID: uuid.New()},
	}

	handler := NewTaskHandler(engine, repo, nil, false).WithObjectives(objectives)

	body := `{"manager_domain":"JJ","goal":"sned a whatsapp message to bhai saying hello","priority":"NORMAL_P2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	handler.CreateTask(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 1, objectives.calls)
	assert.Equal(t, "sned a whatsapp message to bhai saying hello", objectives.last.RequestText)

	var envelope struct {
		Success bool        `json:"success"`
		Data    domain.Task `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &envelope)
	require.NoError(t, err)
	assert.True(t, envelope.Success)
	assert.Equal(t, rootID, envelope.Data.ID)
	assert.Equal(t, "sned a whatsapp message to bhai saying hello", envelope.Data.Goal)
}
