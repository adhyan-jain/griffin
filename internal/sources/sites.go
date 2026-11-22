// Package sources reads internship postings from the Indian internship sites (Internshala, Unstop; InternDock is
// read by domain.ParseInternDock). Nothing here is guessed: every posting comes from the site's own listing, with
// its own link and the stipend the site shows.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Site names, recorded as each listing's source.
const (
	Internshala = "internshala"
	Unstop      = "unstop"
	InternDock  = "interndock"
)

// InternshalaPages are the Internshala categories for the owner's roles (software, backend, AI of any sort).
var InternshalaPages = []string{
	"/internships/software-development-internship/",
	"/internships/computer-science-internship/",
	"/internships/backend-development-internship/",
	"/internships/machine-learning-internship/",
	"/internships/artificial-intelligence-ai-internship/",
	"/internships/data-science-internship/",
	"/internships/python-django-internship/",
}

// UnstopSearches are the Unstop search terms for the owner's roles.
var UnstopSearches = []string{"software", "backend", "machine learning", "artificial intelligence", "data science", "sde"}

const (
	internshalaBase = "https://internshala.com"
	unstopSearch    = "https://unstop.com/api/public/opportunity/search-result?opportunity=internships&per_page=50&page=1&searchTerm="
)

// Get fetches a URL's body. It is a parameter so tests need no network.
type Get func(ctx context.Context, url string) ([]byte, error)

// HTTPGet is the real Get.
func HTTPGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Griffin/1.0")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req) // Unstop's open-hackathon search takes 30 seconds on a bad day
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// Read collects the postings from every page of every site. A page that fails is named in problems; the rest
// still count.
func Read(ctx context.Context, get Get) (posts []domain.Posting, problems []string) {
	if get == nil {
		get = HTTPGet
	}
	for _, p := range InternshalaPages {
		body, err := get(ctx, internshalaBase+p)
		if err != nil {
			problems = append(problems, "Internshala "+p)
			continue
		}
		posts = append(posts, ParseInternshala(string(body))...)
	}
	for _, q := range UnstopSearches {
		body, err := get(ctx, unstopSearch+url.QueryEscape(q))
		if err != nil {
			problems = append(problems, "Unstop \""+q+"\"")
			continue
		}
		ps, err := ParseUnstop(body, time.Now())
		if err != nil {
			problems = append(problems, "Unstop \""+q+"\"")
			continue
		}
		posts = append(posts, ps...)
	}
	for _, p := range domain.InternDockPages {
		body, err := get(ctx, domain.InternDockBase+p)
		if err != nil {
			problems = append(problems, "InternDock "+p)
			continue
		}
		posts = append(posts, domain.ParseInternDock(string(body))...)
	}
	return posts, problems
}

var (
	isCardSplit = regexp.MustCompile(`<div class="container-fluid individual_internship `)
	isHref      = regexp.MustCompile(`data-href='([^']+)'`)
	isTitle     = regexp.MustCompile(`class="job-title-href"[^>]*>([^<]+)<`)
	isCompany   = regexp.MustCompile(`(?s)class="company-name">\s*([^<]+?)\s*<`)
	isLocations = regexp.MustCompile(`(?s)class="row-1-item locations">(.*?)</div>`)
	isLink      = regexp.MustCompile(`<a[^>]*>([^<]+)</a>`)
	isStipend   = regexp.MustCompile(`class='stipend'>([^<]+)<`)
)

