package ats

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeWeb is a local server that stands in for every provider host. The Fetcher's client rewrites each request to
// it and passes the host that was asked for in X-Orig-Host, so a handler can tell api.lever.co from api.eu.lever.co.
type fakeWeb struct {
	t      *testing.T
	mu     sync.Mutex
	Calls  []call
	handle func(w http.ResponseWriter, r *http.Request, c call)
}

type call struct {
	Host, Method, Path, Query, Body, UserAgent string
}

func newFakeWeb(t *testing.T, h func(w http.ResponseWriter, r *http.Request, c call)) (*fakeWeb, *Fetcher) {
	t.Helper()
	fw := &fakeWeb{t: t, handle: h}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c := call{Host: r.Header.Get("X-Orig-Host"), Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(b), UserAgent: r.UserAgent()}
		fw.mu.Lock()
		fw.Calls = append(fw.Calls, c)
		fw.mu.Unlock()
		fw.handle(w, r, c)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	f := &Fetcher{
		Client: &http.Client{Transport: rewrite{u}, Timeout: 5 * time.Second},
		// no waiting in tests; the throttle and retry waits are checked with their own fake clock
		Sleep:      func(context.Context, time.Duration) error { return nil },
		MaxRetries: 2, Backoff: time.Second, MaxWait: time.Minute,
	}
	return fw, f
}

type rewrite struct{ to *url.URL }

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("X-Orig-Host", r.URL.Host)
	r2.URL.Scheme, r2.URL.Host = rw.to.Scheme, rw.to.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func (fw *fakeWeb) count(pathPrefix string) int {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	n := 0
	for _, c := range fw.Calls {
		if len(c.Path) >= len(pathPrefix) && c.Path[:len(pathPrefix)] == pathPrefix {
			n++
		}
	}
	return n
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func serve(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func fail(w http.ResponseWriter, code int) { http.Error(w, "no", code) }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
