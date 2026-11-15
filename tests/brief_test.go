package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type pushLog struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (p *pushLog) Send(_ context.Context, m notify.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, m)
	return nil
}

func TestMorningBriefOnRealPostgres(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "brief@example.com")
	other := CreateTestUser(t, env.DB, "brief-other@example.com")
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := env.DB.Pool.Exec(ctx, q, args...)
		require.NoError(t, err)
	}

	campaign := uuid.New()
	exec(`INSERT INTO outreach_campaigns (id, user_id, name, limits) VALUES ($1,$2,'jobs','{}')`, campaign, u)
	exec(`INSERT INTO outreach_targets (campaign_id, user_id, name, handle, status, replied_at, reply_text) VALUES ($1,$2,'Priya','priya@acme.com','replied',now(),$3)`,
		campaign, u, "Please finish this test https://app.codility.com/c/run/xyz by Friday.")
	exec(`INSERT INTO outreach_targets (campaign_id, user_id, name, handle, status, sent_at, next_followup_at) VALUES ($1,$2,'Neha','neha@x.com','sent',now(),now() - interval '1 day')`, campaign, u)
	// Someone else's reply must never leak in.
	oc := uuid.New()
	exec(`INSERT INTO outreach_campaigns (id, user_id, name, limits) VALUES ($1,$2,'theirs','{}')`, oc, other)
	exec(`INSERT INTO outreach_targets (campaign_id, user_id, name, handle, status, replied_at, reply_text) VALUES ($1,$2,'Secret','s@x.com','replied',now(),'private')`, oc, other)

	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: u, Title: "Apply to Acme", Goal: "g", MaxRetries: 1, InputPayload: []byte(`{"tool_mode":"act"}`)}, nil)
	require.NoError(t, err)
	w := newTestWorker(env, "w-brief", &scriptedExecutor{})
	_, err = w.ProcessNext(ctx) // stops at its approval
	require.NoError(t, err)
	failed, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: u, Title: "boom job", Goal: "g", MaxRetries: 1}, nil)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		exec(`UPDATE tasks SET scheduled_for = now() - interval '1 second' WHERE status = 'RETRYING'`)
		_, err = w.ProcessNext(ctx)
		require.NoError(t, err)
	}
	_ = task
	_ = failed

	loc, _ := time.LoadLocation("Asia/Kolkata")
	clock := time.Now().In(loc)
	clock = time.Date(clock.Year(), clock.Month(), clock.Day(), 7, 0, 0, 0, loc)
	dsaRepo := repository.NewDsaRepository(env.DB.Pool)
	coach := &service.DsaCoach{Store: dsaRepo, Now: func() time.Time { return clock }}
	push := &pushLog{}
	br := &service.Briefer{Sources: repository.NewReportRepository(env.DB.Pool), Coach: coach, Sender: push, AppURL: "http://app", Now: func() time.Time { return clock }}

	brief, err := br.Build(ctx, u)
	require.NoError(t, err)
	assert.False(t, brief.Empty)
	require.GreaterOrEqual(t, len(brief.Plan), 5, "%v", brief.Plan)
	assert.Contains(t, brief.Plan[0], "Reply from Priya", "the assessment reply is first")
	assert.Contains(t, brief.Plan[0], "https://app.codility.com/c/run/xyz")
	assert.Contains(t, brief.Plan[0], "deadline")
	assert.Contains(t, brief.Text, "Approve or refuse")
	assert.Contains(t, brief.Text, "Follow-up due: Neha")
	assert.Contains(t, brief.Text, "DSA: Two Sum")
	assert.Contains(t, brief.Text, "Failed overnight: boom job")
	assert.NotContains(t, brief.Text, "Secret")
	assert.NotContains(t, brief.Text, "private")

	// Push: nothing at 07:00 local, one at 08:00, not again the same day, again the next day.
	br.Tick(ctx)
	assert.Empty(t, push.msgs, "before the brief hour nothing is sent")
	clock = clock.Add(time.Hour)
	br.Tick(ctx)
	require.Len(t, push.msgs, 1)
	assert.Contains(t, push.msgs[0].Body, "Reply from Priya")
	br.Tick(ctx)
	assert.Len(t, push.msgs, 1, "only once a day")
	clock = clock.AddDate(0, 0, 1)
	br.Tick(ctx)
	assert.Len(t, push.msgs, 2)
}
