package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

type tierStore struct{ got domain.AssessedFilter }

func (s *tierStore) List(context.Context, uuid.UUID, []string, int) ([]*domain.Listing, error) {
	return nil, nil
}
func (s *tierStore) Get(context.Context, uuid.UUID, uuid.UUID) (*domain.Listing, error) {
	return nil, domain.ErrNotFound
}
func (s *tierStore) Move(context.Context, uuid.UUID, uuid.UUID, string, string, string, *uuid.UUID) error {
	return nil
}
func (s *tierStore) ListAssessed(_ context.Context, _ uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error) {
	s.got = f
	return nil, nil
}

func TestListingsTierFilter(t *testing.T) {
	st := &tierStore{}
	h := NewListingsHandler(st)
	do := func(q string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/listings"+q, nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
		w := httptest.NewRecorder()
		h.List(w, req)
		return w.Code
	}
	if code := do("?tier=A,B"); code != http.StatusOK || len(st.got.Tiers) != 2 || st.got.Tiers[0] != "A" {
		t.Fatalf("code %d tiers %v", code, st.got.Tiers)
	}
	if code := do("?tier=Z"); code == http.StatusOK {
		t.Fatal("an unknown tier must be rejected, not widen the list")
	}
}
