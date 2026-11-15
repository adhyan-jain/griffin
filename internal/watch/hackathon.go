// Package watch holds the owner's standing look-outs: online hackathons worth entering, and open-source issues
// worth picking up. Everything here only reads from public sources.
package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Hackathon is one opening, in the fields the owner asked to keep.
type Hackathon struct {
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	Prize     string    `json:"prize"`
	CashPrize bool      `json:"cash_prize"`
	Starts    time.Time `json:"starts,omitempty"`
	Ends      time.Time `json:"ends,omitempty"`
	// RegCloses is when entering stops. For Devpost this is the submission deadline, the only date its list gives.
	RegCloses time.Time `json:"reg_closes"`
	TeamMin   int       `json:"team_min,omitempty"`
	TeamMax   int       `json:"team_max,omitempty"`
	Themes    []string  `json:"themes"`
	Link      string    `json:"link"`
	Online    bool      `json:"online"`
	// India says how eligibility for Indian students was established: "yes" (the listing says so), or
	// "unchecked" (the listing does not say; check the page before applying).
	India string `json:"india"`
	// Blurb is the listing's own one-line description, used only to judge the theme.
	Blurb string `json:"blurb,omitempty"`
}

// Key identifies a hackathon across runs.
func (h Hackathon) Key() string { return h.Source + "|" + h.Link }

// MinDaysLeft is how long registration must still be open for a hackathon to be worth reporting.
const MinDaysLeft = 3

// Eligible applies the owner's filter: online, open to Indian students, a cash prize, and registration open for
// at least MinDaysLeft more days.
func (h Hackathon) Eligible(now time.Time) bool {
	return h.Online && h.India != "no" && h.CashPrize && !h.RegCloses.IsZero() &&
		h.RegCloses.Sub(now) >= MinDaysLeft*24*time.Hour
}

var devThemeRe = regexp.MustCompile(`(?i)\b(ai|a\.i\.|artificial intelligence|machine learning|ml|llms?|genai|gen ai|generative|agents?|agentic|deep learning|nlp|computer vision|rag|systems?|distributed|infra|infrastructure|cloud|devops|kubernetes|serverless|database|databases|compilers?|operating systems?|kernel|rust|golang|developer tools?|dev ?tools?|devtools|developer experience|dx|open ?source|sdk|apis?|cli)\b`)

// WantedTheme reports whether the hackathon is about AI, systems or developer tools, judged from its themes,
// name and description. These are the only ones the owner is notified about.
func (h Hackathon) WantedTheme() bool {
	return devThemeRe.MatchString(strings.Join(h.Themes, " ") + " " + h.Name + " " + h.Blurb)
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return strings.TrimSpace(tagRe.ReplaceAllString(s, "")) }

func getJSON(ctx context.Context, hc *http.Client, method, u string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "griffin-watch/1.0")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(out)
}

const maxPages = 6

// ---- Devpost ---------------------------------------------------------------------------------------------------

// FetchDevpost lists open online hackathons with their themes and prizes.
func FetchDevpost(ctx context.Context, hc *http.Client, base string) ([]Hackathon, error) {
	var out []Hackathon
	for page := 1; page <= maxPages; page++ {
		var r struct {
			Hackathons []struct {
				Title      string                  `json:"title"`
				URL        string                  `json:"url"`
				Dates      string                  `json:"submission_period_dates"`
				Prize      string                  `json:"prize_amount"`
				InviteOnly bool                    `json:"invite_only"`
				Themes     []struct{ Name string } `json:"themes"`
				Counts     struct{ Cash int }      `json:"prizes_counts"`
			} `json:"hackathons"`
		}
		u := fmt.Sprintf("%s/api/hackathons?challenge_type[]=online&status[]=open&order_by=deadline&per_page=50&page=%d", strings.TrimRight(base, "/"), page)
		if err := getJSON(ctx, hc, http.MethodGet, u, nil, &r); err != nil {
			if page > 1 {
				break // keep what we have
			}
			return nil, fmt.Errorf("devpost: %w", err)
		}
		for _, h := range r.Hackathons {
			if h.InviteOnly {
				continue
			}
			start, end := ParseDevpostDates(h.Dates)
			x := Hackathon{Source: "devpost", Name: h.Title, Link: h.URL, Online: true, India: "unchecked",
				Starts: start, Ends: end, RegCloses: end, Prize: strings.TrimSpace(stripTags(h.Prize)), CashPrize: h.Counts.Cash > 0}
			for _, t := range h.Themes {
				x.Themes = append(x.Themes, t.Name)
			}
			out = append(out, x)
		}
		if len(r.Hackathons) < 50 {
			break
		}
	}
	return out, nil
}

