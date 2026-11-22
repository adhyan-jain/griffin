package ats

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetcherRetriesOn429HonouringRetryAfter(t *testing.T) {
	var n atomic.Int32
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			fail(w, 429)
			return
		}
		serve(w, []byte(`{"ok":true}`))
	})
	var slept []time.Duration
	f.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	body, err := f.Get(context.Background(), "https://api.lever.co/x")
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("got %q, %v", body, err)
	}
	if n.Load() != 2 {
		t.Fatalf("one retry expected, got %d requests", n.Load())
	}
	found := false
	for _, d := range slept {
		found = found || d == 7*time.Second
	}
	if !found {
		t.Fatalf("Retry-After of 7s was not waited: %v", slept)
	}
}

func TestFetcherRetriesServerErrorsTwiceThenGivesUp(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { fail(w, 503) })
	var slept []time.Duration
	f.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	_, err := f.Get(context.Background(), "https://api.lever.co/x")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Fatalf("want the HTTP 503, got %v", err)
	}
	if len(fw.Calls) != 3 {
		t.Fatalf("one try and two retries, got %d requests", len(fw.Calls))
	}
	if len(slept) != 2 || slept[0] != time.Second || slept[1] != 2*time.Second {
		t.Fatalf("backoff should double: %v", slept)
	}
}

func TestFetcherDoesNotRetryOtherClientErrors(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { fail(w, 404) })
	_, err := f.Get(context.Background(), "https://api.lever.co/x")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 404 || len(fw.Calls) != 1 {
		t.Fatalf("a 404 is final: %v after %d requests", err, len(fw.Calls))
	}
}

func TestFetcherStopsWhenRetryAfterIsLongerThanItWillWait(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		w.Header().Set("Retry-After", "3600")
		fail(w, 429)
	})
	_, err := f.Get(context.Background(), "https://api.lever.co/x")
	if err == nil || len(fw.Calls) != 1 {
		t.Fatalf("an hour-long Retry-After ends the attempts: %v after %d requests", err, len(fw.Calls))
	}
}

func TestFetcherRetriesTimeouts(t *testing.T) {
	var n atomic.Int32
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if n.Add(1) == 1 {
			time.Sleep(300 * time.Millisecond) // longer than the client timeout below
		}
		serve(w, []byte(`[]`))
	})
	f.Client.Timeout = 100 * time.Millisecond
	body, err := f.Get(context.Background(), "https://api.lever.co/x")
	if err != nil || string(body) != `[]` || n.Load() != 2 {
		t.Fatalf("a timeout is retried: %q %v after %d requests", body, err, n.Load())
	}
}

func TestFetcherSendsItsUserAgentAndPostBody(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, []byte(`{}`)) })
	f.UserAgent = UserAgent
	if _, err := f.PostJSON(context.Background(), "https://x.example/api", map[string]any{"limit": 20}); err != nil {
		t.Fatal(err)
	}
	c := fw.Calls[0]
	if c.UserAgent != "griffin-jobs/1.0" || c.Method != "POST" || c.Body != `{"limit":20}` {
		t.Fatalf("%+v", c)
	}
}

func TestFetcherSpacesRequestsToOneHost(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, []byte(`{}`)) })
	clock := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	f.Now = func() time.Time { return clock }
	var slept []time.Duration
	f.Sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		clock = clock.Add(d)
		return nil
	}
	f.MinInterval = time.Second
	ctx := context.Background()
	for _, u := range []string{"https://a.example/1", "https://a.example/2", "https://b.example/1", "https://a.example/3"} {
		if _, err := f.Get(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	// a/1 free, a/2 waits 1s, b/1 free (another host), a/3 waits 1s after a/2
	var total time.Duration
	for _, d := range slept {
		total += d
	}
	if len(slept) != 2 || total != 2*time.Second {
		t.Fatalf("expected two 1s waits for host a only, got %v", slept)
	}
}

func TestRetryAfterReadsSecondsAndHTTPDates(t *testing.T) {
	f := &Fetcher{Now: func() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC) }}
	if d := f.retryAfter("12"); d != 12*time.Second {
		t.Fatal(d)
	}
	if d := f.retryAfter("Sun, 22 Nov 2026 10:00:30 GMT"); d != 30*time.Second {
		t.Fatal(d)
	}
	if f.retryAfter("") != 0 || f.retryAfter("soon") != 0 {
		t.Fatal("unreadable values are zero")
	}
}

func TestRunStatusIsOKOnlyWhenEveryPageWasRead(t *testing.T) {
	f := &Fetcher{}
	cases := []struct {
		ok, bad int
		want    string
	}{{3, 0, "ok"}, {2, 1, "partial"}, {0, 2, "failed"}, {0, 0, "failed"}}
	for _, c := range cases {
		r := f.begin("x", "y")
		for i := 0; i < c.ok; i++ {
			r.pageOK()
		}
		for i := 0; i < c.bad; i++ {
			r.pageFailed(errors.New("boom"))
		}
		s := r.finish()
		if string(s.Status) != c.want {
			t.Errorf("ok=%d bad=%d: status %s, want %s", c.ok, c.bad, s.Status, c.want)
		}
		if (s.Status == "ok") != (s.Error == "") {
			t.Errorf("error text must be empty exactly when ok: %+v", s)
		}
		if s.Status == "failed" && len(s.Jobs) != 0 {
			t.Errorf("a failed scan carries no jobs")
		}
	}
	_ = strings.Join
}
