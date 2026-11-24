package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/research"
)

type mockResearchStore struct {
	mu       sync.RWMutex
	runs     map[uuid.UUID]*domain.ResearchRun
	steps    []*domain.ResearchStep
	sources  []*domain.ResearchSource
	evidence []*domain.EvidenceItem

	lastListLimit int
}

func newMockResearchStore() *mockResearchStore {
	return &mockResearchStore{
		runs: make(map[uuid.UUID]*domain.ResearchRun),
	}
}

func (m *mockResearchStore) CreateRun(_ context.Context, r *domain.ResearchRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	m.runs[r.ID] = r
	return nil
}

func (m *mockResearchStore) GetRun(_ context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[runID]
	if !ok || r.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return r, nil
}

func (m *mockResearchStore) UpdateRun(_ context.Context, r *domain.ResearchRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = r
	return nil
}

func (m *mockResearchStore) TransitionRun(_ context.Context, userID, runID uuid.UUID, to domain.ResearchPhase,
	from []domain.ResearchPhase, stopReason string, errMsg *string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok || r.UserID != userID {
		return false, nil
	}
	for _, f := range from {
		if f == r.Status {
			r.Status = to
			if stopReason != "" {
				r.StopReason = stopReason
			}
			if errMsg != nil {
				r.ErrorMessage = errMsg
			}
			return true, nil
		}
	}
	return false, nil
}

func (m *mockResearchStore) SetEvidenceStatus(_ context.Context, _, _ uuid.UUID, _ domain.EvidenceStatus) error {
	return nil
}

func (m *mockResearchStore) ListRuns(_ context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastListLimit = limit
	var list []*domain.ResearchRun
	for _, r := range m.runs {
		if r.UserID == userID {
			list = append(list, r)
			if len(list) >= limit {
				break
			}
		}
	}
	return list, nil
}

func (m *mockResearchStore) RecordStep(_ context.Context, s *domain.ResearchStep) error {
	m.steps = append(m.steps, s)
	return nil
}

func (m *mockResearchStore) ListSteps(_ context.Context, userID, runID uuid.UUID) ([]*domain.ResearchStep, error) {
	var list []*domain.ResearchStep
	for _, s := range m.steps {
		if s.UserID == userID && s.RunID == runID {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockResearchStore) AddSource(_ context.Context, s *domain.ResearchSource) error {
	m.sources = append(m.sources, s)
	return nil
}

func (m *mockResearchStore) AddEvidence(_ context.Context, e *domain.EvidenceItem) error {
	m.evidence = append(m.evidence, e)
	return nil
}

func (m *mockResearchStore) ListEvidenceForRun(_ context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error) {
	var list []*domain.EvidenceItem
	for _, e := range m.evidence {
		if e.UserID == userID && e.RunID != nil && *e.RunID == runID {
			list = append(list, e)
		}
	}
	return list, nil
}

func withUserContext(r *http.Request, u uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, u))
}

// scheduled records the tasks the handler asked to have created, instead of a task engine.
type scheduled struct {
	mu    sync.Mutex
	tasks []*domain.Task
	err   error
}

func (s *scheduled) create(_ context.Context, t *domain.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.tasks = append(s.tasks, t)
	return nil
}

func newResearchHandler(t *testing.T) (*ResearchHandler, *mockResearchStore, *scheduled, uuid.UUID) {
	t.Helper()
	store := newMockResearchStore()
	sched := &scheduled{}
	engine := research.NewEngine(store, nil, nil, nil, nil)
	return NewResearchHandler(engine, store).WithScheduler(sched.create), store, sched, uuid.New()
}

