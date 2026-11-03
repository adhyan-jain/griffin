package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJWKSFailedRefreshBackoff(t *testing.T) {
	var fetches atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		if fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer srv.Close()

	now := time.Unix(1_000_000, 0)
	var nowMu sync.Mutex
	c := NewJWKSCache(srv.URL, nil)
	c.now = func() time.Time { nowMu.Lock(); defer nowMu.Unlock(); return now }
	advance := func(d time.Duration) { nowMu.Lock(); now = now.Add(d); nowMu.Unlock() }

	var wg sync.WaitGroup
	var errs atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Key("unknown"); err != nil {
				errs.Add(1)
			}
		}()
	}
	wg.Wait()
	if errs.Load() != 50 {
		t.Fatalf("expected all 50 to fail closed, got %d errors", errs.Load())
	}
	if n := fetches.Load(); n > 1 {
		t.Fatalf("expected <=1 outbound fetch within backoff window, got %d", n)
	}

	advance(jwksFailureBackoff - time.Second)
	_, _ = c.Key("unknown")
	if n := fetches.Load(); n != 1 {
		t.Fatalf("fetch inside window: %d", n)
	}

	fail.Store(false)
	advance(2 * time.Second)
	_, _ = c.Key("unknown")
	if n := fetches.Load(); n != 2 {
		t.Fatalf("expected refetch after window, fetches=%d", n)
	}
}
