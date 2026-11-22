package ats

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

const srPage = 100

// SmartRecruiters reads api.smartrecruiters.com/v1/companies/{id}/postings (offset/limit pages). The list carries
// no description, so the posting detail is read for the first MaxDetail postings; later ones keep an empty
// description (and the scan stays ok: they were listed).
type SmartRecruiters struct {
	F         *Fetcher
	MaxDetail int // 0 means the default (50)
}

func (SmartRecruiters) Provider() string { return "smartrecruiters" }

type srSummary struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReleasedDate string `json:"releasedDate"`
	Company      struct {
		Identifier string `json:"identifier"`
		Name       string `json:"name"`
	} `json:"company"`
	Location struct {
		FullLocation string `json:"fullLocation"`
		City         string `json:"city"`
		Region       string `json:"region"`
		Country      string `json:"country"`
		Remote       *bool  `json:"remote"`
	} `json:"location"`
	Department struct {
		Label string `json:"label"`
	} `json:"department"`
	TypeOfEmployment struct {
		Label string `json:"label"`
	} `json:"typeOfEmployment"`
}

func (s SmartRecruiters) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(s.F)
	r := f.begin("smartrecruiters", token)
	limit := s.MaxDetail
	if limit <= 0 {
		limit = defaultMaxFetch
	}
	var list []srSummary
	for page, offset := 0, 0; ; page++ {
		if page >= maxPages {
			r.stopped(fmt.Sprintf("stopped after %d pages", maxPages))
			break
		}
		u := fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings?limit=%d&offset=%d", url.PathEscape(token), srPage, offset)
		body, err := f.Get(ctx, u)
		if err != nil {
			r.pageFailed(err)
			break
		}
		var resp struct {
			TotalFound int          `json:"totalFound"`
			Content    *[]srSummary `json:"content"`
		}
		if err := decode(body, &resp); err != nil {
			r.pageFailed(err)
			break
		}
		if resp.Content == nil {
			r.pageFailed(fmt.Errorf("smartrecruiters %q: the answer has no content list: %s", token, preview(body)))
			break
		}
		r.pageOK()
		list = append(list, *resp.Content...)
		offset += len(*resp.Content)
		if len(*resp.Content) == 0 || offset >= resp.TotalFound {
			break
		}
	}
	if r.ok == 0 {
		return r.finish()
	}
	fetched := 0
	for _, p := range list {
		j := jobs.RawJob{
			Source: "smartrecruiters", SourceKind: jobs.EmployerATS, BoardToken: token,
			ExternalID:     p.ID,
			EmployerName:   strings.TrimSpace(p.Company.Name),
			Title:          strings.TrimSpace(p.Name),
			EmploymentType: strings.TrimSpace(p.TypeOfEmployment.Label),
			Department:     strings.TrimSpace(p.Department.Label),
			Remote:         p.Location.Remote,
			PostedAt:       parseTime(p.ReleasedDate),
			FetchedAt:      r.scan.StartedAt,
		}
		j.Locations = addUnique(nil, p.Location.FullLocation)
		// the list gives no URL; this is SmartRecruiters' own posting address, replaced by postingUrl below when the detail is read
		ident := firstNonEmpty(p.Company.Identifier, token)
		j.URL = "https://jobs.smartrecruiters.com/" + url.PathEscape(ident) + "/" + url.PathEscape(p.ID)
		if fetched < limit {
			fetched++
			if err := s.detail(ctx, f, token, p.ID, &j); err != nil {
				if ctx.Err() != nil {
					r.pageFailed(ctx.Err())
					break
				}
				r.pageFailed(fmt.Errorf("posting %s: %w", p.ID, err))
			} else {
				r.pageOK()
			}
		}
		r.scan.Jobs = append(r.scan.Jobs, j)
	}
	return r.finish()
}

var srSectionOrder = []string{"companyDescription", "jobDescription", "qualifications", "additionalInformation"}

func (SmartRecruiters) detail(ctx context.Context, f *Fetcher, token, id string, j *jobs.RawJob) error {
	body, err := f.Get(ctx, "https://api.smartrecruiters.com/v1/companies/"+url.PathEscape(token)+"/postings/"+url.PathEscape(id))
	if err != nil {
		return err
	}
	var d struct {
		PostingURL string `json:"postingUrl"`
		ApplyURL   string `json:"applyUrl"`
		JobAd      struct {
			Sections map[string]struct {
				Title string `json:"title"`
				Text  string `json:"text"`
			} `json:"sections"`
		} `json:"jobAd"`
	}
	if err := decode(body, &d); err != nil {
		return err
	}
	if u := strings.TrimSpace(d.PostingURL); u != "" {
		j.URL = u
	}
	j.ApplyURL = strings.TrimSpace(d.ApplyURL)
	keys := append([]string{}, srSectionOrder...)
	var extra []string
	for k := range d.JobAd.Sections {
		known := false
		for _, o := range srSectionOrder {
			known = known || o == k
		}
		if !known {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	var parts []string
	for _, k := range append(keys, extra...) {
		sec, ok := d.JobAd.Sections[k]
		if !ok {
			continue
		}
		if text := htmlToText(sec.Text); text != "" {
			parts = append(parts, joinText(cleanPlain(sec.Title), text))
		}
	}
	j.Description = joinText(parts...)
	return nil
}
