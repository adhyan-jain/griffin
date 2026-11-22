package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/griffin/core/internal/budget"
)

// Hackathon site names, recorded in each row's Source.
const (
	Devpost  = "Devpost"
	UnstopH  = "Unstop"
	Devfolio = "Devfolio"
)

const (
	devpostAPI  = "https://devpost.com/api/hackathons?status[]=open&status[]=upcoming&challenge_type[]=online&order_by=deadline"
	unstopHacks = "https://unstop.com/api/public/opportunity/search-result?opportunity=hackathons&per_page=50&oppstatus=open&page="
	devfolioAPI = "https://api.devfolio.co/api/search/hackathons"
)

// Page limits keep a run small: Devpost shows 9 hackathons a page, Unstop 50.
const (
	devpostPages = 7
	unstopPages  = 4
)

// MinDaysLeft is the least time a hackathon must leave to be worth listing.
const MinDaysLeft = 3

// MaxHackathonRows is how many hackathons the tracker lists, the soonest deadlines first.
const MaxHackathonRows = 40

// Hackathon is one open hackathon read from a site's own listing.
type Hackathon struct {
	Name, Organizer string
	Online, InIndia bool
	Location        string // city or venue, empty for online
	Prize           string
	Themes          []string
	Deadline        time.Time
	Source, Link    string
}

// DaysLeft is the whole days from now to the deadline.
func (h Hackathon) DaysLeft(now time.Time) int {
	return int(h.Deadline.Sub(now).Hours() / 24)
}

// Mode is "Online" or the place it is held at.
func (h Hackathon) Mode() string {
	switch {
	case h.Online && h.Location != "":
		return "Online + " + h.Location
	case h.Online:
		return "Online"
	case h.Location != "":
		return "In person, " + h.Location
	}
	return "In person"
}

// Post sends a JSON body and returns the answer's body. It is a parameter so tests need no network.
type Post func(ctx context.Context, url string, body []byte) ([]byte, error)

