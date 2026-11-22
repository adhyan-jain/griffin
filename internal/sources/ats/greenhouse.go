package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// Greenhouse reads boards-api.greenhouse.io/v1/boards/{token}/jobs?content=true. The API returns the whole board
// in one answer; the description is entity-escaped HTML.
type Greenhouse struct{ F *Fetcher }

func (Greenhouse) Provider() string { return "greenhouse" }

func (g Greenhouse) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(g.F)
	r := f.begin("greenhouse", token)
	body, err := f.Get(ctx, "https://boards-api.greenhouse.io/v1/boards/"+url.PathEscape(token)+"/jobs?content=true")
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	var resp struct {
		Jobs *[]struct {
			ID          flexString `json:"id"`
			Title       string     `json:"title"`
			AbsoluteURL string     `json:"absolute_url"`
			CompanyName string     `json:"company_name"`
			Location    struct {
				Name string `json:"name"`
			} `json:"location"`
			Content          string `json:"content"`
			UpdatedAt        string `json:"updated_at"`
			FirstPublished   string `json:"first_published"`
			ApplicationDeadl string `json:"application_deadline"`
			Departments      []struct {
				Name string `json:"name"`
			} `json:"departments"`
		} `json:"jobs"`
	}
	if err := decode(body, &resp); err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	if resp.Jobs == nil {
		r.pageFailed(fmt.Errorf("greenhouse board %q: the answer has no jobs list: %s", token, preview(body)))
		return r.finish()
	}
	r.pageOK()
	for _, j := range *resp.Jobs {
		job := jobs.RawJob{
			Source: "greenhouse", SourceKind: jobs.EmployerATS, BoardToken: token,
			ExternalID:   string(j.ID),
			EmployerName: strings.TrimSpace(j.CompanyName),
			Title:        strings.TrimSpace(j.Title),
			URL:          strings.TrimSpace(j.AbsoluteURL),
			Description:  htmlToText(j.Content),
			PostedAt:     parseTime(j.FirstPublished),
			UpdatedAt:    parseTime(j.UpdatedAt),
			Deadline:     parseTime(j.ApplicationDeadl),
			FetchedAt:    r.scan.StartedAt,
		}
		job.Locations = addUnique(nil, j.Location.Name)
		if len(j.Departments) > 0 {
			job.Department = strings.TrimSpace(j.Departments[0].Name)
		}
		r.scan.Jobs = append(r.scan.Jobs, job)
	}
	return r.finish()
}
