package linkedin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/griffin/core/internal/hands/whatsapp"
)

// Scan is one LinkedIn scan: scroll, keep the real internship posts, and merge them into the shared list.
type Scan struct {
	// Laptop opens the laptop browser (signed in as the owner) and returns its page and a closer. nil or an
	// error means the laptop route is unavailable and the phone is used.
	Laptop     func(ctx context.Context, workspace int) (Page, func(), error)
	Phone      whatsapp.ADB
	Classifier Classifier
	Extractor  Extractor
	Pause      time.Duration // wait between scrolls; 0 uses each scroller's default
}

// Run scrolls for the given time and returns the task's answer. goal is the task's goal as the server
// showed it, which carries the current list; the answer carries the complete updated list.
func (s Scan) Run(ctx context.Context, goal string, minutes, workspace int) (string, error) {
	d := time.Duration(minutes) * time.Minute
	var posts []Post
	var st Stats
	var err error
	via := ""
	if s.Laptop != nil {
		if page, closeFn, lerr := s.Laptop(ctx, workspace); lerr == nil {
			posts, st, err = WebScroller{Page: page, Classifier: s.Classifier, Pause: s.Pause}.Run(ctx, d)
			closeFn()
			via = "the laptop"
		}
	}
	if via == "" {
		if s.Phone == nil {
			return "", fmt.Errorf("neither the laptop browser nor a phone is available for LinkedIn")
		}
		posts, st, err = Scroller{ADB: s.Phone, Classifier: s.Classifier, Pause: s.Pause}.Run(ctx, d)
		via = "the phone"
	}
	if err != nil && len(posts) == 0 {
		return "", err
	}

	var rows []Row
	notInternship := 0
	for _, p := range posts {
		r, xerr := s.Extractor.Extract(ctx, p.Author, p.Text, p.URLs)
		if xerr != nil {
			continue
		}
		if !strings.EqualFold(r.Type, "internship") {
			notInternship++
			continue
		}
		rows = append(rows, r)
	}
	existing := TableInGoal(goal)
	table, added := MergeInto(existing, rows)
	report := fmt.Sprintf("Scrolled LinkedIn on %s for %d minutes. Saw %d posts: %d were ads, %d were not jobs, %d were not real offers, %d were people looking for work. %d real job posts were kept; %d were not internships; %d new internships were added to the list (%d were already in it).",
		via, minutes, st.Seen, st.Ads, st.NotAJob+st.NoAuthor, st.NotReal, st.Seeker, st.Kept, notInternship, added, len(rows)-added)
	if err != nil {
		report += " The scroll stopped early: " + err.Error() + "."
	}
	if added == 0 && existing == "" {
		return report, nil // nothing to put in a list that does not exist yet
	}
	return Answer(report, table), nil
}
