// Package web is the read-only web worker: it searches and reads public pages for the owner. It never
// signs in anywhere and never reaches the owner's own network: every connection is checked at the moment it
// is made, so redirects and DNS tricks cannot lead it to a private address.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"

	"github.com/griffin/core/internal/domain"
)

// UserAgent identifies Griffin honestly to the sites it reads.
const UserAgent = "Griffin/1.0 (personal assistant; read-only fetch)"

// ErrBlockedAddress means a connection to a private, local or otherwise internal address was refused.
var ErrBlockedAddress = errors.New("that address is on a private or internal network and is not reachable by the web worker")

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

func blockedIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	a = a.Unmap()
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// FetcherConfig configures a Fetcher.
type FetcherConfig struct {
	Timeout time.Duration // whole request; default 20 s
	// MaxBytes caps what is read from one response; default 2 MB.
	MaxBytes int64
	// MaxText caps the text kept from a page; default 20,000 characters.
	MaxText int
	// AllowPrivate switches the address check off. It exists for tests only.
	AllowPrivate bool
}

// Fetcher reads public pages.
type Fetcher struct {
	cfg    FetcherConfig
	client *http.Client
}

// NewFetcher builds a Fetcher, applying defaults.
func NewFetcher(cfg FetcherConfig) *Fetcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 2 << 20
	}
	if cfg.MaxText <= 0 {
		cfg.MaxText = 20000
	}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Control runs with the address actually about to be connected to (after DNS), for every
		// connection including redirects, which is what makes this check hard to get around.
		Control: func(network, address string, _ syscall.RawConn) error {
			if cfg.AllowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlockedAddress
			}
			if ip := net.ParseIP(host); ip == nil || blockedIP(ip) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil, // a proxy setting must not route around the address check
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableKeepAlives:     true,
	}
	return &Fetcher{cfg: cfg, client: &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return domain.ValidateWebURL(req.URL.String())
		},
	}}
}

// Page is a fetched page reduced to readable text.
type Page struct {
	URL       string `json:"url"`
	Status    int    `json:"status"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Fetch reads one page. Only HTTP(S) addresses are accepted, and only text-like content comes back.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	if err := domain.ValidateWebURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.1")
	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) || strings.Contains(err.Error(), ErrBlockedAddress.Error()) {
			return nil, ErrBlockedAddress
		}
		return nil, fmt.Errorf("could not read %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	page := &Page{URL: resp.Request.URL.String(), Status: resp.StatusCode}
	if resp.StatusCode >= 400 {
		return page, fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	textual := ct == "" || strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml")
	if !textual {
		return page, fmt.Errorf("%s is not a text page (%s)", rawURL, strings.SplitN(ct, ";", 2)[0])
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.cfg.MaxBytes+1))
	if err != nil {
		return page, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	truncated := int64(len(body)) > f.cfg.MaxBytes
	if truncated {
		body = body[:f.cfg.MaxBytes]
	}
	if strings.Contains(ct, "html") || ct == "" {
		page.Title, page.Text = htmlToText(string(body))
	} else {
		page.Text = strings.TrimSpace(string(body))
	}
	if r := []rune(page.Text); len(r) > f.cfg.MaxText {
		page.Text, truncated = string(r[:f.cfg.MaxText]), true
	}
	page.Truncated = truncated
	return page, nil
}

var skipTags = map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "template": true, "iframe": true, "head": true}
var blockTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "section": true, "article": true, "header": true, "footer": true, "pre": true, "blockquote": true, "table": true, "ul": true, "ol": true}

// htmlToText returns the page title and its readable text, with scripts, styles and markup removed.
func htmlToText(src string) (title, text string) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", strings.TrimSpace(src)
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "title" && title == "" && n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
			}
			if skipTags[n.Data] {
				return
			}
		}
		if n.Type == html.TextNode {
			if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
				b.WriteString(t)
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockTags[n.Data] {
			b.WriteString("\n")
		}
	}
	// the title lives in <head>, which the body walk skips: find it separately
	var findTitle func(n *html.Node)
	findTitle = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil && title == "" {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTitle(c)
		}
	}
	findTitle(doc)
	walk(doc)
	lines := strings.Split(b.String(), "\n")
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return title, strings.Join(out, "\n")
}
