package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// Workable reads apply.workable.com/api/v3/accounts/{slug}/jobs (a POST that needs no key; the answer names the
// next page with a token), then api/v2/accounts/{slug}/jobs/{shortcode} for the description of the first MaxDetail
// postings. The public address of a posting is https://apply.workable.com/{slug}/j/{shortcode}/.
type Workable struct {
	F         *Fetcher
	MaxDetail int // 0 means the default (50)
}

func (Workable) Provider() string { return "workable" }

type workableJob struct {
	Shortcode string `json:"shortcode"`
	Title     string `json:"title"`
	Remote    *bool  `json:"remote"`
	Workplace string `json:"workplace"`
	State     string `json:"state"`
	Internal  bool   `json:"isInternal"`
	Published string `json:"published"`
	Type      string `json:"type"`
	Location  struct {
		City    string `json:"city"`
		Region  string `json:"region"`
		Country string `json:"country"`
	} `json:"location"`
	Department []string `json:"department"`
}

func (w Workable) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(w.F)
	r := f.begin("workable", token)
	if token == "" || strings.ContainsAny(token, "/ ") {
		r.pageFailed(fmt.Errorf("workable token %q is not an account slug", token))
		return r.finish()
	}
	limit := w.MaxDetail
	if limit <= 0 {
		limit = defaultMaxFetch
	}
	base := "https://apply.workable.com/api/v3/accounts/" + url.PathEscape(token) + "/jobs"
	var list []workableJob
	next := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			r.stopped(fmt.Sprintf("stopped after %d pages", maxPages))
			break
		}
		payload := map[string]any{"query": "", "location": []any{}, "department": []any{}, "worktype": []any{}, "remote": []any{}}
		if next != "" {
			payload["token"] = next
		}
		body, err := f.PostJSON(ctx, base, payload)
		if err != nil {
			r.pageFailed(err)
			break
		}
		var resp struct {
			Results  *[]workableJob `json:"results"`
			NextPage string         `json:"nextPage"`
		}
		if err := decode(body, &resp); err != nil {
			r.pageFailed(err)
			break
		}
		if resp.Results == nil {
			r.pageFailed(fmt.Errorf("workable %q: the answer has no results list: %s", token, preview(body)))
			break
		}
		r.pageOK()
		list = append(list, *resp.Results...)
		if len(*resp.Results) == 0 || resp.NextPage == "" || resp.NextPage == next {
			break
		}
		next = resp.NextPage
	}
	if r.ok == 0 {
		return r.finish()
	}
	fetched := 0
	for _, p := range list {
		if p.Internal || (p.State != "" && p.State != "published") || p.Shortcode == "" {
			continue
		}
		j := jobs.RawJob{
			Source: "workable", SourceKind: jobs.EmployerATS, BoardToken: token,
			ExternalID:     p.Shortcode,
			Title:          strings.TrimSpace(p.Title),
			URL:            "https://apply.workable.com/" + url.PathEscape(token) + "/j/" + url.PathEscape(p.Shortcode) + "/",
			Remote:         p.Remote,
			EmploymentType: strings.TrimSpace(p.Type),
			Department:     strings.Join(nonEmpty(p.Department...), ", "),
			PostedAt:       parseTime(p.Published),
			FetchedAt:      r.scan.StartedAt,
		}
		j.Locations = addUnique(nil, strings.Join(nonEmpty(p.Location.City, p.Location.Region, p.Location.Country), ", "))
		if fetched < limit {
			fetched++
			if err := w.detail(ctx, f, token, p.Shortcode, &j); err != nil {
				if ctx.Err() != nil {
					r.pageFailed(ctx.Err())
					break
				}
				r.pageFailed(fmt.Errorf("posting %s: %w", p.Shortcode, err))
			} else {
				r.pageOK()
			}
		}
		r.scan.Jobs = append(r.scan.Jobs, j)
	}
	return r.finish()
}

func (Workable) detail(ctx context.Context, f *Fetcher, token, shortcode string, j *jobs.RawJob) error {
	body, err := f.Get(ctx, "https://apply.workable.com/api/v2/accounts/"+url.PathEscape(token)+"/jobs/"+url.PathEscape(shortcode))
	if err != nil {
		return err
	}
	var d struct {
		Description  string `json:"description"`
		Requirements string `json:"requirements"`
		Benefits     string `json:"benefits"`
	}
	if err := decode(body, &d); err != nil {
		return err
	}
	j.Description = joinText(htmlToText(d.Description), htmlToText(d.Requirements), htmlToText(d.Benefits))
	return nil
}
