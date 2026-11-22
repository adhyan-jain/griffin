package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// ListingsSyncTitle is the daily routine that refreshes the verified internship listings, in code.
const ListingsSyncTitle = domain.ListingsSyncTitle

// IsListingsSync reports whether a task is that routine.
func IsListingsSync(title string) bool {
	return strings.Contains(strings.ToUpper(title), ListingsSyncTitle)
}

// ListingStore is what the sync and the tools need from the listings table.
type ListingStore interface {
	Upsert(ctx context.Context, l *domain.Listing) (bool, error)
	List(ctx context.Context, userID uuid.UUID, stages []string, limit int) ([]*domain.Listing, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Listing, error)
	Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error
	Checked(ctx context.Context, userID, id uuid.UUID) error
}

// ListingSync keeps the listings true to the internship sites (Internshala, Unstop, InternDock). Each page it reads
// is ingested as one scan (Ingester), so every listing is a lead: it was read from an aggregator, never from the
// employer. Every listing it adds fits the owner's role and place; pay never excludes one. Every open listing whose
// link stops showing the role is closed. No model is involved.
type ListingSync struct {
	Listings ListingStore
	// Ingest records the scans and writes the listings; nil means Listings itself when it is an IngestStore.
	Ingest *Ingester
	// Get fetches a page; nil means the network.
	Get   sources.Get
	Check func(ctx context.Context, link string) PostingState
	Now   func() time.Time
	// Details reads the detail pages of a few listings that have no description once the pages are read; nil skips it.
	Details *ListingDetails
}

// syncDetailCap is the most detail pages one sync reads itself: the LISTING DETAILS routine reads the rest.
const syncDetailCap = 4

// syncPagesShare is the part of the time a sync may spend on reading the listing pages; the rest is for the
// details of new listings and for re-checking open ones.
const syncPagesShare = 0.6

func (s *ListingSync) ingester() (*Ingester, error) {
	if s.Ingest != nil {
		return s.Ingest, nil
	}
	if st, ok := s.Listings.(IngestStore); ok {
		return &Ingester{Store: st, Now: s.Now}, nil
	}
	return nil, fmt.Errorf("listing sync: the listings store cannot ingest scans")
}

// pageLabel names a page that could not be read, for the owner.
func pageLabel(scan jobs.Scan) string {
	switch scan.Source {
	case sources.Unstop:
		return "Unstop \"" + scan.Target + "\""
	case sources.InternDock:
		return "InternDock " + scan.Target
	default:
		return "Internshala " + scan.Target
	}
}

// ListingSyncResult counts what one sync did.
type ListingSyncResult struct {
	Added, Seen, Closed int
	AddedNames          []string
	Problems            []string
	// What the time budget left for the next run: pages not read, re-checks not made, listings without a description.
	PagesLeft, RecheckLeft, DetailsLeft int
	DetailsSaved                        int
}

// Summary is the owner-facing line for a sync.
func (r ListingSyncResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Checked Internshala, Unstop and InternDock: %d new internships that fit you, %d already listed, %d closed because their posting is gone.", r.Added, r.Seen, r.Closed)
	if len(r.AddedNames) > 0 {
		b.WriteString("\n\nNew: " + strings.Join(r.AddedNames, "; ") + ".")
	}
	if r.Added == 0 {
		b.WriteString("\n\nNo new openings today. The list holds only postings read from the sites themselves, so an empty day means they really have none that fit.")
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n\nCould not read: " + strings.Join(r.Problems, "; ") + ".")
	}
	if r.DetailsSaved > 0 {
		fmt.Fprintf(&b, "\n\nRead the detail pages of %d listings for their descriptions.", r.DetailsSaved)
	}
	var left []string
	if r.PagesLeft > 0 {
		left = append(left, fmt.Sprintf("%d pages not read", r.PagesLeft))
	}
	if r.RecheckLeft > 0 {
		left = append(left, fmt.Sprintf("%d link re-checks", r.RecheckLeft))
	}
	if r.DetailsLeft > 0 {
		left = append(left, fmt.Sprintf("%d listings still without a description", r.DetailsLeft))
	}
	if len(left) > 0 {
		b.WriteString("\n\nLeft for the next run (out of time): " + strings.Join(left, ", ") + ".")
	}
	return b.String()
}

