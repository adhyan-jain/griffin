package research

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/web"
)

// SearchHit represents one discovered URL from search.
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Domain  string `json:"domain"`
	Source  string `json:"source"`
}

// FetchedContent holds the readable text and metadata of an inspected page.
type FetchedContent struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Domain      string `json:"domain"`
	Text        string `json:"text"`
	Status      int    `json:"status"`
	ContentHash string `json:"content_hash"`
	Truncated   bool   `json:"truncated"`
}

// ErrNoTools is returned when a search or fetch is asked of a gateway that has no backend configured.
var ErrNoTools = errors.New("research tools are not configured")

// ToolBackend is what the engine needs from the outside world: web search and page fetching. ToolGateway is the
// real one; tests supply fakes that return fixed pages.
type ToolBackend interface {
	Search(ctx context.Context, query string, limit int) ([]SearchHit, error)
	Fetch(ctx context.Context, rawURL string) (*FetchedContent, error)
}

// ToolGateway interfaces with external search and page inspection capabilities.
type ToolGateway struct {
	Searcher *web.Searcher
	Fetcher  *web.Fetcher
}

// NewToolGateway builds a ToolGateway with default or provided fetcher and searcher.
func NewToolGateway(s *web.Searcher, f *web.Fetcher) *ToolGateway {
	if s == nil {
		s = web.SearcherFromEnv()
	}
	if f == nil {
		f = web.NewFetcher(web.FetcherConfig{})
	}
	return &ToolGateway{Searcher: s, Fetcher: f}
}

// Search executes a search query across configured providers.
func (g *ToolGateway) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if g == nil || g.Searcher == nil {
		return nil, ErrNoTools
	}
	results, srcName, err := g.Searcher.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	var hits []SearchHit
	for _, r := range results {
		d := extractDomain(r.URL)
		hits = append(hits, SearchHit{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
			Domain:  d,
			Source:  srcName,
		})
	}
	return hits, nil
}

// Fetch opens and extracts readable text from an arbitrary URL with security controls.
func (g *ToolGateway) Fetch(ctx context.Context, rawURL string) (*FetchedContent, error) {
	if g == nil || g.Fetcher == nil {
		return nil, ErrNoTools
	}
	p, err := g.Fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("fetch returned no page")
	}

	h := domain.DescriptionHash(p.Text)
	return &FetchedContent{
		URL:         p.URL,
		Title:       p.Title,
		Domain:      extractDomain(p.URL),
		Text:        p.Text,
		Status:      p.Status,
		ContentHash: h,
		Truncated:   p.Truncated,
	}, nil
}

// trackingParams are query parameters that identify a visit, not a page.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "msclkid": true, "mc_cid": true, "mc_eid": true,
	"igshid": true, "ref": true, "ref_src": true, "_hsenc": true, "_hsmi": true, "yclid": true,
}

// CanonicalURL normalises a page address so the same page is recognised twice: the scheme and host are lower-cased,
// the fragment and tracking parameters (utm_*, fbclid, gclid, ...) are dropped, the remaining parameters are
// sorted, and a trailing slash on a non-root path is removed. It returns "" for anything that is not http(s).
func CanonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	u.Scheme = scheme
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	u.User = nil
	if len(u.Path) > 1 {
		u.Path = strings.TrimRight(u.Path, "/")
		if u.Path == "" {
			u.Path = "/"
		}
		u.RawPath = ""
	}
	q := u.Query()
	for k := range q {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode() // sorted by key
	return u.String()
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(h, "www.")
}

// ClassifySourceType infers the source type and reliability tier from domain and path.
func ClassifySourceType(rawURL string) (domain.SourceType, domain.ReliabilityTier) {
	d := extractDomain(rawURL)
	u := strings.ToLower(rawURL)

	switch {
	case strings.Contains(d, "greenhouse.io") || strings.Contains(d, "lever.co") ||
		strings.Contains(d, "ashbyhq.com") || strings.Contains(d, "smartrecruiters.com") ||
		strings.Contains(d, "workable.com"):
		return domain.SourceOfficialATS, domain.ReliabilityPrimaryOfficial
	case strings.Contains(u, "/careers") || strings.Contains(u, "/jobs") || strings.Contains(d, "careers."):
		return domain.SourceOfficialCareerPage, domain.ReliabilityPrimaryOfficial
	case strings.Contains(d, "ycombinator.com") || strings.Contains(d, "techstars.com"):
		return domain.SourceAcceleratorDirectory, domain.ReliabilityHighAuthority
	case strings.Contains(d, "techcrunch.com") || strings.Contains(d, "bloomberg.com") ||
		strings.Contains(d, "reuters.com") || strings.Contains(d, "forbes.com"):
		return domain.SourceCrediblePublication, domain.ReliabilitySecondaryCredible
	case strings.Contains(d, "reddit.com") || strings.Contains(d, "news.ycombinator.com"):
		return domain.SourceCommunityDiscussion, domain.ReliabilityCommunityUser
	default:
		return domain.SourceUnverifiedWeb, domain.ReliabilityUnverified
	}
}
