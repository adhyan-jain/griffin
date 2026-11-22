package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Navigator is a Page that can also open an address. ChromePage is one.
type Navigator interface {
	Navigate(ctx context.Context, address string) error
}

// peopleScript reads the people-search page and returns {url, text, cards}. It reads only: it clicks nothing.
// Selectors are deliberately few, because LinkedIn's markup changes: a card is whatever holds an anchor whose href
// contains "/in/" (the nearest list item when it holds one person, else the widest ancestor holding only that
// person's links). Each card is returned as its raw lines, and Go (ParsePeoplePage) decides which line is what.
const peopleScript = `(() => {
  const root = document.querySelector('main') || document.body;
  const handle = a => { const m = (a.getAttribute('href') || '').match(/\/in\/([^/?#]+)/); return m ? m[1].toLowerCase() : ''; };
  const others = (el, h) => Array.from(el.querySelectorAll('a[href*="/in/"]')).some(x => handle(x) && handle(x) !== h);
  const lines = el => (el.innerText || '').split('\n').map(s => s.trim()).filter(Boolean);
  const cards = new Map();
  for (const a of root.querySelectorAll('a[href*="/in/"]')) {
    const h = handle(a);
    if (!h) continue;
    let card = a.closest('li, [role="listitem"]');
    if (!card || others(card, h)) {
      card = a;
      while (card.parentElement && card.parentElement !== root && !others(card.parentElement, h)) card = card.parentElement;
    }
    const name = lines(a)[0] || '';
    const prev = cards.get(h);
    if (!prev || (!prev.name && name)) cards.set(h, {url: a.href, name, lines: lines(card).slice(0, 14)});
  }
  return {url: location.href, text: (document.body.innerText || '').slice(0, 1500), cards: Array.from(cards.values())};
})()`

// scrollScript moves the page down so the cards LinkedIn draws late are on the page when it is read.
const scrollScript = `window.scrollTo(0, document.body.scrollHeight); null`

// peoplePage is what peopleScript returns.
type peoplePage struct {
	URL   string `json:"url"`
	Text  string `json:"text"`
	Cards []struct {
		URL   string   `json:"url"`
		Name  string   `json:"name"`
		Lines []string `json:"lines"`
	} `json:"cards"`
}

var (
	degreeLine = regexp.MustCompile(`(?i)^[•·\s]*(1st|2nd|3rd\+?|connect|message|follow|following|pending|premium)$`)
	noiseLine  = regexp.MustCompile(`(?i)(\bmutual connections?\b[^|]*$|^[\d,.kKmM+]+ followers?$|^status is )`)
	snippetLn  = regexp.MustCompile(`(?i)^(current|past|summary|skills?):`)
	viewName   = regexp.MustCompile(`(?i)^view\s+|[’']s\s+(graphic\s+)?link(ed)?in\s+profile$|[’']s profile$`)
	degreeTail = regexp.MustCompile(`\s*[•·]\s*(1st|2nd|3rd\+?)\s*$`)
)

// ParsePeoplePage reads peopleScript's JSON into the page's address, its start of text and its result cards.
// Each card's lines are sorted into a name, a headline (the first line after the name), a location (the next, when
// it is not a "Current:"/"Past:" line) and the rest as the snippet. Lines that are only buttons or degrees ("• 2nd",
// "Connect") are dropped. Nothing is guessed: a card with no lines has an empty headline.
func ParsePeoplePage(raw json.RawMessage) (address, text string, hits []domain.PersonHit, err error) {
	var p peoplePage
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", nil, fmt.Errorf("could not read the people search page: %w", err)
	}
	for _, c := range p.Cards {
		name := degreeTail.ReplaceAllString(strings.TrimSpace(viewName.ReplaceAllString(strings.TrimSpace(c.Name), "")), "")
		var rest []string
		for _, l := range c.Lines {
			l = strings.TrimSpace(l)
			if l == "" || degreeLine.MatchString(l) || noiseLine.MatchString(l) {
				continue
			}
			if name == "" {
				name = degreeTail.ReplaceAllString(l, "")
				continue
			}
			if strings.EqualFold(degreeTail.ReplaceAllString(l, ""), name) {
				continue // the name again (LinkedIn repeats it for screen readers)
			}
			rest = append(rest, l)
		}
		h := domain.PersonHit{Name: name, URL: c.URL}
		if len(rest) > 0 && !snippetLn.MatchString(rest[0]) {
			h.Headline, rest = rest[0], rest[1:]
		}
		if len(rest) > 0 && !snippetLn.MatchString(rest[0]) {
			h.Location, rest = rest[0], rest[1:]
		}
		h.Snippet = strings.Join(rest, "\n")
		hits = append(hits, h)
	}
	return p.URL, p.Text, hits, nil
}

