package hands

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands/web"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/worker"
)

type fixedPolicy struct {
	p  policy.Policy
	ok bool
}

func (f fixedPolicy) Policy() (policy.Policy, int, bool) { return f.p, 1, f.ok }

type stubMessenger struct {
	calls int
	got   *domain.MessagingAction
	res   *whatsapp.Result
	err   error
}

func (s *stubMessenger) Do(_ context.Context, a *domain.MessagingAction) (*whatsapp.Result, error) {
	s.calls++
	s.got = a
	if s.err != nil {
		return nil, s.err
	}
	if s.res != nil {
		return s.res, nil
	}
	return &whatsapp.Result{Via: "whatsapp-web", Sent: a.Outward()}, nil
}

type stubAgent struct{ calls int }

func (s *stubAgent) Execute(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
	s.calls++
	return &worker.ExecutionResult{Output: json.RawMessage(`{"response":"agent"}`)}, nil
}
func (*stubAgent) Name() string                            { return "agent" }
func (*stubAgent) IsHealthy(context.Context) (bool, error) { return true, nil }

func newExec(p policy.Policy, synced bool, m *stubMessenger, agent worker.TaskExecutor) *Executor {
	return &Executor{Agent: agent, Gate: gateway.New("laptop", fixedPolicy{p, synced}, nil), WhatsApp: m}
}

func sendTask(t *testing.T, act domain.MessagingAction, extra map[string]any) (*domain.Task, *domain.MessagingAction) {
	t.Helper()
	require.NoError(t, act.Validate())
	payload := map[string]any{"requires": []string{"messaging"}, "messaging": act}
	for k, v := range extra {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "t", Goal: "g", InputPayload: b}, &act
}

func send(contact, text string) domain.MessagingAction {
	return domain.MessagingAction{Op: domain.MessagingSend, Contact: contact, Text: text}
}

func TestSendAsksByDefaultAndDoesNothingUntilApproved(t *testing.T) {
	m := &stubMessenger{}
	task, act := sendTask(t, send("Mom", "running late"), nil)
	res, err := newExec(policy.DefaultPolicy(), true, m, nil).Execute(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, res.RequiresApproval, "the default policy asks before sending")
	assert.Equal(t, act.ApprovalActionType(), res.RequiresApproval.ActionType)
	assert.Equal(t, domain.ApprovalRiskHigh, res.RequiresApproval.RiskLevel)
	assert.Contains(t, res.RequiresApproval.Summary, "Mom")
	assert.Contains(t, res.RequiresApproval.Summary, "running late")
	assert.Zero(t, m.calls, "nothing is sent while waiting")
}

func TestApprovedSendGoesAheadOnceAndReportsHowItWasSent(t *testing.T) {
	m := &stubMessenger{}
	task, act := sendTask(t, send("Mom", "running late"), nil)
	ctx := worker.WithApprovedActions(context.Background(), []string{act.ApprovalActionType()})
	res, err := newExec(policy.DefaultPolicy(), true, m, nil).Execute(ctx, task)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Equal(t, 1, m.calls)
	assert.Equal(t, "hands/whatsapp-web", res.ProviderName)
	var out map[string]any
	require.NoError(t, json.Unmarshal(res.Output, &out))
	assert.Contains(t, out["response"], "Sent your WhatsApp message to Mom")
	assert.Equal(t, true, out["sent"])
	assert.Equal(t, "whatsapp-web", out["via"])
}

// An approval covers exactly the action the owner saw. A different text or person asks again.
func TestAnApprovalDoesNotCoverADifferentMessage(t *testing.T) {
	m := &stubMessenger{}
	approvedFor := send("Mom", "hi")
	require.NoError(t, approvedFor.Validate())
	ctx := worker.WithApprovedActions(context.Background(), []string{approvedFor.ApprovalActionType()})
	for name, other := range map[string]domain.MessagingAction{
		"other text":   send("Mom", "hi!"),
		"other person": send("Dad", "hi"),
	} {
		task, _ := sendTask(t, other, nil)
		res, err := newExec(policy.DefaultPolicy(), true, m, nil).Execute(ctx, task)
		require.NoError(t, err, name)
		assert.NotNil(t, res.RequiresApproval, name)
	}
	assert.Zero(t, m.calls)
}

