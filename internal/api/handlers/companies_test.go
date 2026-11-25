package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

type mockKnowledgeStore struct {
	entities      map[uuid.UUID]*domain.CanonicalEntity
	relationships []*domain.EntityRelationship
	observations  []*domain.EntityObservation
	aliases       []*domain.EntityAlias
}

func newMockKnowledgeStore() *mockKnowledgeStore {
	return &mockKnowledgeStore{
		entities: make(map[uuid.UUID]*domain.CanonicalEntity),
	}
}

func (m *mockKnowledgeStore) UpsertEntity(_ context.Context, e *domain.CanonicalEntity) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	e.UpdatedAt = time.Now()
	m.entities[e.ID] = e
	return nil
}

func (m *mockKnowledgeStore) GetEntity(_ context.Context, userID, id uuid.UUID) (*domain.CanonicalEntity, error) {
	e, ok := m.entities[id]
	if !ok || e.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return e, nil
}

func (m *mockKnowledgeStore) FindEntityByKey(_ context.Context, userID uuid.UUID, kind domain.EntityKind, key string) (*domain.CanonicalEntity, error) {
	for _, e := range m.entities {
		if e.UserID == userID && e.Kind == kind && e.CanonicalKey == key {
			return e, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockKnowledgeStore) ListEntities(_ context.Context, userID uuid.UUID, kind domain.EntityKind, limit int) ([]*domain.CanonicalEntity, error) {
	var list []*domain.CanonicalEntity
	for _, e := range m.entities {
		if e.UserID == userID && e.Kind == kind {
			list = append(list, e)
			if len(list) >= limit {
				break
			}
		}
	}
	return list, nil
}

func (m *mockKnowledgeStore) AddAlias(_ context.Context, a *domain.EntityAlias) error {
	m.aliases = append(m.aliases, a)
	return nil
}

func (m *mockKnowledgeStore) FindEntityByAlias(_ context.Context, userID uuid.UUID, alias string) (*domain.CanonicalEntity, error) {
	return nil, domain.ErrNotFound
}

func (m *mockKnowledgeStore) UpsertRelationship(_ context.Context, rel *domain.EntityRelationship) error {
	m.relationships = append(m.relationships, rel)
	return nil
}

func (m *mockKnowledgeStore) ListRelationships(_ context.Context, userID, entityID uuid.UUID) ([]*domain.EntityRelationship, error) {
	var list []*domain.EntityRelationship
	for _, r := range m.relationships {
		if r.UserID == userID && (r.SubjectID == entityID || r.ObjectID == entityID) {
			list = append(list, r)
		}
	}
	return list, nil
}

func (m *mockKnowledgeStore) RecordObservation(_ context.Context, obs *domain.EntityObservation) error {
	m.observations = append(m.observations, obs)
	return nil
}

func (m *mockKnowledgeStore) ListObservations(_ context.Context, userID, entityID uuid.UUID) ([]*domain.EntityObservation, error) {
	var list []*domain.EntityObservation
	for _, o := range m.observations {
		if o.UserID == userID && o.EntityID == entityID {
			list = append(list, o)
		}
	}
	return list, nil
}

func TestCompaniesHandler(t *testing.T) {
	userID := uuid.New()
	store := newMockKnowledgeStore()
	handler := NewCompaniesHandler(store, nil)

	// Seed a company
	company := &domain.CanonicalEntity{
		ID:           uuid.New(),
		UserID:       userID,
		Kind:         domain.EntityCompany,
		Name:         "Databricks",
		CanonicalKey: "databricks.com",
		Summary:      "Unified data and AI platform",
		Metadata:     map[string]any{"funding": "Series I"},
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, store.UpsertEntity(context.Background(), company))

	// 1. Explorer list
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies/explorer", nil), userID)
	w := httptest.NewRecorder()
	handler.Explorer(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Success bool `json:"success"`
		Data    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.True(t, listResp.Success)
	require.Len(t, listResp.Data, 1)
	require.Equal(t, "Databricks", listResp.Data[0].Name)

	// 2. Get detail
	getReq := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+company.ID.String(), nil), userID)
	getReq.SetPathValue("id", company.ID.String())
	getW := httptest.NewRecorder()
	handler.Get(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)
}
