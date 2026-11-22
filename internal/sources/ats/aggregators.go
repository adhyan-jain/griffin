package ats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
)

// The aggregator adapters wrap the existing parsers (sources.ParseInternshala, sources.ParseUnstop,
// domain.ParseInternDock) and return their postings as jobs.Scan with SourceKind Aggregator. They are leads:
// the employer's own posting still has to be fetched to verify them. The parsers' own choices stay (for example
// Internshala titles without "intern" get " Intern" appended and its locations get ", India").

// Internshala reads one category page; the token is its path, for example
// "/internships/software-development-internship/".
type Internshala struct{ F *Fetcher }

func (Internshala) Provider() string { return sources.Internshala }

func (a Internshala) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(a.F)
	r := f.begin(sources.Internshala, token)
	body, err := f.Get(ctx, "https://internshala.com"+token)
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	posts := sources.ParseInternshala(string(body))
	if len(posts) == 0 {
		r.pageFailed(fmt.Errorf("no internship cards found on the page (its markup may have changed)"))
		return r.finish()
	}
	r.pageOK()
	r.scan.Jobs = postingsToJobs(posts, sources.Internshala, r.scan.StartedAt)
	return r.finish()
}

// InternDock reads one collection page; the token is its path, for example "/internships/india".
type InternDock struct{ F *Fetcher }

func (InternDock) Provider() string { return sources.InternDock }

func (a InternDock) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(a.F)
	r := f.begin(sources.InternDock, token)
	body, err := f.Get(ctx, domain.InternDockBase+token)
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	posts := domain.ParseInternDock(string(body))
	if len(posts) == 0 {
		r.pageFailed(fmt.Errorf("no internship cards found on the page (its markup may have changed)"))
		return r.finish()
	}
	r.pageOK()
	r.scan.Jobs = postingsToJobs(posts, sources.InternDock, r.scan.StartedAt)
	return r.finish()
}

// Unstop reads one search; the token is the search term.
type Unstop struct{ F *Fetcher }

func (Unstop) Provider() string { return sources.Unstop }

func (a Unstop) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(a.F)
	r := f.begin(sources.Unstop, token)
	body, err := f.Get(ctx, "https://unstop.com/api/public/opportunity/search-result?opportunity=internships&per_page=50&page=1&searchTerm="+url.QueryEscape(token))
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	var shape struct {
		Data *struct {
			Data *[]json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := decode(body, &shape); err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	if shape.Data == nil || shape.Data.Data == nil {
		r.pageFailed(fmt.Errorf("the answer has no data.data list: %s", preview(body)))
		return r.finish()
	}
	posts, err := sources.ParseUnstop(body, f.now())
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	r.pageOK()
	r.scan.Jobs = postingsToJobs(posts, sources.Unstop, r.scan.StartedAt)
	return r.finish()
}

func postingsToJobs(posts []domain.Posting, source string, fetched time.Time) []jobs.RawJob {
	out := make([]jobs.RawJob, 0, len(posts))
	for _, p := range posts {
		j := jobs.RawJob{
			Source: source, SourceKind: jobs.Aggregator,
			ExternalID:   lastSegment(p.Link),
			EmployerName: strings.TrimSpace(p.Company),
			Title:        strings.TrimSpace(p.Title),
			URL:          p.Link,
			Compensation: strings.TrimSpace(p.Pay),
			FetchedAt:    fetched,
		}
		j.Locations = addUnique(nil, p.Location)
		if p.Location == "Remote (India)" {
			j.Remote = ptr(true) // the sites' own "work from home" marking
		}
		if !p.Posted.IsZero() {
			t := p.Posted.UTC()
			j.PostedAt = &t
		}
		out = append(out, j)
	}
	return out
}

// lastSegment is the final path element of a posting link: the site's own slug or id for the posting.
func lastSegment(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return path.Base(strings.TrimRight(u.Path, "/"))
}