// Run syncs one owner's listings.
func (s *ListingSync) Run(ctx context.Context, userID uuid.UUID) (ListingSyncResult, error) {
	var res ListingSyncResult
	if s == nil || s.Listings == nil {
		return res, fmt.Errorf("listing sync: no listings store")
	}
	check, now := s.Check, s.Now
	if check == nil {
		check = func(ctx context.Context, link string) PostingState { return CheckPosting(ctx, nil, link) }
	}
	if now == nil {
		now = time.Now
	}
	ing, err := s.ingester()
	if err != nil {
		return res, err
	}
	// Each page is one scan. A role listed on several pages is counted once. Fetches stop before the deadline so the
	// writes after them fit, and under a deadline the first page read rotates, so the pages a short run never
	// reaches are not always the same ones.
	work, cancelWork := budget.Work(ctx)
	defer cancelWork()
	pagesCtx, cancelPages := budget.Share(work, syncPagesShare)
	defer cancelPages()
	start := 0
	if _, timed := ctx.Deadline(); timed {
		start = int(now().Unix() / 3600)
	}
	seen := map[string]bool{}
	scans, skipped := sources.ReadScans(pagesCtx, s.Get, start)
	res.PagesLeft = skipped
	for _, scan := range scans {
		if scan.Status == jobs.ScanFailed {
			res.Problems = append(res.Problems, pageLabel(scan))
		}
		var fit []jobs.RawJob
		for _, j := range scan.Jobs {
			p := sources.PostingFromRawJob(j)
			key := domain.NormalizeEmployer(p.Company) + "|" + domain.NormalizeRole(p.Title)
			if seen[key] || p.Link == "" || p.Company == "" || p.Title == "" || !sources.Fits(p) {
				continue
			}
			seen[key] = true
			fit = append(fit, j)
		}
		scan.Jobs = fit
		out, err := ing.Ingest(ctx, userID, scan)
		if err != nil {
			return res, err
		}
		res.Problems = append(res.Problems, out.Problems...)
		res.Seen += out.Updated + out.Merged
		for _, l := range out.Created {
			pay := l.Pay
			if pay == "" {
				pay = "pay not stated"
			}
			res.Added++
			res.AddedNames = append(res.AddedNames, l.Role+" at "+l.Company+" ("+pay+")")
		}
	}

	// A few listings that have no description get one from their own page; the LISTING DETAILS routine does the rest.
	if s.Details != nil && !budget.Short(work) {
		if dr, err := s.Details.Run(ctx, userID, syncDetailCap); err == nil {
			res.DetailsSaved, res.DetailsLeft = dr.Saved, dr.Left
		}
	}

	// Re-check open listings once a day: a posting that is gone closes its listing, and nothing more is done for it.
	open, err := s.Listings.List(ctx, userID, []string{domain.StageVerified, domain.StageDrafted, domain.StageNeedsYou}, 500)
	if err != nil {
		return res, err
	}
	for i, l := range open {
		if l.LastCheckedAt != nil && now().Sub(*l.LastCheckedAt) < 20*time.Hour {
			continue
		}
		if budget.Short(work) {
			for _, rest := range open[i:] {
				if rest.LastCheckedAt == nil || now().Sub(*rest.LastCheckedAt) >= 20*time.Hour {
					res.RecheckLeft++
				}
			}
			break
		}
		switch check(work, l.URL) {
		case PostingMissing:
			if s.Listings.Move(ctx, userID, l.ID, l.Stage, domain.StageClosed, "the posting is no longer at its link", nil) == nil {
				res.Closed++
			}
		case PostingOpen:
			_ = s.Listings.Checked(ctx, userID, l.ID)
		}
	}
	return res, nil
}

// SyncListings runs the sync for the routine task, in code. handled is false for any other task.
func (w *TrackerLists) SyncListings(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Sync == nil || task == nil || !IsListingsSync(task.Title) {
		return "", false
	}
	res, err := w.Sync.Run(ctx, task.UserID)
	if err != nil {
		return "", false // the normal path reports and retries it
	}
	if err := w.WriteListingsList(ctx, task.UserID); err != nil {
		return res.Summary() + "\n\nCould not refresh the Internships list: " + err.Error(), true
	}
	return res.Summary(), true
}

// WriteListingsList replaces the Internships list with the verified listings, so every reader of that list (the
// apply batch, the tracker routines, the Trackers page) sees only postings read from the internship sites. Rows
// written by hand or by a model before are dropped.
func (w *TrackerLists) WriteListingsList(ctx context.Context, userID uuid.UUID) error {
	if w == nil || w.Store == nil || w.Sync == nil || w.Sync.Listings == nil {
		return nil
	}
	all, err := w.Sync.Listings.List(ctx, userID, nil, 500)
	if err != nil {
		return err
	}
	text := domain.ListingsListText(all)
	if list := w.list(ctx, userID, storage.ListKind, "internships"); list != nil {
		_, err = w.Store.UpdateText(ctx, list.ID, userID, text, false, trackerReadLimit)
		return err
	}
	_, err = w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: userID, Title: "Internships tracker", Filename: "internships-tracker.md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body:     bytes.NewReader([]byte(text)),
		Metadata: domain.TrackerListMetadata(storage.ListKind, "internships"),
	})
	return err
}
