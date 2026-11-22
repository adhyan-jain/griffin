package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
)

// IngestStore is what Ingest needs from the database. It is plain reads and writes: every rule lives in Ingest.
type IngestStore interface {
	BackfillIdentity(ctx context.Context, userID uuid.UUID) error
	UpsertCompany(ctx context.Context, c *domain.Company) error
	UpsertBoard(ctx context.Context, b *domain.ATSBoard) error
	RecordBoardScan(ctx context.Context, userID uuid.UUID, provider, token, status string) error
	RecordScan(ctx context.Context, s *domain.SourceScan) error
	FindListing(ctx context.Context, userID uuid.UUID, id domain.Identity) (*domain.Listing, error)
	InsertListing(ctx context.Context, l *domain.Listing) error
	UpdateListing(ctx context.Context, l *domain.Listing) error
	Reopen(ctx context.Context, userID, id uuid.UUID, note string) error
	SetClosedAt(ctx context.Context, userID, id uuid.UUID) error
	OpenFromTarget(ctx context.Context, userID uuid.UUID, source, target string) ([]*domain.Listing, error)
	Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error
	AddSnapshot(ctx context.Context, s *domain.JobSnapshot) error
	AddEvidence(ctx context.Context, e *domain.ListingEvidence) error
}

// Ingester turns what a source scan returned into listings, companies, snapshots and scan history. It is
// incremental and idempotent: ingesting the same scan twice changes nothing the second time except when the
// listings were last seen. No model is involved.
type Ingester struct {
	Store IngestStore
	Now   func() time.Time
	// CloseAggregatorMisses lets a complete scan of an aggregator page close the listings it no longer shows. Off by
	// default: an aggregator page is a window onto a search (the newest N results), so a listing missing from it
	// may only have moved off the page. Aggregator listings are closed by checking their own link instead.
	CloseAggregatorMisses bool
}

// IngestResult counts what one Ingest did.
type IngestResult struct {
	Created   []*domain.Listing // listings first seen in this scan
	Updated   int               // known listings seen again (changed or not)
	Changed   int               // of those, how many had a change recorded as a snapshot
	Merged    int               // records that were the same opportunity as a listing from a better or equal source
	Reopened  int
	Closed    int
	Skipped   int // records without an employer, title or link, which cannot be identified
	Problems  []string
	ScanState domain.SourceScan
}

// Ingest reads one scan. The scan itself is always recorded. A failed scan contributes nothing else. A partial scan
// adds and updates what it read but closes nothing. Only a complete scan may close a listing, and only one that was
// last read from that same source target and is missing from it.
func (in *Ingester) Ingest(ctx context.Context, userID uuid.UUID, scan jobs.Scan) (IngestResult, error) {
	var res IngestResult
	if in == nil || in.Store == nil {
		return res, errors.New("ingest: no store")
	}
	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	status, errText := scan.Status, scan.Error
	switch status {
	case jobs.ScanOK, jobs.ScanPartial, jobs.ScanFailed:
	default:
		// An unknown status is never read as a good scan.
		status, errText = jobs.ScanFailed, strings.TrimSpace("unknown scan status "+fmt.Sprintf("%q", string(scan.Status))+" "+errText)
	}
	scan.Status = status
	started, finished := scan.StartedAt, scan.FinishedAt
	if finished.IsZero() {
		finished = now()
	}
	if started.IsZero() {
		started = finished
	}
	res.ScanState = domain.SourceScan{UserID: userID, Source: scan.Source, Target: scan.Target, Status: string(status), JobsSeen: len(scan.Jobs), Pages: scan.Pages, Error: errText, StartedAt: started, FinishedAt: finished}
	if err := in.Store.RecordScan(ctx, &res.ScanState); err != nil {
		return res, err
	}
	recordBoard := func() {
		if err := in.Store.RecordBoardScan(ctx, userID, scan.Source, scan.Target, string(status)); err != nil {
			res.Problems = append(res.Problems, err.Error())
		}
	}
	if status == jobs.ScanFailed {
		recordBoard()
		return res, nil
	}
	if err := in.Store.BackfillIdentity(ctx, userID); err != nil {
		return res, err
	}

	run := &ingestRun{in: in, userID: userID, scan: scan, now: now(), res: &res, touched: map[uuid.UUID]bool{}, boards: map[string]*domain.ATSBoard{}}
	for _, raw := range scan.Jobs {
		if err := run.job(ctx, raw); err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", raw.Title, raw.EmployerName, err))
			run.incomplete = true
		}
	}
	recordBoard() // after the jobs, so a board first seen in this scan exists to be marked read
	// A listing may be closed only when the whole target was read, and every record in it was handled: a record that
	// failed to save is a listing we did not see, not one that is gone.
	if scan.Complete() && !run.incomplete {
		run.closeMissing(ctx)
	}
	return res, nil
}

