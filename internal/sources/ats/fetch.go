// Package ats reads job postings from employer applicant-tracking boards (Greenhouse, Lever, Ashby, SmartRecruiters,
// Workday, Recruitee, Workable), from careers pages that carry schema.org JobPosting JSON-LD, and from the three
// internship aggregators. Every adapter returns a jobs.Scan: a board that could not be read completely is never
// reported as ok, and nothing a source does not say is guessed.
package ats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/griffin/core/internal/hands/web"
)

const (
	// UserAgent identifies Griffin to the boards. It carries no personal detail.
	UserAgent = "griffin-jobs/1.0"

	maxBody = 32 << 20 // the largest board seen is ~6 MB; anything bigger is an error, never a silent truncation
)

// HTTPError is a non-success answer from a board.
type HTTPError struct {
	Method, URL string
	Status      int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s answered HTTP %d", e.Method, e.URL, e.Status)
}

// Fetcher is the one HTTP client every adapter shares: a minimum gap between requests to the same host, a timeout,
// and at most MaxRetries retries (honouring Retry-After) on 429, 5xx and network errors or timeouts.
type Fetcher struct {
	Client      *http.Client
	UserAgent   string
	MinInterval time.Duration // between two requests to one host
	MaxRetries  int           // retries after the first attempt
	Backoff     time.Duration // first retry delay when the server gives no Retry-After; doubles each retry
	MaxWait     time.Duration // longest wait a retry may take; a longer Retry-After ends the attempts

	// Now and Sleep are replaceable so tests need no wall clock.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	next map[string]time.Time
}

// NewFetcher returns a Fetcher with the production settings.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client:      web.SafeHTTPClient(45 * time.Second),
		UserAgent:   UserAgent,
		MinInterval: time.Second,
		MaxRetries:  2,
		Backoff:     2 * time.Second,
		MaxWait:     60 * time.Second,
	}
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// throttle blocks until this host may be asked again, and reserves the slot after it.
func (f *Fetcher) throttle(ctx context.Context, host string) error {
	f.mu.Lock()
	if f.next == nil {
		f.next = map[string]time.Time{}
	}
	now := f.now()
	at := f.next[host]
	if at.Before(now) {
		at = now
	}
	f.next[host] = at.Add(f.MinInterval)
	f.mu.Unlock()
	return f.sleep(ctx, at.Sub(now))
}

// Do sends one request, with throttle and retry, and returns the body of a 2xx answer.
func (f *Fetcher) Do(ctx context.Context, method, rawURL string, body []byte, header map[string]string) ([]byte, error) {
	client := f.Client
	if client == nil {
		client = web.SafeHTTPClient(45 * time.Second)
	}
	ua := f.UserAgent
	if ua == "" {
		ua = UserAgent
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.5")
		for k, v := range header {
			req.Header.Set(k, v)
		}
		if err := f.throttle(ctx, req.URL.Host); err != nil {
			return nil, err
		}

		var (
			wait    time.Duration
			failure error
		)
		resp, err := client.Do(req)
		switch {
		case err != nil:
			failure = fmt.Errorf("%s %s: %w", method, rawURL, err)
		default:
			data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
			resp.Body.Close()
			switch {
			case rerr != nil:
				failure = fmt.Errorf("%s %s: reading the answer: %w", method, rawURL, rerr)
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				if len(data) > maxBody {
					return nil, fmt.Errorf("%s %s: the answer is larger than %d bytes", method, rawURL, maxBody)
				}
				return data, nil
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				failure = &HTTPError{Method: method, URL: rawURL, Status: resp.StatusCode}
				wait = f.retryAfter(resp.Header.Get("Retry-After"))
			default:
				return nil, &HTTPError{Method: method, URL: rawURL, Status: resp.StatusCode}
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt >= f.MaxRetries {
			return nil, failure
		}
		if wait <= 0 {
			wait = f.Backoff << attempt
		}
		if f.MaxWait > 0 && wait > f.MaxWait {
			return nil, fmt.Errorf("%w (asked to wait %s, longer than the %s allowed)", failure, wait, f.MaxWait)
		}
		if err := f.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// retryAfter reads a Retry-After header: seconds or an HTTP date. Zero when absent or unreadable.
func (f *Fetcher) retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(f.now()); d > 0 {
			return d
		}
	}
	return 0
}

// Get is Do with GET.
func (f *Fetcher) Get(ctx context.Context, rawURL string) ([]byte, error) {
	return f.Do(ctx, http.MethodGet, rawURL, nil, nil)
}

// PostJSON sends payload as JSON and returns the answer body.
func (f *Fetcher) PostJSON(ctx context.Context, rawURL string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return f.Do(ctx, http.MethodPost, rawURL, b, map[string]string{"Content-Type": "application/json"})
}

// decode reads a JSON body into v and says what was wrong when it is not JSON of the expected kind.
func decode(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unreadable JSON (%v): %s", err, preview(body))
	}
	return nil
}

func preview(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return strconv.Quote(s)
}