// HTTPPost is the real Post.
func HTTPPost(ctx context.Context, u string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Griffin/1.0")
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// ReadHackathons collects the open hackathons from Devpost, Unstop and Devfolio. A page that fails is named in
// problems; the rest still count. The caller filters with FilterHackathons.
func ReadHackathons(ctx context.Context, get Get, post Post, now time.Time) (hs []Hackathon, problems []string) {
	if get == nil {
		get = HTTPGet
	}
	if post == nil {
		post = HTTPPost
	}
	left := 0 // requests not sent because the time budget ran out
	for p := 1; p <= devpostPages; p++ {
		if budget.Short(ctx) {
			left++
			continue
		}
		body, err := get(ctx, devpostAPI+"&page="+strconv.Itoa(p))
		if err != nil {
			problems = append(problems, "Devpost page "+strconv.Itoa(p))
			continue
		}
		page, err := ParseDevpost(body, now)
		if err != nil {
			problems = append(problems, "Devpost page "+strconv.Itoa(p))
			continue
		}
		hs = append(hs, page...)
		if len(page) == 0 {
			break
		}
	}
	for p := 1; p <= unstopPages; p++ {
		if budget.Short(ctx) {
			left++
			continue
		}
		body, err := get(ctx, unstopHacks+strconv.Itoa(p))
		if err != nil {
			problems = append(problems, "Unstop page "+strconv.Itoa(p))
			continue
		}
		page, err := ParseUnstopHackathons(body, now)
		if err != nil {
			problems = append(problems, "Unstop page "+strconv.Itoa(p))
			continue
		}
		hs = append(hs, page...)
	}
	if budget.Short(ctx) {
		left++
	} else {
		body, err := post(ctx, devfolioAPI, []byte(`{"type":"application_open","from":0,"size":30}`))
		if err == nil {
			var page []Hackathon
			if page, err = ParseDevfolio(body, now); err == nil {
				hs = append(hs, page...)
			}
		}
		if err != nil {
			problems = append(problems, "Devfolio")
		}
	}
	if left > 0 {
		problems = append(problems, fmt.Sprintf("%d hackathon pages left for the next run (out of time)", left))
	}
	return hs, problems
}

var (
	devpostEnd = regexp.MustCompile(`^(?:([A-Z][a-z]{2}) )?(\d{1,2}), (\d{4})$`)
	devpostMon = regexp.MustCompile(`^([A-Z][a-z]{2})\b`)
)

// devpostDeadline is the last day of Devpost's "Oct 30 - Dec 04, 2026", "Nov 11 - 30, 2026" or "Dec 31, 2026".
func devpostDeadline(dates string) (time.Time, bool) {
	parts := strings.Split(dates, " - ")
	m := devpostEnd.FindStringSubmatch(strings.TrimSpace(parts[len(parts)-1]))
	if m == nil {
		return time.Time{}, false
	}
	mon := m[1]
	if mon == "" {
		if f := devpostMon.FindStringSubmatch(strings.TrimSpace(parts[0])); f != nil {
			mon = f[1]
		}
	}
	t, err := time.Parse("Jan 2, 2006", mon+" "+m[2]+", "+m[3])
	if err != nil {
		return time.Time{}, false
	}
	return t.Add(23*time.Hour + 59*time.Minute), true
}

// ParseDevpost reads one page of Devpost's hackathon list. Hackathons already past their deadline are skipped.
func ParseDevpost(body []byte, now time.Time) ([]Hackathon, error) {
	var r struct {
		Hackathons []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			State    string `json:"open_state"`
			Location struct {
				Location string `json:"location"`
			} `json:"displayed_location"`
			Dates  string `json:"submission_period_dates"`
			Themes []struct {
				Name string `json:"name"`
			} `json:"themes"`
			Prize      string `json:"prize_amount"`
			Org        string `json:"organization_name"`
			InviteOnly bool   `json:"invite_only"`
		} `json:"hackathons"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable Devpost result: %w", err)
	}
	var out []Hackathon
	for _, it := range r.Hackathons {
		deadline, ok := devpostDeadline(it.Dates)
		if !ok || !deadline.After(now) || it.URL == "" || it.InviteOnly || (it.State != "open" && it.State != "upcoming") {
			continue
		}
		h := Hackathon{Name: clean(it.Title), Organizer: clean(it.Org), Prize: devpostPrize(it.Prize), Deadline: deadline, Source: Devpost, Link: it.URL}
		place := clean(it.Location.Location)
		if strings.Contains(strings.ToLower(place), "online") {
			h.Online = true
			if strings.ToLower(place) != "online" {
				h.Location = place
			}
		} else {
			h.Location = place
		}
		h.InIndia = IndianPlace(place)
		for _, t := range it.Themes {
			h.Themes = append(h.Themes, t.Name)
		}
		out = append(out, h)
	}
	return out, nil
}

// devpostPrize turns "$<span data-currency-value>100,000</span>" into "$100,000"; a zero prize is "".
func devpostPrize(s string) string {
	s = clean(htmlTag.ReplaceAllString(s, ""))
	if n := number.FindString(s); n == "" || strings.Trim(n, "0,") == "" {
		return ""
	}
	return s
}

// ParseUnstopHackathons reads one page of Unstop's hackathon search. Closed or past-deadline ones are skipped.
func ParseUnstopHackathons(body []byte, now time.Time) ([]Hackathon, error) {
	var r struct {
		Data struct {
			Data []struct {
				Title    string `json:"title"`
				SeoURL   string `json:"seo_url"`
				RegnOpen int    `json:"regn_open"`
				Status   string `json:"status"`
				Region   string `json:"region"`
				EndDate  string `json:"end_date"`
				Org      struct {
					Name string `json:"name"`
				} `json:"organisation"`
				Address struct {
					City    string `json:"city"`
					Country *struct {
						Name string `json:"name"`
					} `json:"country"`
				} `json:"address_with_country_logo"`
				Regn struct {
					End string `json:"end_regn_dt"`
				} `json:"regnRequirements"`
				Prizes []struct {
					Cash     *float64 `json:"cash"`
					Currency string   `json:"currency"`
				} `json:"prizes"`
				Skills []struct {
					Skill string `json:"skill"`
				} `json:"required_skills"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable Unstop hackathon result: %w", err)
	}
	var out []Hackathon
	for _, it := range r.Data.Data {
		if it.RegnOpen != 1 || (it.Status != "" && it.Status != "LIVE") || it.SeoURL == "" {
			continue
		}
		end := it.Regn.End
		if end == "" {
			end = it.EndDate
		}
		deadline, err := time.Parse(time.RFC3339, end)
		if err != nil || !deadline.After(now) {
			continue
		}
		h := Hackathon{Name: clean(it.Title), Organizer: clean(it.Org.Name), Deadline: deadline.UTC(), Source: UnstopH, Link: it.SeoURL}
		h.Online = it.Region == "online" || it.Region == "hybrid"
		if it.Region != "online" {
			h.Location = clean(it.Address.City)
		}
		// Unstop is an Indian site: a venue with no country is in India.
		h.InIndia = it.Address.Country == nil || strings.EqualFold(it.Address.Country.Name, "India")
		var cash float64
		cur := ""
		for _, p := range it.Prizes {
			if p.Cash != nil && *p.Cash > 0 {
				cash += *p.Cash
				cur = p.Currency
			}
		}
		if cash > 0 {
			h.Prize = map[string]string{"fa-rupee": "₹", "fa-dollar": "$", "fa-euro": "€"}[cur] + rupees(cash)
		}
		for _, s := range it.Skills {
			h.Themes = append(h.Themes, s.Skill)
		}
		out = append(out, h)
	}
	return out, nil
}

