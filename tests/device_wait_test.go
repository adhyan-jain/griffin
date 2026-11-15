package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestStepsWaitingTooLongForADeviceFailWithAReason(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "devwait@example.com")
	repo := env.TaskRepo.(*repository.PostgresTaskRepository)

	mk := func(title, payload string) *domain.Task {
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: u, Title: title, Goal: "g", InputPayload: json.RawMessage(payload)}, nil)
		require.NoError(t, err)
		return task
	}
	old := mk("old whatsapp step", `{"requires":["messaging"]}`)
	fresh := mk("fresh whatsapp step", `{"requires":["messaging"]}`)
	plain := mk("old plain step", `{}`)
	for _, id := range []any{old.ID, plain.ID} {
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET created_at = now() - interval '7 hours' WHERE id = $1`, id)
		require.NoError(t, err)
	}

	ids, err := repo.ExpireStaleDeviceWaits(ctx, time.Duration(domain.DeviceWaitLimit)*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []any{old.ID}, []any{ids[0]}, "only the old step that needs a device")
	assert.Len(t, ids, 1)

	got, _ := env.Engine.GetTask(ctx, old.ID)
	assert.Equal(t, domain.TaskStatusFailed, got.Status)
	require.NotNil(t, got.FailureReason)
	assert.Contains(t, *got.FailureReason, "Start the host")
	assert.NotNil(t, got.CompletedAt)
	for _, keep := range []*domain.Task{fresh, plain} {
		g, _ := env.Engine.GetTask(ctx, keep.ID)
		assert.Equal(t, domain.TaskStatusQueued, g.Status, keep.Title)
	}
	ev, _ := env.TaskRepo.GetEvents(ctx, old.ID)
	found := false
	for _, e := range ev {
		found = found || e.EventType == "STATUS_CHANGE"
	}
	assert.True(t, found, "the failure is recorded in the task's activity")

	again, err := repo.ExpireStaleDeviceWaits(ctx, time.Duration(domain.DeviceWaitLimit)*time.Second)
	require.NoError(t, err)
	assert.Empty(t, again, "running it again changes nothing")
}
