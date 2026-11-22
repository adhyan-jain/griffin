package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// Recruitee reads {company}.recruitee.com/api/offers/. One answer holds every published offer.
type Recruitee struct{ F *Fetcher }

func (Recruitee) Provider() string { return "recruitee" }

func (rc Recruitee) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(rc.F)
	r := f.begin("recruitee", token)
	if token == "" || strings.ContainsAny(token, "/.: ") {
		r.pageFailed(fmt.Errorf("recruitee token %q is not a company subdomain", token))
		return r.finish()
	}
	body, err := f.Get(ctx, "https://"+url.PathEscape(token)+".recruitee.com/api/offers/")
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	var resp struct {
		Offers *[]struct {
			ID           flexString `json:"id"`
			GUID         string     `json:"guid"`
			Status       string     `json:"status"`
			Title        string     `json:"title"`
			CompanyName  string     `json:"company_name"`
			CareersURL   string     `json:"careers_url"`
			ApplyURL     string     `json:"careers_apply_url"`
			Location     string     `json:"location"`
			Locations    []recLoc   `json:"locations"`
			Remote       *bool      `json:"remote"`
			EmployType   string     `json:"employment_type_code"`
			Department   string     `json:"department"`
			Description  string     `json:"description"`
			Requirements string     `json:"requirements"`
			PublishedAt  string     `json:"published_at"`
			CreatedAt    string     `json:"created_at"`
			UpdatedAt    string     `json:"updated_at"`
			CloseAt      string     `json:"close_at"`
			Salary       *struct {
				Min      flexString `json:"min"`
				Max      flexString `json:"max"`
				Period   string     `json:"period"`
				Currency string     `json:"currency"`
			} `json:"salary"`
		} `json:"offers"`
	}
	if err := decode(body, &resp); err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	if resp.Offers == nil {
		r.pageFailed(fmt.Errorf("recruitee %q: the answer has no offers list: %s", token, preview(body)))
		return r.finish()
	}
	r.pageOK()
	for _, o := range *resp.Offers {
		if o.Status != "" && o.Status != "published" {
			continue
		}
		j := jobs.RawJob{
			Source: "recruitee", SourceKind: jobs.EmployerATS, BoardToken: token,
			ExternalID:     firstNonEmpty(string(o.ID), o.GUID),
			EmployerName:   strings.TrimSpace(o.CompanyName),
			Title:          strings.TrimSpace(o.Title),
			URL:            strings.TrimSpace(o.CareersURL),
			ApplyURL:       strings.TrimSpace(o.ApplyURL),
			Remote:         o.Remote,
			EmploymentType: strings.TrimSpace(o.EmployType),
			Department:     strings.TrimSpace(o.Department),
			Description:    joinText(htmlToText(o.Description), htmlToText(o.Requirements)),
			PostedAt:       parseTime(firstNonEmpty(o.PublishedAt, o.CreatedAt)),
			UpdatedAt:      parseTime(o.UpdatedAt),
			Deadline:       parseTime(o.CloseAt),
			FetchedAt:      r.scan.StartedAt,
		}
		for _, l := range o.Locations {
			j.Locations = addUnique(j.Locations, strings.Join(nonEmpty(firstNonEmpty(l.City, l.Name), l.State, l.Country), ", "))
		}
		if len(j.Locations) == 0 {
			j.Locations = addUnique(nil, o.Location)
		}
		if s := o.Salary; s != nil {
			j.Compensation = moneyText(s.Currency, string(s.Min), string(s.Max), s.Period)
		}
		r.scan.Jobs = append(r.scan.Jobs, j)
	}
	return r.finish()
}

type recLoc struct {
	Name    string `json:"name"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}