var devfolioPrize = regexp.MustCompile(`(?:₹|Rs\.?\s?|INR\s?|\$)\s?\d[\d,]*(?:\.\d+)?(?:\s?[kKlL]|\s?lakh)?`)

// ParseDevfolio reads Devfolio's search of hackathons whose applications are open.
func ParseDevfolio(body []byte, now time.Time) ([]Hackathon, error) {
	var r struct {
		Hits struct {
			Hits []struct {
				Source struct {
					Name    string `json:"name"`
					Slug    string `json:"slug"`
					Tagline string `json:"tagline"`
					Online  bool   `json:"is_online"`
					Private bool   `json:"private"`
					City    string `json:"city"`
					Country string `json:"country"`
					Themes  []struct {
						Name string `json:"name"`
					} `json:"themes"`
					Prizes []struct {
						Name string `json:"name"`
						Desc string `json:"desc"`
					} `json:"prizes"`
					Setting struct {
						RegEnds string `json:"reg_ends_at"`
					} `json:"hackathon_setting"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable Devfolio result: %w", err)
	}
	var out []Hackathon
	for _, hit := range r.Hits.Hits {
		s := hit.Source
		deadline, err := time.Parse(time.RFC3339, s.Setting.RegEnds)
		if err != nil || !deadline.After(now) || s.Slug == "" || s.Private {
			continue
		}
		h := Hackathon{Name: clean(s.Name), Deadline: deadline.UTC(), Online: s.Online, Source: Devfolio, Link: "https://" + s.Slug + ".devfolio.co/"}
		if !s.Online {
			h.Location = clean(s.City)
		}
		h.InIndia = s.Country == "" || strings.EqualFold(s.Country, "India")
		for _, t := range s.Themes {
			h.Themes = append(h.Themes, t.Name)
		}
		if s.Tagline != "" {
			h.Themes = append(h.Themes, clean(s.Tagline))
		}
		for _, p := range s.Prizes {
			if m := devfolioPrize.FindString(p.Desc); m != "" {
				h.Prize = clean(m)
				break
			}
		}
		out = append(out, h)
	}
	return out, nil
}

var indianPlaces = regexp.MustCompile(`(?i)\b(india|mumbai|bengaluru|bangalore|delhi|noida|gurugram|gurgaon|hyderabad|chennai|pune|kolkata|ahmedabad|jaipur|coimbatore|kochi|trivandrum|thiruvananthapuram|chandigarh|lucknow|indore|bhopal|nagpur|tamil ?nadu|karnataka|kerala|maharashtra)\b`)

// IndianPlace reports whether a place name is in India.
func IndianPlace(place string) bool { return indianPlaces.MatchString(place) }

var (
	hackNoise = regexp.MustCompile(`(?i)\b(quiz|webinar|workshop|olympiad|essay)\b`)
	// hackKeywords are the owner's fields: AI/ML, LLMs, agents, systems, devtools, open source, backend.
	hackKeywords = regexp.MustCompile(`(?i)(\bai\b|\bml\b|\bllms?\b|\bgenai\b|\bgpt\b|\bagent|\bagentic\b|\bmcp\b|\brag\b|machine learning|artificial intelligence|generative|deep learning|\bnlp\b|natural language|language model|prompt|vector|open[- ]?source|devtools?|developer tool|\bdevops\b|\bbackend\b|back-end|\bapis?\b|\bcloud\b|infrastructure|distributed|\bdatabases?\b|systems?\b|\bsaas\b|\bsdk\b|software|coding|\bclaude\b|\bopenai\b|\bgemini\b)`)
)

// Relevance scores how well a hackathon fits the owner: two points for each distinct keyword in the name or a
// theme, up to eight.
func (h Hackathon) Relevance() int {
	seen := map[string]bool{}
	score := 0
	for _, text := range append([]string{h.Name}, h.Themes...) {
		for _, k := range hackKeywords.FindAllString(text, -1) {
			if k = strings.ToLower(k); !seen[k] {
				seen[k] = true
				score += 2
			}
		}
	}
	if score > 8 {
		score = 8
	}
	return score
}

// FilterHackathons keeps the hackathons the owner should see: not past, at least MinDaysLeft days left, online
// or in India, relevant to the owner's fields. The same link read twice counts once; the soonest deadline is first.
func FilterHackathons(hs []Hackathon, now time.Time) []Hackathon {
	var out []Hackathon
	seen := map[string]bool{}
	for _, h := range hs {
		key := strings.TrimRight(strings.ToLower(h.Link), "/")
		if seen[key] || h.Name == "" || h.Link == "" || h.DaysLeft(now) < MinDaysLeft || !(h.Online || h.InIndia) ||
			hackNoise.MatchString(h.Name) || h.Relevance() < 2 {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Deadline.Before(out[j].Deadline) })
	if len(out) > MaxHackathonRows {
		out = out[:MaxHackathonRows]
	}
	return out
}

// HackathonsListText is the Hackathons tracker written from the filtered rows.
func HackathonsListText(hs []Hackathon, now time.Time) string {
	cell := func(s string) string {
		s = strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " "))
		if s == "" {
			return "-"
		}
		return s
	}
	var b strings.Builder
	b.WriteString("# Hackathons tracker\n\n")
	b.WriteString("| Name | Organizer | Mode & Location | Prize | Themes | Deadline | Days left | Source | Link |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, h := range hs {
		themes := h.Themes
		if len(themes) > 4 {
			themes = themes[:4]
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %d | %s | %s |\n", cell(h.Name), cell(h.Organizer), cell(h.Mode()), cell(h.Prize),
			cell(strings.Join(themes, ", ")), h.Deadline.Format("2006-01-02"), h.DaysLeft(now), h.Source, cell(h.Link))
	}
	return b.String()
}
