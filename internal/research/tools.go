package research

import (
	"context"
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
		return nil, nil
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
		return nil, nil
	}
	p, err := g.Fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
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
