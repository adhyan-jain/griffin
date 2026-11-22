package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// maxDiscoverBoards is how many boards one careers page may add. A page that links to more is a job-board directory,
// not an employer's own page.
const maxDiscoverBoards = 6

// Verified is a detected board that its adapter read successfully: the only kind that may be stored as a board.
type Verified struct {
	Ref  BoardRef
	Scan jobs.Scan // the read that proved it, ok by construction
}

// Rejected is a detected board that could not be read, or was read only partly. It is reported, never stored.
type Rejected struct {
	Ref    BoardRef
	Status jobs.ScanStatus
	Error  string
}

// Discovery is what one careers page yielded.
type Discovery struct {
	PageURL  string
	Fetched  bool // the page itself was read
	PageErr  string
	Verified []Verified
	Rejected []Rejected
}

// Discover fetches a company's careers page, finds the ATS boards it links to or embeds (DetectBoards), reads each
// with its adapter, and keeps only the boards whose read was ok: a token that 404s or answers with something that is
// not a board is Rejected and never becomes a board. A page that names no ATS board but carries schema.org JobPosting
// data is itself a verified board (careers_jsonld). Nothing is guessed: every verified board was detected on the page
// and read.
func Discover(ctx context.Context, f *Fetcher, adapters map[string]Adapter, pageURL string) Discovery {
	f = orDefault(f)
	d := Discovery{PageURL: pageURL}
	if u, err := url.Parse(pageURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		d.PageErr = fmt.Sprintf("%q is not an http(s) URL", pageURL)
		return d
	}
	body, err := f.Get(ctx, pageURL)
	if err != nil {
		d.PageErr = err.Error()
		return d
	}
	d.Fetched = true
	refs := DetectBoards(string(body))
	if len(refs) > maxDiscoverBoards {
		refs = refs[:maxDiscoverBoards]
	}
	for _, ref := range refs {
		a := adapters[ref.Provider]
		if a == nil {
			d.Rejected = append(d.Rejected, Rejected{Ref: ref, Status: jobs.ScanFailed, Error: "no adapter for " + ref.Provider})
			continue
		}
		scan := a.Read(ctx, ref.Token)
		if scan.Status != jobs.ScanOK {
			d.Rejected = append(d.Rejected, Rejected{Ref: ref, Status: scan.Status, Error: scan.Error})
			continue
		}
		d.Verified = append(d.Verified, Verified{Ref: ref, Scan: scan})
	}
	if len(d.Verified) == 0 {
		if a := adapters["careers_jsonld"]; a != nil {
			if scan := a.Read(ctx, pageURL); scan.Status == jobs.ScanOK && len(scan.Jobs) > 0 {
				d.Verified = append(d.Verified, Verified{Ref: BoardRef{Provider: "careers_jsonld", Token: pageURL}, Scan: scan})
			}
		}
	}
	return d
}

// CareersURLs are the two pages tried for a company that is known only by its domain.
func CareersURLs(domain string) []string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimPrefix(d, "www.")
	d = strings.TrimRight(d, "/")
	// a company's own public domain: a name with a dot, not an address, a port or a local host
	if d == "" || strings.ContainsAny(d, " /?#:@") || !strings.Contains(d, ".") || d == "localhost" || strings.HasSuffix(d, ".local") || strings.HasSuffix(d, ".internal") {
		return nil
	}
	if last := d[strings.LastIndex(d, ".")+1:]; last == "" || last[0] >= '0' && last[0] <= '9' {
		return nil // an IP address
	}
	return []string{"https://" + d + "/careers", "https://" + d + "/jobs"}
}
