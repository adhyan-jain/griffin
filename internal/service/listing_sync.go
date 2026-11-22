package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// ListingsSyncTitle is the daily routine that refreshes the verified internship listings, in code.
const ListingsSyncTitle = "LISTINGS SYNC"

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

// ListingSync keeps the verified listings true to the internship sites (Internshala, Unstop, InternDock). Every
// listing it adds was read from a site's own listing and fits the owner (role, place, stipend); every open listing
// whose link stops showing the role is closed. No model is involved.
type ListingSync struct {
	Listings ListingStore
	// Get fetches a page; nil means the network.
	Get   sources.Get
	Check func(ctx context.Context, link string) PostingState
	Now   func() time.Time
}

// ListingSyncResult counts what one sync did.
type ListingSyncResult struct {
	Added, Seen, Closed int
	AddedNames          []string
	Problems            []string
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
	posts, problems := sources.Read(ctx, s.Get)
	res.Problems = problems
	seen := map[string]bool{}
	for _, p := range posts {
		key := strings.ToLower(p.Company + "|" + p.Title)
		if seen[key] || p.Link == "" || p.Company == "" || p.Title == "" || !sources.Fits(p) {
			continue
		}
		seen[key] = true
		created, err := s.Listings.Upsert(ctx, domain.ListingFromPosting(userID, p.Source, sources.Tier(p.Pay), p, now()))
		switch {
		case err != nil:
			res.Problems = append(res.Problems, p.Company+" "+p.Title)
		case created:
			res.Added++
			res.AddedNames = append(res.AddedNames, p.Title+" at "+p.Company+" ("+p.Pay+")")
		default:
			res.Seen++
		}
	}

	// Re-check open listings once a day: a posting that is gone closes its listing, and nothing more is done for it.
	open, err := s.Listings.List(ctx, userID, []string{domain.StageVerified, domain.StageDrafted, domain.StageNeedsYou}, 500)
	if err != nil {
		return res, err
	}
	for _, l := range open {
		if l.LastCheckedAt != nil && now().Sub(*l.LastCheckedAt) < 20*time.Hour {
			continue
		}
		switch check(ctx, l.URL) {
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