func TestAnAutoRuleForAContactSendsWithoutAsking(t *testing.T) {
	p := policy.DefaultPolicy()
	p.Rules = append(p.Rules, policy.Rule{Contact: "mom", Class: policy.Send, Mode: policy.Auto})
	m := &stubMessenger{}
	task, _ := sendTask(t, send("Mom", "hi"), nil)
	res, err := newExec(p, true, m, nil).Execute(context.Background(), task)
	require.NoError(t, err)
	assert.Nil(t, res.RequiresApproval)
	assert.NoError(t, res.Error)
	assert.Equal(t, 1, m.calls)

	// The same rule does not cover anyone else.
	m2 := &stubMessenger{}
	task, _ = sendTask(t, send("Dad", "hi"), nil)
	res, _ = newExec(p, true, m2, nil).Execute(context.Background(), task)
	assert.NotNil(t, res.RequiresApproval)
	assert.Zero(t, m2.calls)
}

// "Never" is final: an approval in hand cannot override it.
func TestANeverRuleCannotBeOverriddenByAnApproval(t *testing.T) {
	p := policy.DefaultPolicy()
	p.Rules = append(p.Rules, policy.Rule{Contact: "boss", Class: policy.Send, Mode: policy.Off})
	m := &stubMessenger{}
	task, act := sendTask(t, send("Boss", "hi"), nil)
	ctx := worker.WithApprovedActions(context.Background(), []string{act.ApprovalActionType()})
	res, err := newExec(p, true, m, nil).Execute(ctx, task)
	require.NoError(t, err)
	require.Error(t, res.Error)
	assert.False(t, res.Retryable)
	assert.Contains(t, res.Error.Error(), "permission settings")
	assert.Zero(t, m.calls)
}

func TestNothingOutwardRunsBeforeThePolicyArrives(t *testing.T) {
	m := &stubMessenger{}
	task, act := sendTask(t, send("Mom", "hi"), nil)
	ctx := worker.WithApprovedActions(context.Background(), []string{act.ApprovalActionType()})
	res, err := newExec(policy.Policy{}, false, m, nil).Execute(ctx, task)
	require.NoError(t, err)
	require.Error(t, res.Error, "an unsynced host denies, even with an approval in hand")
	assert.Zero(t, m.calls)
}

func TestReadingIsAllowedByTheDefaultPolicy(t *testing.T) {
	m := &stubMessenger{res: &whatsapp.Result{Via: "whatsapp-android", Chats: []whatsapp.Chat{{Name: "Dad", Unread: 3, Preview: "call me"}}}}
	task, _ := sendTask(t, domain.MessagingAction{Op: domain.MessagingUnread}, nil)
	res, err := newExec(policy.DefaultPolicy(), true, m, nil).Execute(context.Background(), task)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.TextOutput, "Dad (3 unread): call me")
	assert.Contains(t, res.TextOutput, "whatsapp-android")
}

// An action an agent wrote from text it read is not "intended" unless the owner's own request named that
// contact. Even under an auto rule it asks, so text from outside cannot make Griffin message anyone.
func TestAnAgentComposedActionNeedsTheOwnersNamedContact(t *testing.T) {
	p := policy.DefaultPolicy()
	p.Rules = append(p.Rules, policy.Rule{Contact: "mom", Class: policy.Send, Mode: policy.Auto})
	compose := func(contact string) context.Context {
		out, _ := json.Marshal(map[string]string{"response": "Here you go:\n```json\n{\"op\":\"send\",\"contact\":\"" + contact + "\",\"text\":\"hi\"}\n```"})
		return worker.WithDependencyContext(context.Background(), []repository.DependencyOutput{{Title: "compose", Output: out}})
	}
	step := func(extra map[string]any) *domain.Task {
		payload := map[string]any{"requires": []string{"messaging"}}
		for k, v := range extra {
			payload[k] = v
		}
		b, _ := json.Marshal(payload)
		return &domain.Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: b}
	}

	m := &stubMessenger{}
	res, err := newExec(p, true, m, nil).Execute(compose("Mom"), step(nil))
	require.NoError(t, err)
	assert.NotNil(t, res.RequiresApproval, "no contact named by the owner: ask")

	res, _ = newExec(p, true, m, nil).Execute(compose("Mom"), step(map[string]any{"intent_contact": "Mom"}))
	assert.Nil(t, res.RequiresApproval, "the owner named Mom and a rule allows Mom: go")
	assert.Equal(t, 1, m.calls)

	res, _ = newExec(p, true, m, nil).Execute(compose("Dad"), step(map[string]any{"intent_contact": "Mom"}))
	assert.NotNil(t, res.RequiresApproval, "composed for Dad but the owner said Mom: ask")
	assert.Equal(t, 1, m.calls)
}

