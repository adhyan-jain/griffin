package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
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