func postResearch(h *ResearchHandler, user uuid.UUID, body any) *httptest.ResponseRecorder {
	var raw []byte
	switch b := body.(type) {
	case []byte:
		raw = b
	default:
		raw, _ = json.Marshal(b)
	}
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/research", bytes.NewReader(raw)), user)
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

func runAction(fn http.HandlerFunc, user, id uuid.UUID, verb string) *httptest.ResponseRecorder {
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/research/"+id.String()+"/"+verb, nil), user)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func TestResearchHandlerLifecycle(t *testing.T) {
	h, store, sched, userID := newResearchHandler(t)

	// 1. Create run via POST: the run is saved, a task is scheduled, and nothing runs in the request.
	w := postResearch(h, userID, domain.ResearchRequest{Objective: "Investigate AI compiler frameworks", Depth: domain.DepthStandard})
	require.Equal(t, http.StatusAccepted, w.Code)

	var createResp struct {
		Success bool                `json:"success"`
		Data    *domain.ResearchRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	require.True(t, createResp.Success)
	runID := createResp.Data.ID
	require.Equal(t, domain.PhaseRequestReceived, createResp.Data.Status)

	require.Len(t, sched.tasks, 1)
	task := sched.tasks[0]
	require.Equal(t, userID, task.UserID)
	require.Equal(t, "RESEARCH RUN "+runID.String(), task.Title)
	id, ok := domain.ResearchRunIDOf(task.Title)
	require.True(t, ok)
	require.Equal(t, runID, id)
	require.NotNil(t, task.IdempotencyKey)
	require.Empty(t, store.steps, "no step ran inside the request")

	// 2. List runs
	listReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research", nil), userID)
	listW := httptest.NewRecorder()
	h.List(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code)

	// 3. Get run by ID
	getReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research/"+runID.String(), nil), userID)
	getReq.SetPathValue("id", runID.String())
	getW := httptest.NewRecorder()
	h.Get(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	// 4. Pause an active run
	activeRun := &domain.ResearchRun{ID: uuid.New(), UserID: userID, Objective: "Active research task", Status: domain.PhaseInformationRetrieval, NextStepIndex: 4}
	require.NoError(t, store.CreateRun(context.Background(), activeRun))
	require.Equal(t, http.StatusOK, runAction(h.Pause, userID, activeRun.ID, "pause").Code)
	require.Equal(t, domain.PhasePaused, store.runs[activeRun.ID].Status)

	// 5. Resume it: the status goes back and a task is scheduled; the cycle does not run in the request
	before := len(sched.tasks)
	require.Equal(t, http.StatusOK, runAction(h.Resume, userID, activeRun.ID, "resume").Code)
	require.True(t, isActiveResearch(store.runs[activeRun.ID].Status))
	require.Len(t, sched.tasks, before+1)
	require.Equal(t, "RESEARCH RUN "+activeRun.ID.String(), sched.tasks[before].Title)
	require.Empty(t, store.steps)

	// 6. Cancel it
	require.Equal(t, http.StatusOK, runAction(h.Cancel, userID, activeRun.ID, "cancel").Code)
	require.Equal(t, domain.PhaseCancelled, store.runs[activeRun.ID].Status)
	require.Equal(t, domain.StopCancelled, store.runs[activeRun.ID].StopReason)
	require.Equal(t, http.StatusConflict, runAction(h.Pause, userID, activeRun.ID, "pause").Code, "a cancelled run cannot be paused")

	// 7. Evidence
	evReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research/"+runID.String()+"/evidence", nil), userID)
	evReq.SetPathValue("id", runID.String())
	evW := httptest.NewRecorder()
	h.Evidence(evW, evReq)
	require.Equal(t, http.StatusOK, evW.Code)
}

func TestResearchRunsAreScopedToTheirOwner(t *testing.T) {
	h, store, _, owner := newResearchHandler(t)
	run := &domain.ResearchRun{ID: uuid.New(), UserID: owner, Objective: "mine", Status: domain.PhasePlanning}
	require.NoError(t, store.CreateRun(context.Background(), run))
	stranger := uuid.New()
	for name, fn := range map[string]http.HandlerFunc{"pause": h.Pause, "cancel": h.Cancel, "resume": h.Resume} {
		require.Equal(t, http.StatusNotFound, runAction(fn, stranger, run.ID, name).Code, name)
	}
	require.Equal(t, domain.PhasePlanning, store.runs[run.ID].Status)
}

func TestResearchCreateValidation(t *testing.T) {
	h, _, sched, user := newResearchHandler(t)
	long := func(n int) string { return strings.Repeat("x", n) }
	bad := map[string]any{
		"empty objective":     domain.ResearchRequest{Objective: "   "},
		"long objective":      domain.ResearchRequest{Objective: long(domain.MaxResearchObjectiveRunes + 1)},
		"unknown depth":       domain.ResearchRequest{Objective: "x", Depth: "bottomless"},
		"unknown playbook":    domain.ResearchRequest{Objective: "x", Playbook: "../../etc"},
		"too many questions":  domain.ResearchRequest{Objective: "x", ResearchQuestions: make([]string, domain.MaxResearchListItems+1)},
		"long question":       domain.ResearchRequest{Objective: "x", ResearchQuestions: []string{long(domain.MaxResearchItemRunes + 1)}},
		"too many constraint": domain.ResearchRequest{Objective: "x", Constraints: make([]string, domain.MaxResearchListItems+1)},
		"long constraint":     domain.ResearchRequest{Objective: "x", Constraints: []string{long(domain.MaxResearchItemRunes + 1)}},
		"big context":         domain.ResearchRequest{Objective: "x", Context: map[string]any{"k": long(domain.MaxResearchContextBytes)}},
		"negative budget":     domain.ResearchRequest{Objective: "x", MaxPages: -3},
		"not json":            []byte("{nope"),
	}
	for name, body := range bad {
		w := postResearch(h, user, body)
		require.Equal(t, http.StatusBadRequest, w.Code, name)
	}
	require.Empty(t, sched.tasks, "a rejected request schedules nothing")

	// a body over 64KB is refused before it is parsed
	w := postResearch(h, user, []byte(`{"objective":"`+long(70<<10)+`"}`))
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestResearchCreateClampsBudgetsAndAcceptsEveryPlaybook(t *testing.T) {
	h, store, _, user := newResearchHandler(t)
	w := postResearch(h, user, domain.ResearchRequest{Objective: "x", Depth: domain.DepthQuick, MaxSearches: 100000, MaxPages: 100000, MaxModelCalls: 100000, MaxTimeSeconds: 100000})
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct{ Data *domain.ResearchRun }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for k, ceil := range domain.BudgetMax() {
		require.Equal(t, ceil, store.runs[resp.Data.ID].BudgetConfig[k], k)
	}
	for _, p := range domain.ResearchPlaybooks {
		require.Equal(t, http.StatusAccepted, postResearch(h, user, domain.ResearchRequest{Objective: "x", Playbook: p}).Code, p)
	}
}

func TestResearchCreateFailsTheRunWhenItCannotBeScheduled(t *testing.T) {
	h, store, sched, user := newResearchHandler(t)
	sched.err = errors.New("queue down")
	w := postResearch(h, user, domain.ResearchRequest{Objective: "x"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Len(t, store.runs, 1)
	for _, r := range store.runs {
		require.Equal(t, domain.PhaseFailed, r.Status, "a run nobody will advance is not left looking active")
		require.NotNil(t, r.ErrorMessage)
	}

	// a duplicate of a task that already exists is fine
	h2, _, sched2, user2 := newResearchHandler(t)
	sched2.err = domain.ErrIdempotentConflict
	require.Equal(t, http.StatusAccepted, postResearch(h2, user2, domain.ResearchRequest{Objective: "x"}).Code)

	// and with no scheduler at all nothing is saved
	store3 := newMockResearchStore()
	h3 := NewResearchHandler(research.NewEngine(store3, nil, nil, nil, nil), store3)
	require.Equal(t, http.StatusInternalServerError, postResearch(h3, user, domain.ResearchRequest{Objective: "x"}).Code)
	require.Empty(t, store3.runs)
}

func TestResearchListLimitIsCapped(t *testing.T) {
	h, store, _, user := newResearchHandler(t)
	for q, want := range map[string]int{"": 20, "?limit=5": 5, "?limit=100000": 200, "?limit=abc": 20, "?limit=-4": 20} {
		req := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research"+q, nil), user)
		w := httptest.NewRecorder()
		h.List(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, want, store.lastListLimit, q)
	}
}