func TestAStepThatNeedsAnActionButHasNoneFails(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"requires": []string{"messaging"}})
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: b}
	res, err := newExec(policy.DefaultPolicy(), true, &stubMessenger{}, nil).Execute(context.Background(), task)
	require.NoError(t, err)
	require.Error(t, res.Error)
	assert.False(t, res.Retryable)
}

func TestStepsWithoutHandsGoToTheAgent(t *testing.T) {
	agent := &stubAgent{}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: json.RawMessage(`{}`)}
	res, err := newExec(policy.DefaultPolicy(), true, &stubMessenger{}, agent).Execute(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, 1, agent.calls)
	assert.JSONEq(t, `{"response":"agent"}`, string(res.Output))
}

func TestDriverFailuresAreExplainedAndNeverRetried(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"unconfirmed": {whatsapp.ErrUnconfirmed, "will not send it again"},
		"not ready":   {errors.Join(whatsapp.ErrNotReady, errors.New("laptop browser not signed in")), "any of your devices"},
		"not found":   {whatsapp.ErrContactNotFound, "contact not found"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := &stubMessenger{err: c.err}
			p := policy.DefaultPolicy()
			p.Rules = append(p.Rules, policy.Rule{Contact: "mom", Class: policy.Send, Mode: policy.Auto})
			task, _ := sendTask(t, send("Mom", "hi"), nil)
			res, err := newExec(p, true, m, nil).Execute(context.Background(), task)
			require.NoError(t, err)
			require.Error(t, res.Error)
			assert.False(t, res.Retryable, "a failed send is never retried automatically")
			assert.Contains(t, res.Error.Error(), c.want)
			assert.Equal(t, 1, m.calls)
		})
	}
}

func TestActionExtraction(t *testing.T) {
	dep := func(resp string) []repository.DependencyOutput {
		b, _ := json.Marshal(map[string]string{"response": resp})
		return []repository.DependencyOutput{{Output: b}}
	}
	act, err := ActionFromDependencies(dep("Sure! {\"unrelated\": 1} then {\"messaging\": {\"op\":\"send\",\"phone\":\"+91 98765 43210\",\"text\":\"hi\"}} done"))
	require.NoError(t, err)
	require.NotNil(t, act)
	assert.Equal(t, "919876543210", act.Phone)

	_, err = ActionFromDependencies(dep(`{"op":"send","contact":"Mom"}`))
	require.Error(t, err, "a send with no text is refused, not guessed")
	assert.ErrorIs(t, err, domain.ErrValidation)

	_, err = ActionFromDependencies(dep("no json here"))
	assert.ErrorIs(t, err, ErrNoAction)
}

// ---- web worker ----

type fakeProvider struct{ res []web.Result }

func (fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Search(context.Context, string, int) ([]web.Result, error) {
	return f.res, nil
}

func webExec(p policy.Policy, pages map[string]string) (*Executor, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>T" + r.URL.Path + "</title></head><body><p>" + body + "</p></body></html>"))
	}))
	ex := &Executor{
		Gate: gateway.New("laptop", fixedPolicy{p, true}, nil),
		Web: &WebService{
			Fetcher: web.NewFetcher(web.FetcherConfig{AllowPrivate: true}),
			Searcher: &web.Searcher{Providers: []web.Provider{fakeProvider{res: []web.Result{
				{Title: "One", URL: srvURL(srv, "/a")}, {Title: "Gone", URL: srvURL(srv, "/missing")}, {Title: "Two", URL: srvURL(srv, "/b")},
			}}}},
		},
	}
	return ex, srv
}