var devpostDay = regexp.MustCompile(`^\s*(?:([A-Za-z]{3,9})\s+)?(\d{1,2})(?:,\s*(\d{4}))?\s*$`)

// ParseDevpostDates reads "Oct 16 - Nov 15, 2026", "Nov 13 - 15, 2026" and "Dec 30, 2026 - Jan 5, 2027"
// (the end is the day the period closes, taken as the end of that day, UTC).
func ParseDevpostDates(s string) (start, end time.Time) {
	parts := strings.SplitN(s, " - ", 2)
	if len(parts) != 2 {
		return
	}
	pe := devpostDay.FindStringSubmatch(parts[1])
	if pe == nil {
		return
	}
	ps := devpostDay.FindStringSubmatch(parts[0])
	if ps == nil {
		return
	}
	endMonth, year := pe[1], pe[3]
	if year == "" {
		return
	}
	if endMonth == "" {
		endMonth = ps[1]
	}
	startMonth, startYear := ps[1], ps[3]
	if startYear == "" {
		startYear = year
		// A period that starts late in the year and ends early in the next ("Dec 30 - Jan 5, 2027").
		if m1, m2 := monthNum(startMonth), monthNum(endMonth); m1 > m2 {
			startYear = fmt.Sprint(atoi(year) - 1)
		}
	}
	day := func(mon, d, y string, endOfDay bool) time.Time {
		m := monthNum(mon)
		if m == 0 {
			return time.Time{}
		}
		t := time.Date(atoi(y), time.Month(m), atoi(d), 0, 0, 0, 0, time.UTC)
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t
	}
	return day(startMonth, ps[2], startYear, false), day(endMonth, pe[2], year, true)
}

func monthNum(s string) int {
	if len(s) < 3 {
		return 0
	}
	for i, m := range []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"} {
		if strings.EqualFold(s[:3], m) {
			return i + 1
		}
	}
	return 0
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// ---- Devfolio --------------------------------------------------------------------------------------------------

var cashHint = regexp.MustCompile(`(?i)(₹|\brs\.?\s?\d|\binr\b|\$\s?\d|\bcash\b|\bprize pool\b|\bbounty\b|\bbounties\b)`)

