package hands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

type recDriver struct{ did []domain.BrowserStep }

func (r *recDriver) Do(_ context.Context, s domain.BrowserStep) error {
	r.did = append(r.did, s)
	return nil
}
func (r *recDriver) Snapshot(context.Context) (*browser.Page, error) {
	return &browser.Page{URL: "https://mail.example", Title: "Compose", Text: "Hello Ada",
		Elements: []browser.Element{{Ref: "1", Role: "textbox", Name: "Message"}, {Ref: "2", Role: "button", Name: "Send"}}}, nil
}

type moves struct {
	m []browser.Decision
	i int
}

func (s *moves) Next(context.Context, string, *browser.Page, []domain.BrowserStep) (*browser.Decision, error) {
	d := s.m[s.i]
	s.i++
	return &d, nil
}

func browserExec(t *testing.T, drv *recDriver, dec browser.Decider) *Executor {
	t.Helper()
	p := policy.DefaultPolicy()
	return &Executor{
		Gate: gateway.New("laptop", fixedPolicy{p, true}, nil),
		Browser: &BrowserService{
			Open:    func(context.Context) (browser.Driver, func(), error) { return drv, func() {}, nil },
			Decider: dec,
			Stage:   browser.Stage{Dir: t.TempDir()},
		},
	}
}

func browserTask() *domain.Task {
	b, _ := json.Marshal(map[string]any{"requires": []string{"browser"}, "browser": map[string]any{"goal": "reply to Ada", "start_url": "https://mail.example"}})
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "t", Goal: "g", InputPayload: b}
}

func TestBrowserJobStopsBeforeSendingAndSendsOnlyTheApprovedScript(t *testing.T) {
	drv := &recDriver{}
	ex := browserExec(t, drv, &moves{m: []browser.Decision{
		{Step: domain.BrowserStep{Kind: domain.StepType, Ref: "1", Text: "Hi Ada, yes!"}},
		{Step: domain.BrowserStep{Kind: domain.StepClick, Ref: "2", Final: true}},
	}})
	task := browserTask()

	res, err := ex.Execute(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, "AWAITING_APPROVAL", res.Status)
	require.NotNil(t, res.RequiresApproval)
	for _, s := range drv.did {
		require.False(t, s.Final, "nothing may be sent before approval")
	}
	require.True(t, strings.HasPrefix(res.RequiresApproval.ActionType, "host_action:browser:"))
	require.Contains(t, string(res.RequiresApproval.ActionPayload), "Hi Ada, yes!", "the owner must see what will be typed")

	// Re-run with the owner's approval of exactly that script: it replays, sending.
	drv.did = nil
	ctx := worker.WithApprovedActions(context.Background(), []string{res.RequiresApproval.ActionType})
	res2, err := ex.Execute(ctx, task)
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", res2.Status)
	require.Len(t, drv.did, 3) // goto, type, click
	require.True(t, drv.did[2].Final)

	// An approval for some other script does not run this one: the job is prepared again and asks again.
	drv.did = nil
	ex.Browser.Decider = &moves{m: []browser.Decision{
		{Step: domain.BrowserStep{Kind: domain.StepType, Ref: "1", Text: "Different text"}},
		{Step: domain.BrowserStep{Kind: domain.StepClick, Ref: "2", Final: true}},
	}}
	res3, err := ex.Execute(worker.WithApprovedActions(context.Background(), []string{"host_action:browser:deadbeef"}), browserTask())
	require.NoError(t, err)
	require.Equal(t, "AWAITING_APPROVAL", res3.Status)
	for _, s := range drv.did {
		require.False(t, s.Final)
	}
}

func TestBrowserJobWithoutABrowserFailsClearly(t *testing.T) {
	ex := &Executor{Gate: gateway.New("laptop", fixedPolicy{policy.DefaultPolicy(), true}, nil)}
	res, err := ex.Execute(context.Background(), browserTask())
	require.NoError(t, err)
	require.Equal(t, "FAILED", res.Status)
}