// Wall says why LinkedIn has stopped the search ("" when it has not): a sign-in page, a checkpoint, or a notice
// about unusual activity or a search limit. The search stops at the first sign of any of them.
func Wall(address, text string) string {
	a := strings.ToLower(address)
	for _, p := range []string{"/login", "/authwall", "/checkpoint", "/uas/", "/signup"} {
		if strings.Contains(a, p) {
			return "LinkedIn sent the browser to " + strings.SplitN(strings.TrimPrefix(address, "https://www.linkedin.com"), "?", 2)[0]
		}
	}
	t := strings.ToLower(text)
	for _, p := range []string{"unusual activity", "quick security check", "security verification", "not a robot", "verify you're a human", "temporarily restricted", "commercial use limit", "monthly limit", "weekly limit", "search limit"} {
		if strings.Contains(t, p) {
			return "LinkedIn says \"" + p + "\""
		}
	}
	return ""
}

// ErrNoPeople is the error when a whole search read no result cards. Trying again would not help.
var ErrNoPeople = errors.New("LinkedIn showed no people")

// NeedsYou is the error a search ends with when LinkedIn shows a wall: the owner must look at their account.
type NeedsYou struct{ Why string }

func (e *NeedsYou) Error() string {
	return "needs you: LinkedIn stopped the people search (" + e.Why + "). Open LinkedIn in your own browser, finish any check it shows, and run the Alumni search again later. Griffin stopped at once and did not try again."
}

// PeopleSearch runs the Alumni people search on the laptop's browser.
type PeopleSearch struct {
	// Open opens the signed-in laptop browser and returns its page and a closer (OpenLaptop, bound to its config).
	Open func(ctx context.Context, workspace int) (Page, func(), error)
	// Pause waits between page loads. nil waits a random 4 to 8 seconds.
	Pause func(ctx context.Context) bool
	// Now is the clock (the day picks the queries and dates the rows). nil is time.Now.
	Now func() time.Time
}

// searchAddress is the people-search page for a query; page 1 has no page parameter.
func searchAddress(query string, page int) string {
	a := "https://www.linkedin.com/search/results/people/?keywords=" + url.QueryEscape(query) + "&origin=GLOBAL_SEARCH_HEADER"
	if page > 1 {
		a += fmt.Sprintf("&page=%d", page)
	}
	return a
}

func randomPause(ctx context.Context) bool {
	return sleep(ctx, 4*time.Second+time.Duration(rand.Int63n(int64(4*time.Second))))
}

// Run searches each query for up to domain.MaxPeoplePages pages and returns the task's answer: a short report and,
// when anyone qualified, the new people as an Alumni table under "## Tracker" (the server merges it into the list and
// drops anyone already there). It only reads. It returns *NeedsYou at the first sign of a wall.
func (s PeopleSearch) Run(ctx context.Context, p *domain.LinkedInPeople) (string, error) {
	if s.Open == nil {
		return "", fmt.Errorf("this host has no laptop browser for LinkedIn")
	}
	pause, now := s.Pause, time.Now
	if pause == nil {
		pause = randomPause
	}
	if s.Now != nil {
		now = s.Now
	}
	page, closeFn, err := s.Open(ctx, p.Workspace)
	if err != nil {
		return "", fmt.Errorf("could not open LinkedIn on the laptop: %w", err)
	}
	defer closeFn()
	nav, ok := page.(Navigator)
	if !ok {
		return "", fmt.Errorf("the laptop browser cannot open pages")
	}

	queries := p.QueriesFor(now())
	var hits []domain.PersonHit
	pages, cards := 0, 0
	for _, q := range queries {
		for pg := 1; pg <= domain.MaxPeoplePages; pg++ {
			if _, st := domain.AlumniRows(hits, now(), p.Max); st.Kept >= p.Max {
				break
			}
			if err := nav.Navigate(ctx, searchAddress(q, pg)); err != nil {
				return "", fmt.Errorf("could not open the LinkedIn people search: %w", err)
			}
			if !pause(ctx) {
				return "", ctx.Err()
			}
			_, _ = page.Eval(ctx, scrollScript)
			if !pause(ctx) { // the cards LinkedIn draws late need a moment after the scroll

				return "", ctx.Err()
			}
			raw, err := page.Eval(ctx, peopleScript)
			if err != nil {
				return "", fmt.Errorf("could not read the people search page: %w", err)
			}
			address, text, found, err := ParsePeoplePage(raw)
			if err != nil {
				return "", err
			}
			pages++
			if why := Wall(address, ""); why != "" {
				return "", &NeedsYou{Why: why}
			}
			if len(found) == 0 {
				// A page with no people may be a notice (a limit, a security check) rather than an empty result.
				if why := Wall(address, text); why != "" {
					return "", &NeedsYou{Why: why}
				}
				break // no results for this query: page 2 would have none either
			}
			cards += len(found)
			hits = append(hits, found...)
		}
	}
	if cards == 0 {
		return "", fmt.Errorf("%w for any of the %d searches: the page did not load, or its layout changed and the reader needs updating", ErrNoPeople, len(queries))
	}
	table, st := domain.AlumniRows(hits, now(), p.Max)
	report := fmt.Sprintf("Searched LinkedIn people for %s (%d pages read). %s Nobody was messaged, connected with or followed.", strings.Join(queryList(queries), "; "), pages, st.Report())
	if table == "" {
		return report + " Nobody new to add today.", nil
	}
	return Answer(report+" Griffin adds the new ones to the Alumni list and skips anyone already on it.", table), nil
}

func queryList(qs []string) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = "\"" + q + "\""
	}
	return out
}
