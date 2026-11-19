package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// DB-integration test: the proactive routes must exist when the production dependencies are wired,
// reject unauthenticated callers, and recognise an authenticated user (regression for the untyped
// "user_id" context lookup that made every call 401).
func TestProactiveRoutesAreMountedAndAuthenticated(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	inboundRepo := repository.NewInboundEventRepository(env.DB.Pool)
	triage := &service.InboundTriageService{Events: inboundRepo}
	srv := api.NewServer(api.ServerDependencies{
		Config:           env.Config,
		DB:               env.DB,
		Engine:           env.Engine,
		TaskRepo:         env.TaskRepo,
		ApprovalRepo:     env.ApprovalRepo,
		AgentRepo:        env.AgentRepo,
		ArtifactRepo:     env.ArtifactRepo,
		StorageService:   env.StorageService,
		WorkerDefRepo:    env.WorkerDefRepo,
		OrchRepo:         env.OrchRepo,
		Orchestrator:     env.Orchestrator,
		Factory:          env.AgentFactory,
		ManagerRegistry:  env.ManagerReg,
		InboundEventRepo: inboundRepo,
		InboundTriage:    triage,
		InboxRepo:        repository.NewInboxRepository(env.DB.Pool),
		GmailSyncer:      &service.GmailSyncer{InboxRepo: repository.NewInboxRepository(env.DB.Pool), Events: inboundRepo, Triage: triage},
		DiscoveryRepo:    repository.NewDiscoveryRepository(env.DB.Pool),
		TTS:              service.NewTTSService(t.TempDir()),
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := CreateTestUser(t, env.DB, "wiring@example.com")
	auth := GenerateTestJWT(u, "wiring@example.com")

	do := func(method, path, token, body string) int {
		req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	routes := []struct{ method, path string }{
		{"GET", "/api/v1/inbox/assessments"},
		{"POST", "/api/v1/inbox/sync"},
		{"GET", "/api/v1/discovery/jobs"},
		{"GET", "/api/v1/events/inbound"},
		{"GET", "/api/v1/voice/voices"},
	}
	for _, r := range routes {
		require.Equalf(t, http.StatusUnauthorized, do(r.method, r.path, "", "{}"), "%s %s must require auth (not 404)", r.method, r.path)
	}

	require.Equal(t, http.StatusUnauthorized, do("GET", "/api/v1/system/capabilities", "", ""))
	require.Equal(t, http.StatusOK, do("GET", "/api/v1/system/capabilities", auth, ""))

	// Authenticated: the handlers must see the user (formerly always 401).
	require.Equal(t, http.StatusOK, do("GET", "/api/v1/inbox/assessments", auth, ""))
	require.Equal(t, http.StatusOK, do("GET", "/api/v1/discovery/jobs", auth, ""))
	require.Equal(t, http.StatusOK, do("POST", "/api/v1/inbox/sync", auth, `{"messages":[]}`))
}
