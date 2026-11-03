package middleware_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

// decodeHandler mimics a JSON handler: decode errors go through ErrorJSON.
var decodeHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	var v map[string]any
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}
	w.WriteHeader(http.StatusOK)
})

func bigJSON(n int) string {
	return `{"goal":"` + strings.Repeat("a", n) + `"}`
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestBodyLimitOversizedIs413(t *testing.T) {
	h := middleware.BodyLimit(0, middleware.IsArtifactUploadRoute)(decodeHandler)
	rec := post(h, "/api/v1/tasks", bigJSON(2<<20))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("expected clear message, got %s", rec.Body.String())
	}
}

func TestBodyLimitNormalUnaffected(t *testing.T) {
	h := middleware.BodyLimit(0, middleware.IsArtifactUploadRoute)(decodeHandler)
	if rec := post(h, "/api/v1/tasks", bigJSON(1000)); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestBodyLimitSkipsArtifactRoutes(t *testing.T) {
	h := middleware.BodyLimit(0, middleware.IsArtifactUploadRoute)(decodeHandler)
	for _, p := range []string{"/api/v1/artifacts/upload", "/api/v1/artifacts"} {
		if rec := post(h, p, bigJSON(2<<20)); rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d", p, rec.Code)
		}
	}
}