type ingestRun struct {
	in         *Ingester
	userID     uuid.UUID
	scan       jobs.Scan
	now        time.Time
	res        *IngestResult
	touched    map[uuid.UUID]bool
	boards     map[string]*domain.ATSBoard
	incomplete bool
}

func trimmedNonEmpty(parts []string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ptrIfSet(s string) *string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return &s
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// verificationFor is employer_verified only for a record from the employer's own board or site that carries a title
// and the link it was fetched from. Everything else is a lead.
func verificationFor(kind string, raw jobs.RawJob) string {
	if domain.IsEmployerKind(kind) && strings.TrimSpace(raw.Title) != "" && strings.TrimSpace(raw.URL) != "" {
		return domain.VerificationEmployer
	}
	return domain.VerificationLead
}

func kindOf(raw jobs.RawJob) (string, bool) {
	switch k := string(raw.SourceKind); k {
	case "":
		return domain.SourceKindAggregator, true // an adapter that does not say is treated as the least trusted
	case domain.SourceKindAggregator, domain.SourceKindEmployerATS, domain.SourceKindEmployerSite:
		return k, true
	}
	return "", false
}

func (r *ingestRun) job(ctx context.Context, raw jobs.RawJob) error {
	title, employer, link := strings.TrimSpace(raw.Title), strings.TrimSpace(raw.EmployerName), strings.TrimSpace(raw.URL)
	if title == "" || employer == "" || link == "" {
		r.res.Skipped++
		return nil
	}
	kind, ok := kindOf(raw)
	if !ok {
		return fmt.Errorf("unknown source kind %q", raw.SourceKind)
	}
	store := r.in.Store
	id := domain.NewIdentity(raw.Source, raw.ExternalID, employer, title, link)
	existing, err := store.FindListing(ctx, r.userID, id)
	if err != nil {
		return err
	}

	// The employer: a candidate when only an aggregator names it, active once its own board or site yields a posting.
	company := &domain.Company{UserID: r.userID, Name: employer, NormalizedName: id.EmployerKey, Kind: "unknown", DiscoveredVia: raw.Source, EvidenceURL: ptrIfSet(link), Status: domain.CompanyCandidate}
	if domain.IsEmployerKind(kind) {
		company.Status = domain.CompanyActive
	}
	if d := strings.ToLower(strings.TrimSpace(raw.EmployerDomain)); d != "" {
		company.Domain = &d
	}
	if err := store.UpsertCompany(ctx, company); err != nil {
		return err
	}
	var boardID *uuid.UUID
	if domain.IsEmployerKind(kind) && domain.ATSProviders[raw.Source] {
		token := strings.TrimSpace(raw.BoardToken)
		if token == "" {
			token = r.scan.Target
		}
		if token != "" {
			key := raw.Source + "|" + token
			b := r.boards[key]
			if b == nil {
				b = &domain.ATSBoard{UserID: r.userID, CompanyID: &company.ID, Provider: raw.Source, TokenOrURL: token}
				if err := store.UpsertBoard(ctx, b); err != nil {
					return err
				}
				r.boards[key] = b
			}
			boardID = &b.ID
		}
	}

	incoming := &domain.Listing{
		UserID: r.userID, Source: raw.Source, ExternalID: strings.TrimSpace(raw.ExternalID), Company: employer, Role: title, URL: link,
		Location: strings.Join(trimmedNonEmpty(raw.Locations), "; "), Pay: strings.TrimSpace(raw.Compensation),
		CompanyID: &company.ID, ATSBoardID: boardID, SourceKind: kind, Verification: verificationFor(kind, raw),
		CanonicalURL: ptrIfSet(id.CanonicalURL), CompensationText: ptrIfSet(raw.Compensation), Deadline: raw.Deadline, PostedAt: raw.PostedAt,
		Description: strings.TrimSpace(raw.Description), DescriptionHash: ptrIfSet(domain.DescriptionHash(raw.Description)),
		EmployerKey: id.EmployerKey, RoleKey: id.RoleKey, SourceTarget: r.scan.Target,
	}
	if domain.IsEmployerKind(kind) {
		incoming.EmployerURL = ptrIfSet(link)
	}
	seenAt := r.now
	if !raw.FetchedAt.IsZero() {
		seenAt = raw.FetchedAt
	}

	if existing == nil {
		incoming.Tier = sources.Tier(incoming.Pay)
		incoming.FirstSeenAt, incoming.LastSeenAt = seenAt, &seenAt
		if err := store.InsertListing(ctx, incoming); err != nil {
			return err
		}
		r.touched[incoming.ID] = true
		r.res.Created = append(r.res.Created, incoming)
		return r.snapshot(ctx, incoming, seenAt)
	}
	r.touched[existing.ID] = true

	// Two sources carry one opportunity: the one closest to the employer is the primary record and supplies the URL.
	takeover := domain.SourceRank(kind) > domain.SourceRank(existing.SourceKind)
	if !takeover && (domain.SourceRank(kind) < domain.SourceRank(existing.SourceKind) || existing.Source != raw.Source) {
		r.res.Merged++
		return nil
	}

	updated := *existing
	if takeover {
		if err := store.AddEvidence(ctx, &domain.ListingEvidence{UserID: r.userID, ListingID: existing.ID, Claim: "lead_url", Value: existing.URL, Quote: existing.Role, SourceURL: existing.URL, CapturedAt: seenAt}); err != nil {
			return err
		}
		updated.Company = employer
	}
	updated.Source, updated.SourceKind, updated.Verification, updated.SourceTarget = raw.Source, kind, incoming.Verification, r.scan.Target
	updated.Role, updated.URL, updated.CanonicalURL = title, link, incoming.CanonicalURL
	updated.EmployerKey, updated.RoleKey = id.EmployerKey, id.RoleKey
	updated.CompanyID = incoming.CompanyID
	if incoming.ExternalID != "" {
		updated.ExternalID = incoming.ExternalID
	}
	if boardID != nil {
		updated.ATSBoardID = boardID
	}
	if incoming.EmployerURL != nil {
		updated.EmployerURL = incoming.EmployerURL
	}
	// A value the source does not give never erases one it gave before: unknown is not "removed".
	if incoming.Location != "" {
		updated.Location = incoming.Location
	}
	if incoming.CompensationText != nil {
		updated.CompensationText, updated.Pay = incoming.CompensationText, incoming.Pay
	}
	if incoming.Deadline != nil {
		updated.Deadline = incoming.Deadline
	}
	if incoming.PostedAt != nil {
		updated.PostedAt = incoming.PostedAt
	}
	if incoming.DescriptionHash != nil {
		updated.DescriptionHash, updated.Description = incoming.DescriptionHash, incoming.Description
	}
	updated.Tier = sources.Tier(updated.Pay)
	updated.LastSeenAt = &seenAt

	changed := existing.Role != updated.Role || existing.Location != updated.Location ||
		!sameString(existing.DescriptionHash, updated.DescriptionHash) || !sameString(existing.CompensationText, updated.CompensationText) ||
		!sameTime(existing.Deadline, updated.Deadline)
	if err := store.UpdateListing(ctx, &updated); err != nil {
		return err
	}
	r.res.Updated++
	if changed {
		r.res.Changed++
		if err := r.snapshot(ctx, &updated, seenAt); err != nil {
			return err
		}
	}
	// A closed listing whose posting is back reopens, unless the owner is the one who closed it.
	if (existing.ClosedAt != nil || existing.Stage == domain.StageClosed) && existing.StageNote != domain.ClosedByOwnerNote {
		if err := store.Reopen(ctx, r.userID, existing.ID, "reopened: the posting is back"); err != nil {
			return err
		}
		r.res.Reopened++
	}
	return nil
}

func (r *ingestRun) snapshot(ctx context.Context, l *domain.Listing, at time.Time) error {
	return r.in.Store.AddSnapshot(ctx, &domain.JobSnapshot{UserID: r.userID, ListingID: l.ID, CapturedAt: at, DescriptionHash: l.DescriptionHash, CompensationText: l.CompensationText, Deadline: l.Deadline, Title: l.Role, Location: l.Location})
}

// closeMissing closes the listings that were last read from this scan's source target and are not in it.
func (r *ingestRun) closeMissing(ctx context.Context) {
	store := r.in.Store
	open, err := store.OpenFromTarget(ctx, r.userID, r.scan.Source, r.scan.Target)
	if err != nil {
		r.res.Problems = append(r.res.Problems, "could not list the open listings of "+r.scan.Target+": "+err.Error())
		return
	}
	note := "the posting is no longer on " + r.scan.Source + " " + r.scan.Target
	for _, l := range open {
		if r.touched[l.ID] || (!domain.IsEmployerKind(l.SourceKind) && !r.in.CloseAggregatorMisses) {
			continue
		}
		if domain.CanMove(l.Stage, domain.StageClosed) {
			if err := store.Move(ctx, r.userID, l.ID, l.Stage, domain.StageClosed, note, nil); err != nil {
				r.res.Problems = append(r.res.Problems, "could not close "+l.Role+" at "+l.Company+": "+err.Error())
				continue
			}
		} else if err := store.SetClosedAt(ctx, r.userID, l.ID); err != nil {
			// The stage may not move (an application already went in): the posting is marked gone, the application kept.
			r.res.Problems = append(r.res.Problems, "could not mark "+l.Role+" at "+l.Company+" gone: "+err.Error())
			continue
		}
		r.res.Closed++
	}
}
