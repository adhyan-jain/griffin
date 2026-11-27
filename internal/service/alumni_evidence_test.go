package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type fakeEvidenceStore struct {
	ids     map[string]uuid.UUID
	checked map[uuid.UUID]string
	applied map[uuid.UUID]*domain.VerifiedFields
}

func (f *fakeEvidenceStore) EvidenceStateByProfile(_ context.Context, _ uuid.UUID, link string) (repository.EvidenceState, error) {
	id, ok := f.ids[domain.CanonicalLinkedInURL(link)]
	if !ok {
		return repository.EvidenceState{}, domain.ErrNotFound
	}
	return repository.EvidenceState{ID: id, Checked: f.checked[id]}, nil
}
func (f *fakeEvidenceStore) MarkEvidenceChecked(_ context.Context, _ uuid.UUID, id uuid.UUID, key, _ string) error {
	f.checked[id] = key
	return nil
}
func (f *fakeEvidenceStore) ApplyVerification(_ context.Context, _ uuid.UUID, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error) {
	f.applied[id] = v
	return &domain.Person{ID: id}, nil
}

func TestTheEvidenceCheckRecordsOnlyQuotesThatAreOnTheirPage(t *testing.T) {
	list := domain.AlumniHeader + "\n" +
		"| Real Quote | 2023 | Beta | SDE | ACM VIT | https://www.linkedin.com/in/real-quote | | not contacted | 2026-11-27 | https://example.org/real | B.Tech CSE, VIT Vellore (2019-2023), ACM VIT core team |\n" +
		"| Made Up | 2023 | Beta | SDE | ACM VIT | https://www.linkedin.com/in/made-up | | not contacted | 2026-11-27 | https://example.org/made-up | B.Tech CSE, VIT Vellore (2019-2023), ACM VIT core team |\n" +
		"| Page Down | 2023 | Beta | SDE | | https://www.linkedin.com/in/page-down | | not contacted | 2026-11-27 | https://example.org/down | B.Tech CSE, VIT Vellore (2019-2023) |\n" +
		"| No Evidence | 2023 | Beta | SDE | | https://www.linkedin.com/in/no-evidence | | not contacted | 2026-11-27 | | |"
	real, madeUp, down, none := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeEvidenceStore{checked: map[uuid.UUID]string{}, applied: map[uuid.UUID]*domain.VerifiedFields{}, ids: map[string]uuid.UUID{
		"https://linkedin.com/in/real-quote": real, "https://linkedin.com/in/made-up": madeUp,
		"https://linkedin.com/in/page-down": down, "https://linkedin.com/in/no-evidence": none,
	}}
	fetched := 0
	fetch := func(_ context.Context, url string) (string, error) {
		fetched++
		switch url {
		case "https://example.org/real":
			return "Team\nReal Quote: B.Tech CSE, VIT Vellore (2019–2023), ACM VIT core team.", nil
		case "https://example.org/made-up":
			return "Team\nMade Up works here.", nil
		}
		return "", errors.New("timeout")
	}
	a := &AlumniPeople{Lists: alumniStore{list}, People: &fakePersonStore{}, Evidence: store, Fetch: fetch}

	res, err := a.CheckEvidence(context.Background(), uuid.New(), 10)
	require.NoError(t, err)
	require.Equal(t, EvidenceResult{Checked: 3, Verified: 1, NotOnPage: 1, Failed: 1}, res)
	require.Contains(t, store.applied, real)
	require.Equal(t, domain.AffiliationConfirmedVellore, *store.applied[real].VITAffiliation)
	require.NotContains(t, store.applied, madeUp, "a quote that is not on its page is never recorded")
	require.NotContains(t, store.checked, down, "a page that could not be opened is tried again")

	// A second run fetches only the page that was down.
	fetched = 0
	res, err = a.CheckEvidence(context.Background(), uuid.New(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, fetched)
	require.Equal(t, 1, res.Failed)
}

func TestAClaimedEvidenceCheckRunsInCode(t *testing.T) {
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.AlumniEvidenceTitle, Goal: domain.AlumniEvidenceGoal}
	answer, how := (&TrackerLists{}).RunInCode(context.Background(), task, nil, nil)
	require.Equal(t, Answered, how)
	require.Contains(t, answer, "not set up")
}
