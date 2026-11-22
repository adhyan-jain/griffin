package ats

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/jobs"
)

const workdayPage = 20

// Workday reads {tenant}.wd{N}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs (POST, offset/limit 20), then the
// posting detail for the first MaxDetail postings. The board token is "tenant.wdN/site", for example
// "nvidia.wd5/NVIDIAExternalCareerSite".
type Workday struct {
	F          *Fetcher
	SearchText string // "" means "intern"
	MaxDetail  int    // 0 means the default (50)
}

func (Workday) Provider() string { return "workday" }

var workdayToken = regexp.MustCompile(`^([a-zA-Z0-9-]+)\.(wd\d+)/([A-Za-z0-9_.-]+)$`)

// workdayBoard splits a token into the host, tenant and site.
func workdayBoard(token string) (host, tenant, site string, err error) {
	m := workdayToken.FindStringSubmatch(strings.TrimSpace(token))
	if m == nil {
		return "", "", "", fmt.Errorf("workday token %q is not tenant.wdN/site", token)
	}
	return m[1] + "." + m[2] + ".myworkdayjobs.com", m[1], m[3], nil
}

func (w Workday) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(w.F)
	r := f.begin("workday", token)
	host, tenant, site, err := workdayBoard(token)
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	search := w.SearchText
	if search == "" {
		search = "intern"
	}
	limit := w.MaxDetail
	if limit <= 0 {
		limit = defaultMaxFetch
	}
	base := "https://" + host + "/wday/cxs/" + tenant + "/" + site

	type posting struct {
		Title        string   `json:"title"`
		ExternalPath string   `json:"externalPath"`
		Locations    string   `json:"locationsText"`
		PostedOn     string   `json:"postedOn"`
		Bullets      []string `json:"bulletFields"`
	}
	var list []posting
	total := 0
	for page, offset := 0, 0; ; page++ {
		if page >= maxPages {
			r.stopped(fmt.Sprintf("stopped after %d pages", maxPages))
			break
		}
		body, err := f.PostJSON(ctx, base+"/jobs", map[string]any{"appliedFacets": map[string]any{}, "limit": workdayPage, "offset": offset, "searchText": search})
		if err != nil {
			r.pageFailed(err)
			break
		}
		var resp struct {
			Total       int        `json:"total"`
			JobPostings *[]posting `json:"jobPostings"`
		}
		if err := decode(body, &resp); err != nil {
			r.pageFailed(err)
			break
		}
		if resp.JobPostings == nil {
			r.pageFailed(fmt.Errorf("workday %q: the answer has no jobPostings list: %s", token, preview(body)))
			break
		}
		r.pageOK()
		if page == 0 {
			total = resp.Total // later pages answer total 0
		}
		list = append(list, *resp.JobPostings...)
		offset += len(*resp.JobPostings)
		if len(*resp.JobPostings) < workdayPage || (total > 0 && offset >= total) {
			break
		}
	}
	if r.ok == 0 {
		return r.finish()
	}
	fetched := 0
	for _, p := range list {
		j := jobs.RawJob{
			Source: "workday", SourceKind: jobs.EmployerATS, BoardToken: token,
			Title:     strings.TrimSpace(p.Title),
			URL:       "https://" + host + "/" + site + p.ExternalPath, // Workday's public address; the detail's externalUrl replaces it
			PostedAt:  ParseWorkdayPosted(p.PostedOn, r.scan.StartedAt),
			FetchedAt: r.scan.StartedAt,
		}
		j.Locations = addUnique(nil, p.Locations)
		if len(p.Bullets) > 0 {
			j.ExternalID = strings.TrimSpace(p.Bullets[0])
		}
		if j.ExternalID == "" {
			j.ExternalID = p.ExternalPath
		}
		if fetched < limit && p.ExternalPath != "" {
			fetched++
			if err := w.detail(ctx, f, base, p.ExternalPath, &j); err != nil {
				if ctx.Err() != nil {
					r.pageFailed(ctx.Err())
					break
				}
				r.pageFailed(fmt.Errorf("posting %s: %w", p.ExternalPath, err))
			} else {
				r.pageOK()
			}
		}
		r.scan.Jobs = append(r.scan.Jobs, j)
	}
	return r.finish()
}

func (Workday) detail(ctx context.Context, f *Fetcher, base, externalPath string, j *jobs.RawJob) error {
	body, err := f.Get(ctx, base+externalPath)
	if err != nil {
		return err
	}
	var d struct {
		Info *struct {
			Description string   `json:"jobDescription"`
			Location    string   `json:"location"`
			Additional  []string `json:"additionalLocations"`
			StartDate   string   `json:"startDate"`
			TimeType    string   `json:"timeType"`
			ReqID       string   `json:"jobReqId"`
			ExternalURL string   `json:"externalUrl"`
		} `json:"jobPostingInfo"`
		Org struct {
			Name string `json:"name"`
		} `json:"hiringOrganization"`
	}
	if err := decode(body, &d); err != nil {
		return err
	}
	if d.Info == nil {
		return fmt.Errorf("the answer has no jobPostingInfo: %s", preview(body))
	}
	in := d.Info
	j.Description = htmlToText(in.Description)
	j.EmploymentType = strings.TrimSpace(in.TimeType)
	j.EmployerName = strings.TrimSpace(d.Org.Name)
	if u := strings.TrimSpace(in.ExternalURL); u != "" {
		if _, err := url.Parse(u); err == nil {
			j.URL = u
		}
	}
	if id := strings.TrimSpace(in.ReqID); id != "" {
		j.ExternalID = id
	}
	j.Locations = addUnique(j.Locations, in.Location)
	j.Locations = addUnique(j.Locations, in.Additional...)
	// startDate is the exact day Workday shows the posting from. It replaces the "N days ago" estimate only when it
	// agrees with it (some tenants use the field for something else), and never when it lies in the future.
	if t := parseTime(in.StartDate); t != nil && !t.After(j.FetchedAt) {
		if j.PostedAt == nil || t.Sub(*j.PostedAt).Abs() <= 24*time.Hour {
			j.PostedAt = t
		}
	}
	return nil
}
