package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func (m *mockResearchStore) ListRuns(_ context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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

func TestResearchHandlerLifecycle(t *testing.T) {
	userID := uuid.New()
	store := newMockResearchStore()
	engine := research.NewEngine(store, nil, nil, nil, nil)
	handler := NewResearchHandler(engine, store)

	// 1. Create run via POST
	reqBody, _ := json.Marshal(domain.ResearchRequest{
		Objective: "Investigate AI compiler frameworks",
		Depth:     domain.DepthStandard,
	})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/research", bytes.NewReader(reqBody)), userID)
	w := httptest.NewRecorder()
	handler.Create(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var createResp struct {
		Success bool                `json:"success"`
		Data    *domain.ResearchRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	require.True(t, createResp.Success)
	runID := createResp.Data.ID

	// 2. List runs
	listReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research", nil), userID)
	listW := httptest.NewRecorder()
	handler.List(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code)

	// 3. Get run by ID
	getReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research/"+runID.String(), nil), userID)
	getReq.SetPathValue("id", runID.String())
	getW := httptest.NewRecorder()
	handler.Get(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	// 4. Pause run on active run
	activeRun := &domain.ResearchRun{
		ID:        uuid.New(),
		UserID:    userID,
		Objective: "Active research task",
		Status:    domain.PhaseInformationRetrieval,
	}
	require.NoError(t, store.CreateRun(context.Background(), activeRun))

	pauseReq := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/research/"+activeRun.ID.String()+"/pause", nil), userID)
	pauseReq.SetPathValue("id", activeRun.ID.String())
	pauseW := httptest.NewRecorder()
	handler.Pause(pauseW, pauseReq)
	require.Equal(t, http.StatusOK, pauseW.Code)

	// 5. Resume run
	resumeReq := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/research/"+activeRun.ID.String()+"/resume", nil), userID)
	resumeReq.SetPathValue("id", activeRun.ID.String())
	resumeW := httptest.NewRecorder()
	handler.Resume(resumeW, resumeReq)
	require.Equal(t, http.StatusOK, resumeW.Code)

	// 6. Evidence
	evReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/research/"+runID.String()+"/evidence", nil), userID)
	evReq.SetPathValue("id", runID.String())
	evW := httptest.NewRecorder()
	handler.Evidence(evW, evReq)
	require.Equal(t, http.StatusOK, evW.Code)
}