// FetchDevfolio lists hackathons whose applications are open. Only online ones are kept.
func FetchDevfolio(ctx context.Context, hc *http.Client, base string) ([]Hackathon, error) {
	var out []Hackathon
	for page := 0; page < maxPages; page++ {
		var r struct {
			Hits struct {
				Hits []struct {
					Source struct {
						Name     string                  `json:"name"`
						Slug     string                  `json:"slug"`
						Tagline  string                  `json:"tagline"`
						Desc     string                  `json:"desc"`
						IsOnline bool                    `json:"is_online"`
						Starts   string                  `json:"starts_at"`
						Ends     string                  `json:"ends_at"`
						TeamMin  int                     `json:"team_min"`
						TeamSize int                     `json:"team_size"`
						Themes   []struct{ Name string } `json:"themes"`
						Setting  struct {
							RegEnds string `json:"reg_ends_at"`
						} `json:"hackathon_setting"`
						Prizes []struct {
							Name string `json:"name"`
							Desc string `json:"desc"`
						} `json:"prizes"`
					} `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		body := map[string]any{"type": "application_open", "from": page * 50, "size": 50}
		if err := getJSON(ctx, hc, http.MethodPost, strings.TrimRight(base, "/")+"/api/search/hackathons", body, &r); err != nil {
			if page > 0 {
				break
			}
			return nil, fmt.Errorf("devfolio: %w", err)
		}
		for _, hit := range r.Hits.Hits {
			s := hit.Source
			if !s.IsOnline {
				continue
			}
			x := Hackathon{Source: "devfolio", Name: s.Name, Link: "https://" + s.Slug + ".devfolio.co", Online: true, India: "unchecked",
				TeamMin: s.TeamMin, TeamMax: s.TeamSize, Blurb: s.Tagline}
			x.Starts, _ = time.Parse(time.RFC3339, s.Starts)
			x.Ends, _ = time.Parse(time.RFC3339, s.Ends)
			x.RegCloses, _ = time.Parse(time.RFC3339, s.Setting.RegEnds)
			for _, t := range s.Themes {
				x.Themes = append(x.Themes, t.Name)
			}
			var names []string
			for _, p := range s.Prizes {
				names = append(names, p.Name)
				if cashHint.MatchString(p.Name + " " + p.Desc) {
					x.CashPrize = true
				}
			}
			if cashHint.MatchString(s.Desc) {
				x.CashPrize = true
			}
			x.Prize = clip(strings.Join(names, ", "), 120)
			if x.Prize == "" && x.CashPrize {
				x.Prize = "cash prizes (see page)"
			}
			out = append(out, x)
		}
		if len(r.Hits.Hits) < 50 {
			break
		}
	}
	return out, nil
}

// ---- Unstop ----------------------------------------------------------------------------------------------------

// FetchUnstop lists open hackathons. Only online ones open to students are kept.
func FetchUnstop(ctx context.Context, hc *http.Client, base string) ([]Hackathon, error) {
	var out []Hackathon
	for page := 1; page <= maxPages; page++ {
		var r struct {
			Data struct {
				Data []struct {
					Title     string `json:"title"`
					PublicURL string `json:"public_url"`
					Region    string `json:"region"`
					Status    string `json:"status"`
					Prizes    []struct {
						Cash     float64 `json:"cash"`
						Currency string  `json:"currencyCode"`
						Rank     string  `json:"rank"`
					} `json:"prizes"`
					Tags   []struct{ Name string } `json:"tags"`
					Skills []struct{ Name string } `json:"required_skills"`
					Regn   struct {
						Start       string `json:"start_regn_dt"`
						End         string `json:"end_regn_dt"`
						MinTeam     int    `json:"min_team_size"`
						MaxTeam     int    `json:"max_team_size"`
						Eligibility string `json:"eligibility"`
					} `json:"regnRequirements"`
				} `json:"data"`
				LastPage int `json:"last_page"`
			} `json:"data"`
		}
		q := url.Values{"opportunity": {"hackathons"}, "per_page": {"50"}, "page": {fmt.Sprint(page)}, "oppstatus": {"open"}}
		if err := getJSON(ctx, hc, http.MethodGet, strings.TrimRight(base, "/")+"/api/public/opportunity/search-result?"+q.Encode(), nil, &r); err != nil {
			if page > 1 {
				break
			}
			return nil, fmt.Errorf("unstop: %w", err)
		}
		for _, it := range r.Data.Data {
			if !strings.EqualFold(it.Region, "online") || !strings.EqualFold(it.Status, "LIVE") {
				continue
			}
			x := Hackathon{Source: "unstop", Name: it.Title, Link: "https://unstop.com/" + strings.TrimLeft(it.PublicURL, "/"), Online: true,
				TeamMin: it.Regn.MinTeam, TeamMax: it.Regn.MaxTeam, India: unstopIndia(it.Regn.Eligibility)}
			x.RegCloses, _ = time.Parse(time.RFC3339, it.Regn.End)
			x.Starts, _ = time.Parse(time.RFC3339, it.Regn.Start)
			var cash []string
			for _, p := range it.Prizes {
				if p.Cash > 0 {
					x.CashPrize = true
					sym := "₹"
					if p.Currency != "" && p.Currency != "INR" {
						sym = p.Currency + " "
					}
					cash = append(cash, fmt.Sprintf("%s%.0f %s", sym, p.Cash, strings.TrimSpace(p.Rank)))
				}
			}
			x.Prize = clip(strings.Join(cash, "; "), 120)
			for _, t := range it.Tags {
				x.Themes = append(x.Themes, t.Name)
			}
			for _, t := range it.Skills {
				x.Themes = append(x.Themes, t.Name)
			}
			out = append(out, x)
		}
		if page >= r.Data.LastPage || len(r.Data.Data) == 0 {
			break
		}
	}
	return out, nil
}

// unstopIndia reads Unstop's eligibility JSON. It is "no" when the listing is for a place outside India or for
// a group that excludes students, and "yes" when students are named and the place is open or India.
func unstopIndia(raw string) string {
	var e struct {
		Sector    []string `json:"sector"`
		Others    []string `json:"others"`
		LocType   string   `json:"location_type"`
		Locations []any    `json:"locations"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &e) != nil {
		return "unchecked"
	}
	students := len(e.Sector) == 0
	for _, s := range append(append([]string{}, e.Sector...), e.Others...) {
		if s == "students" || s == "all" {
			students = true
		}
	}
	if !students {
		return "no"
	}
	if e.LocType == "all" || e.LocType == "" || len(e.Locations) == 0 {
		return "yes"
	}
	if strings.Contains(strings.ToLower(fmt.Sprint(e.Locations)), "india") {
		return "yes"
	}
	return "no"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