// srvURL is filled in after the server exists; the provider above is rebuilt by the caller when needed.
func srvURL(srv *httptest.Server, path string) string {
	if srv == nil {
		return path
	}
	return srv.URL + path
}

func webTask(t *testing.T, w domain.WebAction) *domain.Task {
	t.Helper()
	require.NoError(t, w.Validate())
	b, _ := json.Marshal(map[string]any{"requires": []string{"web"}, "web": w})
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: b}
}

func TestSearchThenReadTheTopPages(t *testing.T) {
	ex, srv := webExec(policy.DefaultPolicy(), map[string]string{"/a": "alpha facts", "/b": "beta facts"})
	defer srv.Close()
	ex.Web.Searcher = &web.Searcher{Providers: []web.Provider{fakeProvider{res: []web.Result{
		{Title: "One", URL: srv.URL + "/a"}, {Title: "Gone", URL: srv.URL + "/missing"}, {Title: "Two", URL: srv.URL + "/b"},
	}}}}

	res, err := ex.Execute(context.Background(), webTask(t, domain.WebAction{Op: domain.WebSearch, Query: "facts", Limit: 3}))
	require.NoError(t, err)
	require.NoError(t, res.Error, "reading is allowed by the default policy with no approval")
	assert.Contains(t, res.TextOutput, "untrusted")
	assert.Contains(t, res.TextOutput, srv.URL+"/a")

	deps := []repository.DependencyOutput{{Title: "search", Output: res.Output}}
	ctx := worker.WithDependencyContext(context.Background(), deps)
	read, err := ex.Execute(ctx, webTask(t, domain.WebAction{Op: domain.WebFetch, FromSearch: true, Limit: 2}))
	require.NoError(t, err)
	require.NoError(t, read.Error)
	assert.Contains(t, read.TextOutput, "alpha facts")
	assert.Contains(t, read.TextOutput, "beta facts", "a dead link is skipped and the next result is used")
	assert.Contains(t, read.TextOutput, "Could not read")
}

func TestReadingAFixedPage(t *testing.T) {
	ex, srv := webExec(policy.DefaultPolicy(), map[string]string{"/a": "alpha facts"})
	defer srv.Close()
	res, err := ex.Execute(context.Background(), webTask(t, domain.WebAction{Op: domain.WebFetch, URL: srv.URL + "/a"}))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.TextOutput, "alpha facts")
}

func TestTheOwnerCanSwitchWebAccessOffOrToAsk(t *testing.T) {
	ex, srv := webExec(policy.Policy{Default: policy.Ask, Rules: []policy.Rule{{Capability: "web", Mode: policy.Off}}}, map[string]string{"/a": "x"})
	defer srv.Close()
	res, _ := ex.Execute(context.Background(), webTask(t, domain.WebAction{Op: domain.WebFetch, URL: srv.URL + "/a"}))
	require.Error(t, res.Error)
	assert.Contains(t, res.Error.Error(), "permission settings")

	ex, srv2 := webExec(policy.Policy{Default: policy.Ask}, map[string]string{"/a": "x"})
	defer srv2.Close()
	task := webTask(t, domain.WebAction{Op: domain.WebFetch, URL: srv2.URL + "/a"})
	res, _ = ex.Execute(context.Background(), task)
	require.NotNil(t, res.RequiresApproval, "web set to ask pauses for approval")
	w, _ := domain.ParseWebAction(task.InputPayload)
	ctx := worker.WithApprovedActions(context.Background(), []string{w.ApprovalActionType()})
	res, _ = ex.Execute(ctx, task)
	assert.NoError(t, res.Error)
}

func TestFetchFromSearchWithoutResultsFails(t *testing.T) {
	ex, srv := webExec(policy.DefaultPolicy(), nil)
	defer srv.Close()
	res, err := ex.Execute(context.Background(), webTask(t, domain.WebAction{Op: domain.WebFetch, FromSearch: true}))
	require.NoError(t, err)
	require.Error(t, res.Error)
}
