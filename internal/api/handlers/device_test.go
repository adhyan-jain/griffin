package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/browser"
)

type fixed struct{ d browser.Decision }

func (f fixed) Next(context.Context, string, *browser.Page, []domain.BrowserStep) (*browser.Decision, error) {
	return &f.d, nil
}

func TestDeviceNextNeedsAHostAndReturnsTheStep(t *testing.T) {
	h := NewDeviceHandler(fixed{browser.Decision{Step: domain.BrowserStep{Kind: domain.StepOpen, Text: "Notes"}}})
	body := `{"goal":"add milk","page":{"url":"home","elements":[]},"done":[]}`

	rec := httptest.NewRecorder()
	h.Next(rec, httptest.NewRequest("POST", "/x", strings.NewReader(body)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no host: want 401, got %d", rec.Code)
	}

	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	req = req.WithContext(middleware.WithHost(req.Context(), &domain.Host{ID: uuid.New(), UserID: uuid.New()}))
	rec = httptest.NewRecorder()
	h.Next(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"open"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/x", strings.NewReader(`{"goal":""}`))
	req = req.WithContext(middleware.WithHost(req.Context(), &domain.Host{ID: uuid.New()}))
	h.Next(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("an empty goal must be refused")
	}
}
