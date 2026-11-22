package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// autoArchiveEvery bounds how often one process looks for tasks to archive.
const autoArchiveEvery = time.Hour

// AutoArchiver archives a user's old, finished, minor tasks. What to archive is decided by domain.AutoArchiveIDs;
// this reads the candidates, asks it, and archives the answer in one statement. It is cheap to call often: it does
// the work at most once an hour per user, and archiving twice changes nothing.
type AutoArchiver struct {
	Store repository.TaskArchiver
	Now   func() time.Time

	mu   sync.Mutex
	last map[uuid.UUID]time.Time
}

func (a *AutoArchiver) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// due reports whether this user's hour is up, and starts the next one.
func (a *AutoArchiver) due(userID uuid.UUID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last == nil {
		a.last = map[uuid.UUID]time.Time{}
	}
	now := a.now()
	if at, ok := a.last[userID]; ok && now.Sub(at) < autoArchiveEvery {
		return false
	}
	a.last[userID] = now
	return true
}

// Run archives what is due for the user, if their hour is up. It returns how many tasks it archived.
func (a *AutoArchiver) Run(ctx context.Context, userID uuid.UUID) (int64, error) {
	if a == nil || a.Store == nil || !a.due(userID) {
		return 0, nil
	}
	tasks, err := a.Store.ArchiveCandidates(ctx, userID)
	if err != nil {
		return 0, err
	}
	ids := domain.AutoArchiveIDs(tasks, a.now())
	if len(ids) == 0 {
		return 0, nil
	}
	return a.Store.ArchiveTasks(ctx, userID, ids, a.now())
}
