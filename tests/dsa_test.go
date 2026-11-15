package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

func TestDsaCoachOnRealPostgres(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "dsa-a@example.com")
	b := CreateTestUser(t, env.DB, "dsa-b@example.com")
	repo := repository.NewDsaRepository(env.DB.Pool)
	loc, _ := time.LoadLocation("Asia/Kolkata")
	clock := time.Date(2026, 11, 14, 8, 0, 0, 0, loc)
	coach := &service.DsaCoach{Store: repo, Now: func() time.Time { return clock }}

	today, err := coach.Today(ctx, a)
	require.NoError(t, err)
	require.Len(t, today, 2)
	assert.Equal(t, "two-sum", today[0].Slug)
	again, _ := coach.Today(ctx, a)
	assert.Equal(t, today[0].Slug, again[0].Slug, "same day, same problems")

	require.NoError(t, coach.MarkSolved(ctx, a, today[0].Slug))
	again, _ = coach.Today(ctx, a)
	require.Len(t, again, 2, "solving early must not hand out a third")

	// Day 2: the unsolved one rests; two new ones are handed out.
	clock = clock.AddDate(0, 0, 1)
	day2, err := coach.Today(ctx, a)
	require.NoError(t, err)
	for _, it := range day2 {
		assert.NotEqual(t, today[1].Slug, it.Slug)
	}
	items, _ := repo.Items(ctx, a)
	var missed *domain.DsaItem
	for _, it := range items {
		if it.Slug == today[1].Slug {
			missed = it
		}
	}
	require.NotNil(t, missed)
	assert.Equal(t, domain.DsaFailed, missed.State)
	assert.Equal(t, "2026-11-21", missed.DueOn.Format("2006-01-02"), "back a week after it was handed out")

	// Day 8 (the 21st): it is back.
	clock = time.Date(2026, 11, 21, 8, 0, 0, 0, loc)
	day8, _ := coach.Today(ctx, a)
	found := false
	for _, it := range day8 {
		found = found || it.Slug == today[1].Slug
	}
	assert.True(t, found, "the missed problem must return after a week")

	// Another owner's list is untouched.
	other, _ := repo.Items(ctx, b)
	for _, it := range other {
		assert.Equal(t, domain.DsaQueued, it.State)
	}
	users, _ := repo.Users(ctx)
	assert.Contains(t, users, a)

	// Settings validate and persist.
	s := domain.DsaSettings{DailyCount: 3, Timezone: "Asia/Kolkata", LeetcodeUsername: "me", NagFromHour: 20}
	require.NoError(t, s.Validate())
	require.NoError(t, repo.SaveSettings(ctx, a, s))
	got, _ := repo.Settings(ctx, a)
	assert.Equal(t, s, got)
	assert.Error(t, domain.DsaSettings{DailyCount: 0, Timezone: "Asia/Kolkata"}.Validate())
	assert.Error(t, domain.DsaSettings{DailyCount: 2, Timezone: "Mars/Base"}.Validate())
}
