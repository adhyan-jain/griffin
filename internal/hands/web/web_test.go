package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockedAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.9", "192.168.1.10", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fe80::1", "fd00::1", "::ffff:127.0.0.1", "224.0.0.1"} {
		assert.True(t, blockedIP(net.ParseIP(ip)), ip)
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		assert.False(t, blockedIP(net.ParseIP(ip)), ip)
	}
}

// The guard is on the connection itself, so the owner's own machine and cloud metadata are unreachable
// whatever address form is used and whichever server redirects there.
func TestFetcherRefusesPrivateDestinations(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("secret")) }))
	defer local.Close()
	f := NewFetcher(FetcherConfig{})
	for _, u := range []string{local.URL, "http://127.0.0.1:1/", "http://[::1]:1/", "http://169.254.169.254/latest/meta-data/", "http://localhost:1/", "http://2130706433/"} {
		_, err := f.Fetch(context.Background(), u)
		require.Error(t, err, u)
		assert.True(t, errors.Is(err, ErrBlockedAddress) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "could not read"), "%s: %v", u, err)
	}
}

// Redirects are validated like the first request: a server cannot bounce the worker to a non-web scheme or
// into an endless chain. (Redirects to private addresses are stopped by the per-connection check above.)
func TestFetcherRefusesUnsafeRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
		}
	}))
	defer srv.Close()
	f := testFetcher()
	_, err := f.Fetch(context.Background(), srv.URL+"/to-file")
	require.Error(t, err)
	_, err = f.Fetch(context.Background(), srv.URL+"/loop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirects")
}

func testFetcher() *Fetcher {
	return NewFetcher(FetcherConfig{AllowPrivate: true, MaxBytes: 4096, MaxText: 200})
}

func TestFetchTurnsHTMLIntoReadableText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Hello  page</title><style>body{x:y}</style></head>
<body><nav>skip? no</nav><h1>Heading</h1><p>First   paragraph.</p><script>alert('x')</script><ul><li>one</li><li>two</li></ul></body></html>`))
	}))
	defer srv.Close()
	p, err := testFetcher().Fetch(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "Hello page"[:5], p.Title[:5])
	assert.Contains(t, p.Text, "Heading")
	assert.Contains(t, p.Text, "First paragraph.")
	assert.Contains(t, p.Text, "one\ntwo")
	assert.NotContains(t, p.Text, "alert", "scripts are dropped")
	assert.NotContains(t, p.Text, "body{x:y}", "styles are dropped")
}

func TestFetchCapsSizeAndRejectsBinaryAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("a", 10000)))
		case "/img":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher()
	p, err := f.Fetch(context.Background(), srv.URL+"/big")
	require.NoError(t, err)
	assert.True(t, p.Truncated)
	assert.LessOrEqual(t, len([]rune(p.Text)), 200)
	_, err = f.Fetch(context.Background(), srv.URL+"/img")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a text page")
	_, err = f.Fetch(context.Background(), srv.URL+"/missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	for _, bad := range []string{"ftp://example.com/x", "file:///etc/passwd", "http://user:pw@example.com/", "javascript:alert(1)", ""} {
		_, err := f.Fetch(context.Background(), bad)
		assert.Error(t, err, bad)
	}
}

func TestBraveAndSearXNGParseTheirResponses(t *testing.T) {
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k123", r.Header.Get("X-Subscription-Token"))
		assert.Equal(t, "go chromedp", r.URL.Query().Get("q"))
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"<strong>chromedp</strong> docs","url":"https://pkg.go.dev/chromedp","description":"Drive &amp; test"},{"title":"two","url":"https://x.test","description":""}]}}`))
	}))
	defer brave.Close()
	res, err := (&Brave{Key: "k123", BaseURL: brave.URL}).Search(context.Background(), "go chromedp", 1)
	require.NoError(t, err)
	assert.Equal(t, []Result{{Title: "chromedp docs", URL: "https://pkg.go.dev/chromedp", Snippet: "Drive & test"}}, res)

	sx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "json", r.URL.Query().Get("format"))
		_, _ = w.Write([]byte(`{"results":[{"title":"A","url":"https://a.test","content":"about a"}]}`))
	}))
	defer sx.Close()
	res, err = (&SearXNG{BaseURL: sx.URL}).Search(context.Background(), "x", 5)
	require.NoError(t, err)
	assert.Equal(t, []Result{{Title: "A", URL: "https://a.test", Snippet: "about a"}}, res)
}

func TestBadKeyIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	_, err := (&Brave{Key: "bad", BaseURL: srv.URL}).Search(context.Background(), "x", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check the key")
}

const ddgFixture = `<html><body><div class="results">
<div class="result"><h2 class="result__title"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc&rut=abc">The Go docs</a></h2>
<a class="result__snippet" href="x">Build <b>fast</b> software</a></div>
<div class="result"><a class="result__a" href="https://example.org/direct">Direct link</a></div>
</div></body></html>`

func TestDuckDuckGoParsingAndBlockDetection(t *testing.T) {
	assert.Equal(t, []Result{
		{Title: "The Go docs", URL: "https://go.dev/doc", Snippet: "Build fast software"},
		{Title: "Direct link", URL: "https://example.org/direct"},
	}, parseDDG(ddgFixture))

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ddgFixture)) }))
	defer ok.Close()
	res, err := (&DuckDuckGo{BaseURL: ok.URL}).Search(context.Background(), "go", 1)
	require.NoError(t, err)
	assert.Len(t, res, 1)

	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }))
	defer blocked.Close()
	_, err = (&DuckDuckGo{BaseURL: blocked.URL}).Search(context.Background(), "go", 1)
	assert.ErrorIs(t, err, ErrSearchBlocked)
}

type failingProvider struct{ err error }

func (failingProvider) Name() string { return "f" }
func (f failingProvider) Search(context.Context, string, int) ([]Result, error) {
	return nil, f.err
}

type okProvider struct{}

func (okProvider) Name() string { return "ok" }
func (okProvider) Search(context.Context, string, int) ([]Result, error) {
	return []Result{{Title: "t", URL: "https://t.test"}}, nil
}

func TestSearcherFallsBackAndExplainsWhenNothingWorks(t *testing.T) {
	res, by, err := (&Searcher{Providers: []Provider{failingProvider{errors.New("down")}, okProvider{}}}).Search(context.Background(), "q", 3)
	require.NoError(t, err)
	assert.Equal(t, "ok", by)
	assert.Len(t, res, 1)

	_, _, err = (&Searcher{Providers: []Provider{failingProvider{ErrSearchBlocked}}}).Search(context.Background(), "q", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BRAVE_SEARCH_API_KEY", "the error says how to fix it")
}
