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
	"github.com/griffin/core/internal/hands/linkedin"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

// peoplePage is a browser that shows the same search page for every address.
type peoplePage struct {
	page   string
	opened int
}

func (p *peoplePage) Navigate(context.Context, string) error { p.opened++; return nil }
func (p *peoplePage) Eval(_ context.Context, script string) (json.RawMessage, error) {
	if strings.Contains(script, "cards") {
		return json.RawMessage(p.page), nil
	}
	return json.RawMessage("null"), nil
}

const okPeoplePage = `{"url":"https://www.linkedin.com/search/results/people/?keywords=x","text":"","cards":[
 {"url":"https://www.linkedin.com/in/riya-sen-1a2b3c","name":"Riya Sen","lines":["Riya Sen","• 2nd","SDE-2 @ Razorpay | VIT '22 | ACM","Bengaluru"]}]}`

func peopleExec(p policy.Policy, page *peoplePage) *Executor {
	return &Executor{
		Gate: gateway.New("laptop", fixedPolicy{p, true}, nil),
		LinkedIn: &LinkedInService{People: linkedin.PeopleSearch{
			Open:  func(context.Context, int) (linkedin.Page, func(), error) { return page, func() {}, nil },
			Pause: func(context.Context) bool { return true },
		}},
	}
}

func peopleTask() *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "ALUMNI OUTREACH TRACKER", Goal: "g",
		InputPayload: json.RawMessage(`{"cadence":"daily","tracker_type":"alumni","linkedin_people":{"max":40,"workspace":8}}`)}
}

func TestAlumniPeopleSearchRunsAsAHandAndReturnsATrackerTable(t *testing.T) {
	page := &peoplePage{page: okPeoplePage}
	res, err := peopleExec(policy.DefaultPolicy(), page).Execute(context.Background(), peopleTask())
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", res.Status, res.Error)
	table, ok := domain.ExtractTrackerTable(res.TextOutput)
	require.True(t, ok, res.TextOutput)
	require.Contains(t, table, "| Riya Sen | 2022 | Razorpay | SDE-2 | ACM VIT | https://www.linkedin.com/in/riya-sen-1a2b3c/ |")
	require.Equal(t, domain.TrackerTypeOf(peopleTask().Title, peopleTask().Goal, peopleTask().InputPayload), "alumni", "the server saves the answer into the Alumni list")
	require.Contains(t, string(res.Output), "response")
}

func TestAlumniPeopleSearchHonoursThePermissionGate(t *testing.T) {
	page := &peoplePage{page: okPeoplePage}
	off := policy.Policy{Default: policy.Ask, Rules: []policy.Rule{{Capability: "browser", App: "linkedin.com", Mode: policy.Off}}}
	res, err := peopleExec(off, page).Execute(context.Background(), peopleTask())
	require.NoError(t, err)
	require.Equal(t, "FAILED", res.Status)
	require.ErrorContains(t, res.Error, "permission settings")
	require.Zero(t, page.opened, "nothing is opened when LinkedIn is switched off")

	ask := policy.Policy{Default: policy.Ask}
	res, err = peopleExec(ask, page).Execute(context.Background(), peopleTask())
	require.NoError(t, err)
	require.Equal(t, "AWAITING_APPROVAL", res.Status)
	require.Zero(t, page.opened, "nothing is opened before the owner approves")
	ctx := worker.WithApprovedActions(context.Background(), []string{res.RequiresApproval.ActionType})
	res, err = peopleExec(ask, page).Execute(ctx, peopleTask())
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", res.Status, res.Error)
}

func TestAlumniPeopleSearchFailsClearlyOnAWallAndDoesNotRetry(t *testing.T) {
	page := &peoplePage{page: `{"url":"https://www.linkedin.com/checkpoint/challenge/x","text":"","cards":[]}`}
	res, err := peopleExec(policy.DefaultPolicy(), page).Execute(context.Background(), peopleTask())
	require.NoError(t, err)
	require.Equal(t, "FAILED", res.Status)
	require.ErrorContains(t, res.Error, "needs you")
	require.False(t, res.Retryable)
	require.Equal(t, 1, page.opened)
}

func TestAlumniPeopleSearchWithoutALaptopBrowserOrWithBadPayloadFails(t *testing.T) {
	ex := &Executor{Gate: gateway.New("laptop", fixedPolicy{policy.DefaultPolicy(), true}, nil), LinkedIn: &LinkedInService{}}
	res, _ := ex.Execute(context.Background(), peopleTask())
	require.Equal(t, "FAILED", res.Status)
	require.ErrorContains(t, res.Error, "laptop browser")

	bad := peopleTask()
	bad.InputPayload = json.RawMessage(`{"linkedin_people":{"queries":["a","b","c","d"]}}`)
	res, _ = peopleExec(policy.DefaultPolicy(), &peoplePage{}).Execute(context.Background(), bad)
	require.Equal(t, "FAILED", res.Status)
	require.ErrorContains(t, res.Error, "at most 3 queries")
}
