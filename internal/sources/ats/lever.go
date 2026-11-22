package ats

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

const leverPage = 100

// Lever reads api.lever.co/v0/postings/{token}?mode=json, page by page (skip/limit). Lever EU boards use
// api.eu.lever.co and are the provider "lever_eu".
type Lever struct {
	F  *Fetcher
	EU bool
}

func (l Lever) Provider() string {
	if l.EU {
		return "lever_eu"
	}
	return "lever"
}

func (l Lever) host() string {
	if l.EU {
		return "api.eu.lever.co"
	}
	return "api.lever.co"
}

type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	ApplyURL   string `json:"applyUrl"`
	CreatedAt  int64  `json:"createdAt"`
	Categories struct {
		Commitment   string   `json:"commitment"`
		Location     string   `json:"location"`
		Team         string   `json:"team"`
		Department   string   `json:"department"`
		AllLocations []string `json:"allLocations"`
	} `json:"categories"`
	WorkplaceType    string `json:"workplaceType"`
	DescriptionPlain string `json:"descriptionPlain"`
	OpeningPlain     string `json:"openingPlain"`
	AdditionalPlain  string `json:"additionalPlain"`
	Lists            []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
	SalaryDescriptionPlain string `json:"salaryDescriptionPlain"`
	SalaryRange            *struct {
		Currency string     `json:"currency"`
		Interval string     `json:"interval"`
		Min      flexString `json:"min"`
		Max      flexString `json:"max"`
	} `json:"salaryRange"`
}

func (l Lever) Read(ctx context.Context, token string) jobs.Scan {
	f := orDefault(l.F)
	r := f.begin(l.Provider(), token)
	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page >= maxPages {
			r.stopped(fmt.Sprintf("stopped after %d pages", maxPages))
			break
		}
		u := fmt.Sprintf("https://%s/v0/postings/%s?mode=json&skip=%d&limit=%d", l.host(), url.PathEscape(token), page*leverPage, leverPage)
		body, err := f.Get(ctx, u)
		if err != nil {
			r.pageFailed(err)
			break
		}
		var list *[]leverPosting
		if err := decode(body, &list); err != nil {
			r.pageFailed(err)
			break
		}
		if list == nil {
			r.pageFailed(fmt.Errorf("lever board %q: the answer is not a list of postings: %s", token, preview(body)))
			break
		}
		posts := *list
		r.pageOK()
		fresh := 0
		for _, p := range posts {
			if p.ID == "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			fresh++
			r.scan.Jobs = append(r.scan.Jobs, l.job(r, token, p))
		}
		// a short page is the last one; a page with nothing new means the server ignored skip/limit
		if len(posts) < leverPage || fresh == 0 {
			break
		}
	}
	return r.finish()
}

func (l Lever) job(r *run, token string, p leverPosting) jobs.RawJob {
	j := jobs.RawJob{
		Source: l.Provider(), SourceKind: jobs.EmployerATS, BoardToken: token,
		ExternalID:     p.ID,
		Title:          strings.TrimSpace(p.Text),
		URL:            strings.TrimSpace(p.HostedURL),
		ApplyURL:       strings.TrimSpace(p.ApplyURL),
		EmploymentType: strings.TrimSpace(p.Categories.Commitment),
		Department:     firstNonEmpty(p.Categories.Team, p.Categories.Department),
		PostedAt:       millisTime(p.CreatedAt),
		FetchedAt:      r.scan.StartedAt,
	}
	j.Locations = addUnique(nil, p.Categories.AllLocations...)
	if len(j.Locations) == 0 {
		j.Locations = addUnique(nil, p.Categories.Location)
	}
	switch strings.ToLower(strings.TrimSpace(p.WorkplaceType)) {
	case "remote":
		j.Remote = ptr(true)
	case "on-site", "onsite", "hybrid":
		j.Remote = ptr(false)
	}
	body := cleanPlain(p.DescriptionPlain)
	if body == "" {
		body = cleanPlain(p.OpeningPlain)
	}
	parts := []string{body}
	for _, li := range p.Lists {
		parts = append(parts, joinText(cleanPlain(li.Text), htmlToText(li.Content)))
	}
	parts = append(parts, cleanPlain(p.AdditionalPlain))
	j.Description = joinText(parts...)
	switch {
	case strings.TrimSpace(p.SalaryDescriptionPlain) != "":
		j.Compensation = cleanPlain(p.SalaryDescriptionPlain)
	case p.SalaryRange != nil:
		j.Compensation = moneyText(p.SalaryRange.Currency, string(p.SalaryRange.Min), string(p.SalaryRange.Max), p.SalaryRange.Interval)
	}
	return j
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
