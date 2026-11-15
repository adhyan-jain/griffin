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
	"github.com/griffin/core/internal/service"
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

func TestInboundScreeningDisabledForwardsDirectly(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "calls_off@example.com")
	calls := repository.NewCallRepository(env.DB.Pool)

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
		Calls: &handlers.CallsHandler{
			Log: calls, AuthToken: callsToken, PublicURL: callsBase, OwnerUserID: owner,
			OwnerName: "Adhyan", OwnerNumber: "+919000000001",
			InboundEnabled: func(ctx context.Context) bool { return false }, // toggled off
		},
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// A stranger calls while automated screening is toggled off: forwarded straight through to owner's number.
	form := url.Values{"CallSid": {"CA-off-1"}, "From": {"+15559998888"}}
	resp, body := signedPost(t, ts, "/api/v1/calls/inbound", form, callsToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "<Dial")
	assert.Contains(t, body, "+919000000001")
	assert.NotContains(t, body, "<Gather")
}

func TestAutonomousOutboundCallFlow(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "outbound_calls@example.com")
	calls := repository.NewCallRepository(env.DB.Pool)
	push := &pushLog{}

	callHandler := &handlers.CallsHandler{
		Log: calls, Sender: push, AuthToken: callsToken, PublicURL: callsBase,
		OwnerUserID: owner, OwnerName: "Adhyan", OwnerNumber: "+919000000001",
	}

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
		Calls: callHandler,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Initiate outbound call
	callID, err := callHandler.MakeOutboundCall(ctx, owner, "+919876543210", "Bhai", "Pick up milk on your way home")
	require.NoError(t, err)
	require.NotEmpty(t, callID)

	// 2. Twilio requests the TwiML for when Bhai answers
	resp, body := signedPost(t, ts, "/api/v1/calls/outbound/twiml?id="+callID, url.Values{}, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "Hello, this is an automated message from Adhyan: Pick up milk on your way home")
	assert.Contains(t, body, "<Gather")
	assert.Contains(t, body, "/api/v1/calls/outbound/gather?id="+callID)

	// 3. Bhai responds with something to say
	form := url.Values{"SpeechResult": {"I will be home by 7, is that fine?"}}
	resp, body = signedPost(t, ts, "/api/v1/calls/outbound/gather?id="+callID, form, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Must say: "I will ask [Owner] and reply in a while, is there anything that you want to mention?"
	assert.Contains(t, body, "I will ask Adhyan and reply in a while, is there anything that you want to mention?")
	assert.Contains(t, body, "<Gather")
	assert.Contains(t, body, "/api/v1/calls/outbound/followup?id="+callID)

	// 4. Bhai answers the followup question
	formFollowup := url.Values{"SpeechResult": {"Also tell him to keep dinner ready"}}
	resp, body = signedPost(t, ts, "/api/v1/calls/outbound/followup?id="+callID, formFollowup, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "Thank you. I have noted that and will inform Adhyan. Goodbye.")

	// 5. Owner receives push notification with both parts
	require.Len(t, push.msgs, 1)
	assert.Contains(t, push.msgs[0].Title, "Bhai")
	assert.Contains(t, push.msgs[0].Body, "I will be home by 7, is that fine?")
	assert.Contains(t, push.msgs[0].Body, "Also tell him to keep dinner ready")

	// 6. Call log recorded
	recent, err := calls.Recent(ctx, owner, 5)
	require.NoError(t, err)
	require.NotEmpty(t, recent)
	assert.Equal(t, "message", recent[0].Outcome)
	assert.Contains(t, recent[0].Message, "Pick up milk")
	assert.Contains(t, recent[0].Message, "I will be home by 7")
}

func TestParseAutomatedCallCommand(t *testing.T) {
	cases := []struct {
		input      string
		wantKind   service.TaskCommandKind
		wantRef    string
		wantTarget string
	}{
		{
			input:      "make an automated call to mom and tell them I will be late",
			wantKind:   service.CmdAutomatedCall,
			wantRef:    "mom",
			wantTarget: "I will be late",
		},
		{
			input:      "make automated call to +919876543210 saying meeting at 5pm",
			wantKind:   service.CmdAutomatedCall,
			wantRef:    "+919876543210",
			wantTarget: "meeting at 5pm",
		},
		{
			input:      "call rahul and tell him I reached safely",
			wantKind:   service.CmdAutomatedCall,
			wantRef:    "rahul",
			wantTarget: "I reached safely",
		},
	}
	for _, tc := range cases {
		cmd := service.ParseTaskCommand(tc.input)
		assert.Equal(t, tc.wantKind, cmd.Kind, "for input: "+tc.input)
		assert.Equal(t, tc.wantRef, cmd.Ref, "for input: "+tc.input)
		assert.Equal(t, tc.wantTarget, cmd.Target, "for input: "+tc.input)
	}
}
