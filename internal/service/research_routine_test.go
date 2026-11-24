package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

type fakeRunner struct {
	run *domain.ResearchRun
	adv domain.ResearchAdvance
	err error
}

func (f *fakeRunner) RunSteps(context.Context, uuid.UUID, uuid.UUID) (*domain.ResearchRun, domain.ResearchAdvance, error) {
	return f.run, f.adv, f.err
}

func researchTask(user, run uuid.UUID) *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: user, Title: domain.ResearchRunTitlePrefix + run.String(), Goal: "g"}
}

func TestResearchRunRoutine(t *testing.T) {
	user, runID := uuid.New(), uuid.New()
	w := &TrackerLists{}
	var made []*domain.Task
	create := func(_ context.Context, t *domain.Task) error { made = append(made, t); return nil }

	// other tasks are not ours
	_, ok := w.ResearchRun(context.Background(), &domain.Task{Title: "Research run for my thesis", Goal: "g"}, create)
	require.False(t, ok)
	_, ok = w.ResearchRun(context.Background(), &domain.Task{Title: domain.ResearchRunTitlePrefix + "not-a-uuid", Goal: "g"}, create)
	require.False(t, ok)

	// no engine: answered, never handed to a model
	ans, ok := w.ResearchRun(context.Background(), researchTask(user, runID), create)
	require.True(t, ok)
	require.Contains(t, ans, "not set up")

	// work remains: the next task is created, keyed to where the run stands
	active := &domain.ResearchRun{ID: runID, UserID: user, Objective: "obj", Status: domain.PhaseSourceDiscovery, NextStepIndex: 7, Revision: 12}
	w.ResearchRunner = &fakeRunner{run: active, adv: domain.ResearchAdvance{Steps: 3}}
	ans, ok = w.ResearchRun(context.Background(), researchTask(user, runID), create)
	require.True(t, ok)
	require.Contains(t, ans, "3 steps")
	require.Len(t, made, 1)
	require.Equal(t, domain.ResearchRunTitlePrefix+runID.String(), made[0].Title)
	require.Equal(t, user, made[0].UserID)
	require.Equal(t, "research:"+runID.String()+":7:12", *made[0].IdempotencyKey)

	// the same point asked twice is not an error
	dup := func(context.Context, *domain.Task) error { return domain.ErrIdempotentConflict }
	ans, _ = w.ResearchRun(context.Background(), researchTask(user, runID), dup)
	require.NotContains(t, ans, "could not be created")

	// a failure to create it is said, so the owner can resume the run
	fail := func(context.Context, *domain.Task) error { return errors.New("db down") }
	ans, _ = w.ResearchRun(context.Background(), researchTask(user, runID), fail)
	require.Contains(t, ans, "could not be created")

	// finished, paused and contended runs make no further task
	made = nil
	for name, r := range map[string]*fakeRunner{
		"completed": {run: &domain.ResearchRun{ID: runID, Status: domain.PhaseCompleted, StopReason: domain.StopBudgetPages, UnresolvedQuestions: []string{"a"}}},
		"paused":    {run: &domain.ResearchRun{ID: runID, Status: domain.PhasePaused}},
		"contended": {run: active, adv: domain.ResearchAdvance{Contended: true}},
		"missing":   {err: domain.ErrNotFound},
		"broken":    {err: errors.New("boom")},
	} {
		w.ResearchRunner = r
		ans, ok = w.ResearchRun(context.Background(), researchTask(user, runID), create)
		require.True(t, ok, name)
		require.Empty(t, made, name)
		if name == "completed" {
			require.True(t, strings.Contains(ans, "incomplete"), ans)
		}
	}
}

func TestRunInCodeDispatchesResearchRuns(t *testing.T) {
	user, runID := uuid.New(), uuid.New()
	w := &TrackerLists{ResearchRunner: &fakeRunner{run: &domain.ResearchRun{ID: runID, UserID: user, Status: domain.PhaseCompleted, StopReason: domain.StopAnswered}}}
	ans, how := w.RunInCode(context.Background(), researchTask(user, runID), nil, func(context.Context, *domain.Task) error { return nil })
	require.Equal(t, Answered, how)
	require.Contains(t, ans, "stop reason answered")
}
