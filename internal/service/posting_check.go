package service

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/hands/web"
)

// PostingState is what a plain fetch of a listing's link shows.
type PostingState string

const (
	PostingOpen    PostingState = "open"
	PostingMissing PostingState = "missing"
	PostingUnknown PostingState = "unknown" // the page needs a browser (a script-built careers page) or did not load
)

var (
	scriptOrStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	anyTag        = regexp.MustCompile(`(?s)<[^>]+>`)
	internWord    = regexp.MustCompile(`(?i)\bintern(ship)?s?\b`)
)

// CheckPosting fetches a listing's link and decides, without a model, whether the internship is plausibly there.
// It is missing only on hard evidence: the link is gone (404/410), or the page renders plenty of text and none of
// it mentions an internship. Anything else is unknown and the agent checks it in a real browser.
func CheckPosting(ctx context.Context, client *http.Client, link string) PostingState {
	if !strings.HasPrefix(link, "http") {
		return PostingUnknown
	}
	if client == nil {
		client = web.SafeHTTPClient(10 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return PostingUnknown
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Griffin/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return PostingUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return PostingMissing
	}
	if resp.StatusCode != http.StatusOK {
		return PostingUnknown
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	text := anyTag.ReplaceAllString(scriptOrStyle.ReplaceAllString(string(body), " "), " ")
	text = strings.Join(strings.Fields(text), " ")
	if internWord.MatchString(text) {
		return PostingOpen
	}
	if len(text) >= 1500 {
		return PostingMissing
	}
	return PostingUnknown
}
