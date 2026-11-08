package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Result is one search hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// Provider is one search backend.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// ErrSearchBlocked means a keyless search engine refused an automated request. The fix is a search API key
// (Brave) or your own SearXNG address; both are in the host's configuration.
var ErrSearchBlocked = errors.New("the search engine refused an automated request")

var tagRe = regexp.MustCompile(`<[^>]*>`)

func clean(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, ""))), " ")
}

func getJSON(ctx context.Context, hc *http.Client, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("the search service refused the request (%d): check the key or address", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("the search service is rate limiting this key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the search service answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// ---- Brave Search API (needs a key; free tier available) ----

// Brave searches through the Brave Search API.
type Brave struct {
	Key     string
	BaseURL string // default https://api.search.brave.com
	HTTP    *http.Client
}

func (b *Brave) Name() string { return "brave" }

func (b *Brave) Search(ctx context.Context, q string, limit int) ([]Result, error) {
	base := b.BaseURL
	if base == "" {
		base = "https://api.search.brave.com"
	}
	var out struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	u := fmt.Sprintf("%s/res/v1/web/search?q=%s&count=%d", strings.TrimRight(base, "/"), url.QueryEscape(q), limit)
	if err := getJSON(ctx, b.client(), u, map[string]string{"X-Subscription-Token": b.Key}, &out); err != nil {
		return nil, err
	}
	var res []Result
	for _, r := range out.Web.Results {
		res = append(res, Result{Title: clean(r.Title), URL: r.URL, Snippet: clean(r.Description)})
	}
	return trim(res, limit), nil
}

func (b *Brave) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// ---- SearXNG (self-hosted, JSON format enabled) ----

// SearXNG searches through a SearXNG instance the owner runs.
type SearXNG struct {
	BaseURL string
	HTTP    *http.Client
}

func (s *SearXNG) Name() string { return "searxng" }

func (s *SearXNG) Search(ctx context.Context, q string, limit int) ([]Result, error) {
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	u := fmt.Sprintf("%s/search?q=%s&format=json", strings.TrimRight(s.BaseURL, "/"), url.QueryEscape(q))
	if err := getJSON(ctx, hc, u, nil, &out); err != nil {
		return nil, err
	}
	var res []Result
	for _, r := range out.Results {
		res = append(res, Result{Title: clean(r.Title), URL: r.URL, Snippet: clean(r.Content)})
	}
	return trim(res, limit), nil
}

// ---- DuckDuckGo HTML (no key; best effort) ----

// DuckDuckGo reads DuckDuckGo's plain HTML results. It needs no key but the service may decline automated
// requests, in which case it reports ErrSearchBlocked and the owner is pointed to a keyed or self-hosted option.
type DuckDuckGo struct {
	BaseURL string // default https://html.duckduckgo.com
	HTTP    *http.Client
}

func (d *DuckDuckGo) Name() string { return "duckduckgo" }

func (d *DuckDuckGo) Search(ctx context.Context, q string, limit int) ([]Result, error) {
	base := d.BaseURL
	if base == "" {
		base = "https://html.duckduckgo.com"
	}
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/html/?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		return nil, ErrSearchBlocked
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the search service answered %d", resp.StatusCode)
	}
	res := parseDDG(string(body))
	if len(res) == 0 {
		if strings.Contains(strings.ToLower(string(body)), "anomaly") || strings.Contains(strings.ToLower(string(body)), "captcha") {
			return nil, ErrSearchBlocked
		}
		return nil, nil // a real search with no results
	}
	return trim(res, limit), nil
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// ddgTarget unwraps DuckDuckGo's redirect link (//duckduckgo.com/l/?uddg=<encoded>) to the real address.
func ddgTarget(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if t := u.Query().Get("uddg"); t != "" {
		return t
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return href
	}
	return ""
}

func parseDDG(src string) []Result {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil
	}
	var res []Result
	var cur *Result
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			switch {
			case hasClass(n, "result__a"):
				if t := ddgTarget(attr(n, "href")); t != "" {
					res = append(res, Result{Title: text(n), URL: t})
					cur = &res[len(res)-1]
				}
			case hasClass(n, "result__snippet") && cur != nil:
				cur.Snippet = text(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(doc)
	return res
}

func trim(r []Result, limit int) []Result {
	if limit > 0 && len(r) > limit {
		return r[:limit]
	}
	return r
}

// ---- Searcher ----

// Searcher tries its providers in order. Searching is read-only, so moving to the next provider after a
// failure is always safe.
type Searcher struct{ Providers []Provider }

// SearcherFromEnv builds the provider list from the host's environment: Brave when BRAVE_SEARCH_API_KEY is
// set, SearXNG when SEARXNG_URL is set, and DuckDuckGo last as the no-key fallback.
func SearcherFromEnv() *Searcher {
	var ps []Provider
	if k := strings.TrimSpace(os.Getenv("BRAVE_SEARCH_API_KEY")); k != "" {
		ps = append(ps, &Brave{Key: k})
	}
	if u := strings.TrimSpace(os.Getenv("SEARXNG_URL")); u != "" {
		ps = append(ps, &SearXNG{BaseURL: u})
	}
	ps = append(ps, &DuckDuckGo{})
	return &Searcher{Providers: ps}
}

// Names lists the providers in order.
func (s *Searcher) Names() []string {
	var n []string
	for _, p := range s.Providers {
		n = append(n, p.Name())
	}
	return n
}

// Search returns results and the provider that produced them.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]Result, string, error) {
	var problems []string
	for _, p := range s.Providers {
		res, err := p.Search(ctx, query, limit)
		if err == nil {
			return res, p.Name(), nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		problems = append(problems, fmt.Sprintf("%s: %v", p.Name(), err))
	}
	msg := "no search service worked (" + strings.Join(problems, "; ") + ")"
	if len(s.Providers) > 0 && errors.Is(lastErr(problems), ErrSearchBlocked) || strings.Contains(msg, ErrSearchBlocked.Error()) {
		msg += ". Set BRAVE_SEARCH_API_KEY (a free key works) or SEARXNG_URL on the host for reliable search"
	}
	return nil, "", errors.New(msg)
}

func lastErr(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New(problems[len(problems)-1])
}
