package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// Ashby reads api.ashbyhq.com/posting-api/job-board/{token}?includeCompensation=true. One answer holds the board.
type Ashby struct{ F *Fetcher }

func (Ashby) Provider() string { return "ashby" }

func (a Ashby) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(a.F)
	r := f.begin("ashby", token)
	body, err := f.Get(ctx, "https://api.ashbyhq.com/posting-api/job-board/"+url.PathEscape(token)+"?includeCompensation=true")
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	var resp struct {
		Jobs *[]struct {
			ID                 string `json:"id"`
			Title              string `json:"title"`
			Department         string `json:"department"`
			Team               string `json:"team"`
			EmploymentType     string `json:"employmentType"`
			Location           string `json:"location"`
			SecondaryLocations []struct {
				Location string `json:"location"`
			} `json:"secondaryLocations"`
			PublishedAt      string `json:"publishedAt"`
			IsListed         *bool  `json:"isListed"`
			IsRemote         *bool  `json:"isRemote"`
			JobURL           string `json:"jobUrl"`
			ApplyURL         string `json:"applyUrl"`
			DescriptionHTML  string `json:"descriptionHtml"`
			DescriptionPlain string `json:"descriptionPlain"`
			Compensation     *struct {
				TierSummary       string `json:"compensationTierSummary"`
				ScrapeableSummary string `json:"scrapeableCompensationSalarySummary"`
			} `json:"compensation"`
		} `json:"jobs"`
	}
	if err := decode(body, &resp); err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	if resp.Jobs == nil {
		r.pageFailed(fmt.Errorf("ashby board %q: the answer has no jobs list: %s", token, preview(body)))
		return r.finish()
	}
	r.pageOK()
	for _, j := range *resp.Jobs {
		if j.IsListed != nil && !*j.IsListed {
			continue // not shown on the public board
		}
		job := jobs.RawJob{
			Source: "ashby", SourceKind: jobs.EmployerATS, BoardToken: token,
			ExternalID:     j.ID,
			Title:          strings.TrimSpace(j.Title),
			URL:            strings.TrimSpace(j.JobURL),
			ApplyURL:       strings.TrimSpace(j.ApplyURL),
			Remote:         j.IsRemote,
			EmploymentType: strings.TrimSpace(j.EmploymentType),
			Department:     firstNonEmpty(j.Department, j.Team),
			PostedAt:       parseTime(j.PublishedAt),
			FetchedAt:      r.scan.StartedAt,
		}
		job.Locations = addUnique(nil, j.Location)
		for _, s := range j.SecondaryLocations {
			job.Locations = addUnique(job.Locations, s.Location)
		}
		if d := cleanPlain(j.DescriptionPlain); d != "" {
			job.Description = d
		} else {
			job.Description = htmlToText(j.DescriptionHTML)
		}
		if c := j.Compensation; c != nil {
			job.Compensation = firstNonEmpty(c.TierSummary, c.ScrapeableSummary)
		}
		r.scan.Jobs = append(r.scan.Jobs, job)
	}
	return r.finish()
}
