package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type fakePersonStore struct {
	imported [][]domain.AlumniImportRow
	dnc      map[string]bool
	dncErr   error
}

func (f *fakePersonStore) ImportAlumni(_ context.Context, _ uuid.UUID, rows []domain.AlumniImportRow) (repository.ImportResult, error) {
	f.imported = append(f.imported, rows)
	return repository.ImportResult{Created: len(rows)}, nil
}
func (f *fakePersonStore) DecideDraft(context.Context, uuid.UUID, uuid.UUID, bool) error { return nil }
func (f *fakePersonStore) DoNotContactProfiles(context.Context, uuid.UUID) (map[string]bool, error) {
	return f.dnc, f.dncErr
}

func TestAlumniBatchImportsFirstAndNeverProposesADoNotContactPerson(t *testing.T) {
	list := domain.AlumniHeader + "\n" +
		"| Sara Iyer | 2021 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | x | not contacted | 2026-11-20 |\n" +
		"| Meera Das | 2022 | Razorpay | Software Engineer | | https://www.linkedin.com/in/meera | x | not contacted | 2026-11-20 |"
	people := &fakePersonStore{dnc: map[string]bool{"https://www.linkedin.com/in/sara/": true}}
	w := &TrackerLists{Store: alumniStore{list}, Alumni: &AlumniPeople{Lists: alumniStore{list}, People: people}}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.AlumniDMBatchTitle, Goal: domain.AlumniDMBatchGoal}

	answer, ok := w.AlumniDMBatch(context.Background(), task)
	require.True(t, ok)
	require.Len(t, people.imported, 1, "the list is imported at the start of the batch")
	require.Len(t, people.imported[0], 2)
	require.NotContains(t, answer, "Sara Iyer", "a person marked do-not-contact is never proposed")
	require.Contains(t, answer, "Meera Das")

	// if do-not-contact cannot be read, nobody is proposed
	people.dncErr = errors.New("db down")
	answer, ok = w.AlumniDMBatch(context.Background(), task)
	require.True(t, ok)
	require.NotContains(t, answer, "Meera Das")
	require.True(t, strings.Contains(answer, "nobody was proposed"), answer)
}
