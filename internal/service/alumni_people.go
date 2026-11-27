package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/web"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

// PersonStore is the person records the Alumni list is imported into. *repository.PeopleRepository implements it.
type PersonStore interface {
	ImportAlumni(ctx context.Context, userID uuid.UUID, rows []domain.AlumniImportRow) (repository.ImportResult, error)
	DoNotContactProfiles(ctx context.Context, userID uuid.UUID) (map[string]bool, error)
	DecideDraft(ctx context.Context, userID, id uuid.UUID, approved bool) error
}

// AlumniPeople connects the Alumni tracker list to the person records. The list stays where it is; the people are
// one record each, keyed on their profile URL, so importing again changes nothing it already wrote.
type AlumniPeople struct {
	Lists  TrackerStore
	People PersonStore
	Logger *slog.Logger
	// Evidence and Fetch run the quote check (CheckEvidence); without them it is off.
	Evidence EvidenceStore
	Fetch    PageFetcher
}

// OutreachDecided records the owner's decision on an outreach draft on the person: approved, or back to not
// contacted when rejected. It is the whole effect of approving: nothing is sent, and no executor handles an
// alumni_outreach approval. The owner sends the message by hand and records it with PATCH /alumni/{id}.
func (a *AlumniPeople) OutreachDecided(ctx context.Context, app *domain.Approval, approved bool) {
	if a == nil || a.People == nil || app == nil {
		return
	}
	var p struct {
		AlumniID string `json:"alumni_id"`
	}
	if json.Unmarshal(app.ActionPayload, &p) != nil {
		return
	}
	id, err := uuid.Parse(p.AlumniID)
	if err != nil {
		return
	}
	if err := a.People.DecideDraft(ctx, app.UserID, id, approved); err != nil && a.Logger != nil {
		a.Logger.Warn("Could not record the outreach decision", "approval_id", app.ID, "error", err)
	}
}

// listText reads the owner's Alumni list; "" when there is none.
func (a *AlumniPeople) listText(ctx context.Context, userID uuid.UUID) (string, error) {
	list := (&TrackerLists{Store: a.Lists}).list(ctx, userID, storage.ListKind, "alumni")
	if list == nil {
		return "", nil
	}
	return a.Lists.ReadText(ctx, userID, list.ID, trackerReadLimit)
}

// Import writes the Alumni list's rows as person records.
func (a *AlumniPeople) Import(ctx context.Context, userID uuid.UUID) (repository.ImportResult, error) {
	if a == nil || a.Lists == nil || a.People == nil {
		return repository.ImportResult{}, fmt.Errorf("%w: alumni import is not set up", domain.ErrValidation)
	}
	text, err := a.listText(ctx, userID)
	if err != nil {
		return repository.ImportResult{}, fmt.Errorf("read the Alumni list: %w", err)
	}
	return a.People.ImportAlumni(ctx, userID, domain.AlumniImportRows(text))
}

// DoNotContact returns a test for a LinkedIn link: true for a person the owner must never be proposed.
func (a *AlumniPeople) DoNotContact(ctx context.Context, userID uuid.UUID) (func(link string) bool, error) {
	set, err := a.People.DoNotContactProfiles(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Compare in one form, whatever form the store keeps its links in.
	canon := make(map[string]bool, len(set))
	for k, v := range set {
		if c := domain.CanonicalLinkedInURL(k); c != "" && v {
			canon[c] = true
		}
	}
	return func(link string) bool {
		c := domain.CanonicalLinkedInURL(link)
		return c != "" && canon[c]
	}, nil
}

// EvidenceStore is what the quote check reads and writes. *repository.PeopleRepository implements it.
type EvidenceStore interface {
	EvidenceStateByProfile(ctx context.Context, userID uuid.UUID, link string) (repository.EvidenceState, error)
	MarkEvidenceChecked(ctx context.Context, userID, id uuid.UUID, key, result string) error
	ApplyVerification(ctx context.Context, userID, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error)
}

// PageFetcher returns the text of a public page. Production uses hands/web's Fetcher, which refuses private addresses.
type PageFetcher func(ctx context.Context, url string) (string, error)

// EvidenceResult says what one quote check run did.
type EvidenceResult struct {
	Checked, Verified, NotOnPage, Failed, Left int
}

// evidenceReserve is the time a check leaves unused before the caller's deadline, so it finishes cleanly.
const evidenceReserve = 4 * time.Second

// CheckEvidence opens the evidence page of each Alumni list row that names one and records, on the person, only
// what the quote proves, and only when the quote is really on the page (domain.EvidenceVerification). Each page and
// quote pair is checked once; rows it has no time for are counted in Left and checked on the next run. Nothing the
// collector wrote counts as verified without this check.
func (a *AlumniPeople) CheckEvidence(ctx context.Context, userID uuid.UUID, max int) (EvidenceResult, error) {
	var res EvidenceResult
	if a == nil || a.Evidence == nil || a.Fetch == nil {
		return res, fmt.Errorf("%w: the evidence check is not set up", domain.ErrValidation)
	}
	text, err := a.listText(ctx, userID)
	if err != nil {
		return res, fmt.Errorf("read the Alumni list: %w", err)
	}
	deadline, hasDeadline := ctx.Deadline()
	for _, row := range domain.AlumniImportRows(text) {
		if strings.TrimSpace(row.EvidenceURL) == "" || strings.TrimSpace(row.Quote) == "" {
			continue
		}
		st, err := a.Evidence.EvidenceStateByProfile(ctx, userID, row.ProfileURL)
		if err != nil {
			continue // not imported yet: the next run, after an import, checks it
		}
		key := strings.TrimSpace(row.EvidenceURL) + "\n" + strings.TrimSpace(row.Quote)
		if st.Checked == key {
			continue
		}
		if res.Checked >= max || (hasDeadline && time.Until(deadline) < evidenceReserve) {
			res.Left++
			continue
		}
		res.Checked++
		page, err := a.Fetch(ctx, row.EvidenceURL)
		if err != nil {
			res.Failed++ // not marked, so a page that was down is tried again next run
			continue
		}
		result := "not_on_page"
		if v, ok := domain.EvidenceVerification(row, page); ok {
			fields, err := v.Resolve(time.Now())
			if err != nil {
				return res, err
			}
			if _, err := a.Evidence.ApplyVerification(ctx, userID, st.ID, fields); err != nil {
				return res, err
			}
			result = "verified"
			res.Verified++
		} else {
			res.NotOnPage++
		}
		if err := a.Evidence.MarkEvidenceChecked(ctx, userID, st.ID, key, result); err != nil {
			return res, err
		}
	}
	return res, nil
}

// NewAlumniPeople connects the Alumni list to the person records, with the quote check on: evidence pages are read
// with hands/web's Fetcher, which refuses private addresses and caps time and size.
func NewAlumniPeople(lists TrackerStore, people *repository.PeopleRepository) *AlumniPeople {
	f := web.NewFetcher(web.FetcherConfig{Timeout: 8 * time.Second})
	return &AlumniPeople{Lists: lists, People: people, Evidence: people, Fetch: func(ctx context.Context, url string) (string, error) {
		p, err := f.Fetch(ctx, url)
		if err != nil {
			return "", err
		}
		if p.Status >= 400 {
			return "", fmt.Errorf("evidence page answered %d", p.Status)
		}
		return p.Title + "\n" + p.Text, nil
	}}
}
