package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

const callsToken = "twilio-test-token"
const callsBase = "https://api.example.test"

func signedPost(t *testing.T, ts *httptest.Server, path string, form url.Values, signWith string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if signWith != "" {
		req.Header.Set("X-Twilio-Signature", domain.TwilioSignature(signWith, callsBase+path, form))
	}
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, sb.String()
}

func TestInboundCallsScreenedForwardedAndRefusedWhenUnsigned(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "calls@example.com")
	calls := repository.NewCallRepository(env.DB.Pool)
	push := &pushLog{}
	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
		Calls: &handlers.CallsHandler{Log: calls, Sender: push, AuthToken: callsToken, PublicURL: callsBase, OwnerUserID: owner,
			OwnerName: "Adhyan", OwnerNumber: "+919000000001", Trusted: []string{"+91 98765 43210"}},
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Unsigned, wrongly signed: refused, nothing recorded.
	form := url.Values{"CallSid": {"CA-bad"}, "From": {"+15550001111"}}
	resp, _ := signedPost(t, ts, "/api/v1/calls/inbound", form, "")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "no signature")
	resp, _ = signedPost(t, ts, "/api/v1/calls/inbound", form, "some-other-token")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "wrong token")

	// 2. A stranger is asked who they are.
	form = url.Values{"CallSid": {"CA-1"}, "From": {"+15550001111"}}
	resp, body := signedPost(t, ts, "/api/v1/calls/inbound", form, callsToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/xml")
	assert.Contains(t, body, "<Gather")
	assert.Contains(t, body, "Adhyan")
	assert.Contains(t, body, callsBase+"/api/v1/calls/screened")
	assert.NotContains(t, body, "<Dial")

	// 3. They answer; the owner gets a push and the call is logged with their words.
	form = url.Values{"CallSid": {"CA-1"}, "From": {"+15550001111"}, "SpeechResult": {"This is Priya from Acme about your interview"}}
	resp, body = signedPost(t, ts, "/api/v1/calls/screened", form, callsToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "Goodbye")
	require.Len(t, push.msgs, 1)
	assert.Contains(t, push.msgs[0].Body, "Priya from Acme")
	assert.Contains(t, push.msgs[0].Title, "+15550001111")

	// 4. A trusted number (written with spaces) rings straight through; no push.
	form = url.Values{"CallSid": {"CA-2"}, "From": {"+919876543210"}}
	_, body = signedPost(t, ts, "/api/v1/calls/inbound", form, callsToken)
	assert.Contains(t, body, "<Dial")
	assert.Contains(t, body, "+919000000001")
	assert.Len(t, push.msgs, 1)

	// 5. Silence is recorded as no answer.
	form = url.Values{"CallSid": {"CA-3"}, "From": {"+15550002222"}}
	signedPost(t, ts, "/api/v1/calls/inbound", form, callsToken)
	_, body = signedPost(t, ts, "/api/v1/calls/screened", form, callsToken)
	assert.Contains(t, body, "Goodbye")

	recent, err := calls.Recent(ctx, owner, 10)
	require.NoError(t, err)
	got := map[string]repository.CallRecord{}
	for _, c := range recent {
		got[c.CallSID] = c
	}
	assert.NotContains(t, got, "CA-bad", "refused calls leave no trace")
	assert.Equal(t, "message", got["CA-1"].Outcome)
	assert.Contains(t, got["CA-1"].Message, "interview")
	assert.Equal(t, "forwarded", got["CA-2"].Outcome)
	assert.Equal(t, "no_answer", got["CA-3"].Outcome)
	assert.Len(t, recent, 3)
}

func TestCallsWithNoTokenConfiguredAcceptNothing(t *testing.T) {
	h := &handlers.CallsHandler{PublicURL: callsBase}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/calls/inbound", strings.NewReader("CallSid=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "anything")
	h.Inbound(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
