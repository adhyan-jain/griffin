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
	"github.com/griffin/core/internal/repository"
)

// mockCompanyStore stands in for the companies table with its research attached.
type mockCompanyStore struct {
	summaries []*repository.CompanySummary
	details   map[uuid.UUID]*repository.CompanyDetail
	owner     uuid.UUID
	gotLimit  int
	err       error
}

func (m *mockCompanyStore) ListCompanySummaries(_ context.Context, userID uuid.UUID, limit int) ([]*repository.CompanySummary, error) {
	m.gotLimit = limit
	if m.err != nil {
		return nil, m.err
	}
	if userID != m.owner {
		return nil, nil // a nil slice: the handler must still answer []
	}
	return m.summaries, nil
}

func (m *mockCompanyStore) GetCompanyDetail(_ context.Context, userID, id uuid.UUID) (*repository.CompanyDetail, error) {
	if d, ok := m.details[id]; ok && userID == m.owner {
		return d, nil
	}
	return nil, domain.ErrNotFound
}

func TestCompaniesHandler(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	obsAt := time.Now().Add(-48 * time.Hour)
	c := domain.Company{ID: id, Name: "Databricks", NormalizedName: "databricks", Kind: "enterprise", Status: domain.CompanyActive}
	store := &mockCompanyStore{
		owner: userID,
		summaries: []*repository.CompanySummary{{
			Company: c, OpenListings: 3,
			Latest: []*domain.CompanyObservation{{Property: domain.PropFunding, Value: "Series I", ClaimOrigin: domain.OriginCompanyReported, ObservedAt: obsAt, AgeDays: 2}},
		}},
		details: map[uuid.UUID]*repository.CompanyDetail{id: {
			Company: &c, OpenListings: 1,
			Listings:     []*repository.CompanyListing{{ID: uuid.New(), Role: "Intern", Open: true}},
			Observations: []*domain.CompanyObservation{{Property: domain.PropFunding, Value: "Series I", AgeDays: 2}},
			Evidence:     []*repository.CompanyEvidence{},
		}},
	}
	handler := NewCompaniesHandler(store)

	// 1. List: companies with open listings and the latest observation per property
	w := httptest.NewRecorder()
	handler.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil), userID))
	require.Equal(t, http.StatusOK, w.Code)
	var listResp struct {
		Success bool `json:"success"`
		Data    []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			OpenListings int    `json:"open_listings"`
			Latest       []struct {
				Property    string `json:"property"`
				ClaimOrigin string `json:"claim_origin"`
			} `json:"latest_observations"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.True(t, listResp.Success)
	require.Len(t, listResp.Data, 1)
	require.Equal(t, "Databricks", listResp.Data[0].Name)
	require.Equal(t, 3, listResp.Data[0].OpenListings)
	require.Equal(t, "funding", listResp.Data[0].Latest[0].Property)
	require.Equal(t, "company_reported", listResp.Data[0].Latest[0].ClaimOrigin)
	require.Equal(t, defaultListLimit, store.gotLimit)

	// 2. The limit is capped at 200, and a bad one is refused
	w = httptest.NewRecorder()
	handler.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies?limit=100000", nil), userID))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 200, store.gotLimit)
	w = httptest.NewRecorder()
	handler.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies?limit=abc", nil), userID))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Another owner has none: an empty array, never null
	w = httptest.NewRecorder()
	handler.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil), uuid.New()))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"data":[]`)

	// 4. A store error is an error, not an empty list
	store.err = context.DeadlineExceeded
	w = httptest.NewRecorder()
	handler.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil), userID))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	store.err = nil

	// 5. Detail: the company, listings, observations with age, and evidence
	get := func(user uuid.UUID, id string) *httptest.ResponseRecorder {
		req := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+id, nil), user)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		handler.Get(rec, req)
		return rec
	}
	rec := get(userID, id.String())
	require.Equal(t, http.StatusOK, rec.Code)
	var detail struct {
		Data struct {
			Company      struct{ Name string } `json:"company"`
			Listings     []json.RawMessage     `json:"listings"`
			Observations []struct {
				AgeDays int `json:"age_days"`
			} `json:"observations"`
			Evidence []json.RawMessage `json:"evidence"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.Equal(t, "Databricks", detail.Data.Company.Name)
	require.Len(t, detail.Data.Listings, 1)
	require.Equal(t, 2, detail.Data.Observations[0].AgeDays)
	require.NotNil(t, detail.Data.Evidence)

	require.Equal(t, http.StatusNotFound, get(uuid.New(), id.String()).Code, "another owner's company")
	require.Equal(t, http.StatusBadRequest, get(userID, "not-a-uuid").Code)
}
