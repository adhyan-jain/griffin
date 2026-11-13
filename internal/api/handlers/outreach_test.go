package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

// A host (an agent's device) must never be able to approve a message or change sending limits: those are the
// owner's decisions. Both are refused before the repository is touched, so a nil repository proves it.
func TestHostCannotApproveOrRaiseLimits(t *testing.T) {
	h := NewOutreachHandler(nil, nil)
	host := &domain.Host{ID: uuid.New(), UserID: uuid.New()}
	id := uuid.New().String()

	req := httptest.NewRequest("PATCH", "/x", strings.NewReader(`{"status":"approved"}`))
	req.SetPathValue("id", id)
	req = req.WithContext(middleware.WithHost(req.Context(), host))
	rec := httptest.NewRecorder()
	h.UpdateTarget(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("host approving: want 403, got %d", rec.Code)
	}

	req = httptest.NewRequest("PATCH", "/x", strings.NewReader(`{"limits":{"daily_cap":500}}`))
	req.SetPathValue("id", id)
	req = req.WithContext(middleware.WithHost(req.Context(), host))
	rec = httptest.NewRecorder()
	h.UpdateCampaign(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("host changing limits: want 403, got %d", rec.Code)
	}
}

func TestCSVCellsCannotRunAsFormulas(t *testing.T) {
	for in, want := range map[string]string{"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")", "+1": "'+1", "-2": "'-2", "@a": "'@a", "plain": "plain", "": ""} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q)=%q want %q", in, got, want)
		}
	}
}
