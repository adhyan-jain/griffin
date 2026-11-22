package service

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
)

// ListingDetailsTitle is the routine that reads the detail pages of listings that have no description, in code.
const ListingDetailsTitle = domain.ListingDetailsTitle

// IsListingDetails reports whether a task is that routine.
func IsListingDetails(title string) bool {
	return strings.Contains(strings.ToUpper(title), ListingDetailsTitle)
}

// DetailStore is what the detail reader needs from the database.
type DetailStore interface {
	ListingsNeedingDetail(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.Listing, error)
	SaveDetail(ctx context.Context, userID, id uuid.UUID, description, hash string, deadline *time.Time) error
	AddSnapshot(ctx context.Context, s *domain.JobSnapshot) error
	RecordScan(ctx context.Context, s *domain.SourceScan) error
}

// DetailsFor is the detail reader for a listings store, or nil when the store cannot hold descriptions.
func DetailsFor(l ListingStore) *ListingDetails {
	if st, ok := l.(DetailStore); ok {
		return &ListingDetails{Store: st}
	}
	return nil
}

const (
	// detailCandidates is how many listings without a description one run looks at.
	detailCandidates = 300
	// detailThrottle is the least time between two requests to one site.
	detailThrottle = time.Second
	// detailTarget is the target of the scan that records one run of one site's detail reads.
	detailTarget = "listing details"
)

// ListingDetails fills in the description and deadline of aggregator listings from their own detail pages, newest
// first, by code alone. A listing's description is the page's own text, so the assessment can quote it; a page that
// cannot be read is recorded as a failed scan and left for a day, and never stops the run.
type ListingDetails struct {
	Store DetailStore
	// Get fetches a page; nil means the network.
	Get sources.Get
	Now func() time.Time
	// Throttle is the least time between two requests to one site; zero means one second.
	Throttle time.Duration
}

// DetailResult counts what one run did.
type DetailResult struct {
	Saved, Failed, Left int
	BySite              map[string]int
	Problems            []string
}

// Summary is the owner-facing line for a run.
func (r DetailResult) Summary() string {
	if r.Saved+r.Failed+r.Left == 0 {
		return "Every open listing already has its description."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Read the detail pages of %d listings", r.Saved)
	if len(r.BySite) > 0 {
		var parts []string
		for s, n := range r.BySite {
			parts = append(parts, fmt.Sprintf("%s %d", s, n))
		}
		sort.Strings(parts)
		b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	b.WriteString(": their descriptions and deadlines are saved, and the assessment re-reads them on its next run.")
	if r.Failed > 0 {
		fmt.Fprintf(&b, " %d pages could not be read and are tried again tomorrow.", r.Failed)
	}
	if r.Left > 0 {
		fmt.Fprintf(&b, " %d listings are left for the next run.", r.Left)
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n\nProblems: " + strings.Join(r.Problems, "; ") + ".")
	}
	return b.String()
}

// Run reads the detail pages of the owner's listings that have none, at most max of them (zero: as many as the time
// allows). It stops starting pages when little time is left on ctx.
func (d *ListingDetails) Run(ctx context.Context, userID uuid.UUID, max int) (DetailResult, error) {
	res := DetailResult{BySite: map[string]int{}}
	if d == nil || d.Store == nil {
		return res, fmt.Errorf("listing details: no store")
	}
	now, throttle := d.Now, d.Throttle
	if now == nil {
		now = time.Now
	}
	if throttle <= 0 {
		throttle = detailThrottle
	}
	todo, err := d.Store.ListingsNeedingDetail(ctx, userID, detailCandidates)
	if err != nil {
		return res, err
	}
	work, cancel := budget.Work(ctx)
	defer cancel()
	type tally struct{ saved, failed int }
	sites := map[string]*tally{}
	lastAt := map[string]time.Time{} // per host: when the last request was sent
	done := 0
	for _, l := range todo {
		if (max > 0 && done >= max) || budget.Short(work) {
			break
		}
		if !sources.HasDetail(l.Source) {
			done++
			continue
		}
		if host := hostOf(l.URL); host != "" {
			if wait := throttle - time.Since(lastAt[host]); !lastAt[host].IsZero() && wait > 0 {
				select {
				case <-work.Done():
				case <-time.After(wait):
				}
				if budget.Short(work) {
					break
				}
			}
			lastAt[host] = time.Now()
		}
		done++
		t := sites[l.Source]
		if t == nil {
			t = &tally{}
			sites[l.Source] = t
		}
		started := now()
		det, err := sources.FetchDetail(work, d.Get, l.Source, l.URL)
		if err != nil {
			t.failed++
			res.Failed++
			if len(res.Problems) < 5 {
				res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", l.Role, l.Company, err))
			}
			// The failure is recorded against the listing's own link, which keeps it out of the next day's runs.
			_ = d.Store.RecordScan(ctx, &domain.SourceScan{UserID: userID, Source: sources.DetailSource(l.Source), Target: l.URL, Status: "failed",
				Error: err.Error(), StartedAt: started, FinishedAt: now()})
			continue
		}
		hash := domain.DescriptionHash(det.Description)
		if err := d.Store.SaveDetail(ctx, userID, l.ID, det.Description, hash, det.Deadline); err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", l.Role, l.Company, err))
			continue
		}
		deadline := l.Deadline
		if det.Deadline != nil {
			deadline = det.Deadline
		}
		// A snapshot records the change, so the history shows when the description first became known.
		if err := d.Store.AddSnapshot(ctx, &domain.JobSnapshot{UserID: userID, ListingID: l.ID, CapturedAt: now(), DescriptionHash: &hash,
			CompensationText: l.CompensationText, Deadline: deadline, Title: l.Role, Location: l.Location}); err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: its history could not be recorded: %v", l.Role, l.Company, err))
		}
		t.saved++
		res.Saved++
		res.BySite[l.Source]++
	}
	res.Left = len(todo) - done
	for site, t := range sites {
		status := "ok"
		switch {
		case t.saved == 0:
			status = "failed"
		case t.failed > 0:
			status = "partial"
		}
		_ = d.Store.RecordScan(ctx, &domain.SourceScan{UserID: userID, Source: sources.DetailSource(site), Target: detailTarget, Status: status,
			JobsSeen: t.saved, Pages: t.saved + t.failed, StartedAt: now(), FinishedAt: now()})
	}
	return res, nil
}

func hostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// RunListingDetails runs the routine for its task, in code. handled is false for any other task.
func (w *TrackerLists) RunListingDetails(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Details == nil || task == nil || !IsListingDetails(task.Title) {
		return "", false
	}
	res, err := w.Details.Run(ctx, task.UserID, 0)
	if err != nil {
		return "", false // the normal path reports and retries it
	}
	return res.Summary(), true
}