// ParseInternshala reads the internship cards on an Internshala category page.
func ParseInternshala(page string) []domain.Posting {
	var out []domain.Posting
	for _, card := range isCardSplit.Split(page, -1)[1:] {
		href, title := isHref.FindStringSubmatch(card), isTitle.FindStringSubmatch(card)
		if href == nil || title == nil {
			continue
		}
		p := domain.Posting{Title: clean(title[1]), Link: internshalaBase + href[1], Source: Internshala}
		if c := isCompany.FindStringSubmatch(card); c != nil {
			p.Company = clean(c[1])
		}
		if l := isLocations.FindStringSubmatch(card); l != nil {
			var places []string
			for _, a := range isLink.FindAllStringSubmatch(l[1], -1) {
				places = append(places, clean(a[1]))
			}
			p.Location = strings.Join(places, ", ")
			if strings.Contains(strings.ToLower(p.Location), "work from home") {
				p.Location = "Remote (India)"
			} else if p.Location != "" {
				p.Location += ", India"
			}
		}
		if s := isStipend.FindStringSubmatch(card); s != nil {
			p.Pay = clean(s[1])
		}
		if !strings.Contains(strings.ToLower(p.Title), "intern") {
			p.Title += " Intern"
		}
		if p.Company != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseUnstop reads an Unstop internship search result. Closed or past-deadline internships are skipped.
func ParseUnstop(body []byte, now time.Time) ([]domain.Posting, error) {
	var r struct {
		Data struct {
			Data []struct {
				Title     string `json:"title"`
				SeoURL    string `json:"seo_url"`
				RegnOpen  int    `json:"regn_open"`
				Status    string `json:"status"`
				EndDate   string `json:"end_date"`
				UpdatedAt string `json:"updated_at"`
				Org       struct {
					Name string `json:"name"`
				} `json:"organisation"`
				Locations []json.RawMessage `json:"locations"`
				Job       *struct {
					Type   string   `json:"type"`
					Min    *float64 `json:"min_salary"`
					Max    *float64 `json:"max_salary"`
					PayIn  string   `json:"pay_in"`
					Paid   string   `json:"paid_unpaid"`
					Hidden bool     `json:"not_disclosed"`
				} `json:"jobDetail"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable Unstop result: %w", err)
	}
	var out []domain.Posting
	for _, it := range r.Data.Data {
		if it.RegnOpen != 1 || (it.Status != "" && it.Status != "LIVE") || it.SeoURL == "" {
			continue
		}
		if end, err := time.Parse(time.RFC3339, it.EndDate); err == nil && end.Before(now) {
			continue
		}
		p := domain.Posting{Title: clean(it.Title), Company: clean(it.Org.Name), Link: it.SeoURL, Source: Unstop}
		var cities []string
		for _, raw := range it.Locations {
			var s string
			var o struct {
				City string `json:"city"`
			}
			if json.Unmarshal(raw, &s) == nil && s != "" {
				cities = append(cities, s)
			} else if json.Unmarshal(raw, &o) == nil && o.City != "" {
				cities = append(cities, o.City)
			}
		}
		switch {
		case len(cities) > 0:
			p.Location = strings.Join(cities, ", ") + ", India"
		case it.Job != nil && it.Job.Type == "wfh":
			p.Location = "Remote (India)"
		default:
			p.Location = "India"
		}
		if j := it.Job; j != nil {
			switch {
			case j.Paid == "unpaid":
				p.Pay = "Unpaid"
			case j.Hidden || j.Max == nil:
			case j.PayIn == "monthly" || j.PayIn == "":
				if j.Min != nil && *j.Min != *j.Max {
					p.Pay = fmt.Sprintf("₹ %s - %s /month", rupees(*j.Min), rupees(*j.Max))
				} else {
					p.Pay = fmt.Sprintf("₹ %s /month", rupees(*j.Max))
				}
			}
		}
		if t, err := time.Parse(time.RFC3339, it.UpdatedAt); err == nil {
			p.Posted = t.UTC()
		}
		out = append(out, p)
	}
	return out, nil
}

func rupees(v float64) string {
	s := strconv.FormatInt(int64(v), 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func clean(s string) string { return strings.Join(strings.Fields(htmlUnescape(s)), " ") }

var entities = strings.NewReplacer("&amp;", "&", "&#039;", "'", "&#39;", "'", "&quot;", "\"", "&lt;", "<", "&gt;", ">", "&nbsp;", " ")

func htmlUnescape(s string) string { return entities.Replace(s) }

var number = regexp.MustCompile(`\d[\d,]*`)

// MonthlyStipend is the highest monthly stipend a pay text shows ("₹ 15,000 - 30,000 /month" is 30000), or 0 when
// it shows none (unpaid, hidden, a lump sum or weekly pay).
func MonthlyStipend(pay string) int {
	if !strings.Contains(strings.ToLower(pay), "/month") {
		return 0
	}
	best := 0
	for _, n := range number.FindAllString(pay, -1) {
		if v, err := strconv.Atoi(strings.ReplaceAll(n, ",", "")); err == nil && v > best {
			best = v
		}
	}
	return best
}

func lowestStipend(pay string) int {
	low := 0
	for _, n := range number.FindAllString(pay, -1) {
		if v, err := strconv.Atoi(strings.ReplaceAll(n, ",", "")); err == nil && (low == 0 || v < low) {
			low = v
		}
	}
	return low
}

// MinStipend is the least a listing must pay a month to be shown: below it are mostly unpaid "training" posts.
const MinStipend = 10000

// Tier ranks a listing by its stipend: A reaches the owner's bar (₹40,000 a month) and starts at ₹15,000 or more
// (so "₹ 4,000 - 40,000" is not A), B reaches ₹15,000, C the rest.
func Tier(pay string) string {
	switch s := MonthlyStipend(pay); {
	case s >= domain.MinInternStipendINR && lowestStipend(pay) >= 15000:
		return "A"
	case s >= 15000:
		return "B"
	default:
		return "C"
	}
}

var internship = regexp.MustCompile(`(?i)\b(intern|interns|internship|trainee)\b`)

// IsInternship reports whether a posting title is an internship.
func IsInternship(title string) bool { return internship.MatchString(title) }

// Fits reports whether a posting is one the owner should see: an internship in a role the owner takes, in a
// place the owner can work, paying at least MinStipend a month. InternDock shows no rupee stipend, so its
// postings are kept on role and place alone.
func Fits(p domain.Posting) bool {
	if !IsInternship(p.Title) || !domain.FitsOwner(p) {
		return false
	}
	return p.Source == InternDock || MonthlyStipend(p.Pay) >= MinStipend
}
