package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// hostServer is e.server plus the Host API, with every repository on the production routing: a user
// transaction (RLS enforced as griffin_app) for owner routes, the explicit system path for host routes.
func (e *rlsEnv) hostServer(t *testing.T) *httptest.Server {
	t.Helper()
	taskRepo := repository.NewPostgresTaskRepository(e.Scoped)
	approvalRepo := repository.NewPostgresApprovalRepository(e.Scoped)
	agentRepo := repository.NewPostgresAgentRepository(e.Scoped)
	artifactRepo := repository.NewPostgresArtifactRepository(e.Scoped)
	workerDefRepo := repository.NewPostgresWorkerDefinitionRepository(e.Scoped)
	orchRepo := repository.NewPostgresOrchestrationRepository(e.Scoped)
	hostRepo := repository.NewPostgresHostRepository(e.Scoped)
	engine := service.NewTaskEngine(taskRepo, approvalRepo, nil)
	factory := service.NewAgentFactoryService(workerDefRepo, e.ManagerReg, nil)
	orch := service.NewJJOrchestrator(engine, orchRepo, taskRepo, e.ManagerReg, factory, nil, nil)
	storageSvc := storage.NewStorageService(storage.NewMockStorageProvider(), artifactRepo, "griffin-test-artifacts", nil)
	cfg := *e.Config
	cfg.MaxUploadBytes = 1 << 20
	cfg.HostsEnabled = true
	srv := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: e.DB, Engine: engine, TaskRepo: taskRepo, ApprovalRepo: approvalRepo, AgentRepo: agentRepo,
		ArtifactRepo: artifactRepo, StorageService: storageSvc, WorkerDefRepo: workerDefRepo, OrchRepo: orchRepo,
		Orchestrator: orch, Factory: factory, ManagerRegistry: e.ManagerReg, UserTx: e.UserTx, HostRepo: hostRepo,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type envData struct {
	Data json.RawMessage `json:"data"`
}

func TestHostsUnderRLSEnforcedRoles(t *testing.T) {
	e := setupRLS(t, 2, "")
	ts := e.hostServer(t)
	ctx := context.Background()
	a := CreateTestUser(t, e.DB, "rls_host_a@example.com")
	b := CreateTestUser(t, e.DB, "rls_host_b@example.com")
	jwtA, jwtB := GenerateTestJWT(a, "rls_host_a@example.com"), GenerateTestJWT(b, "rls_host_b@example.com")

	// A registers a host as griffin_app inside an RLS transaction.
	code, body := doJSON(t, ts, "POST", "/api/v1/hosts", jwtA, nil, map[string]any{"name": "a-laptop", "kind": "laptop", "capabilities": []string{"agent"}})
	if code != http.StatusCreated {
		t.Fatalf("A registers a host: %d %s", code, body)
	}
	var env envData
	_ = json.Unmarshal(body, &env)
	var reg struct {
		Host  struct{ ID uuid.UUID } `json:"host"`
		Token string                 `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &reg); err != nil || reg.Token == "" || reg.Host.ID == uuid.Nil {
		t.Fatalf("bad register response: %s", body)
	}
	if n := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM hosts WHERE id=$1 AND user_id=$2`, reg.Host.ID, a); n != 1 {
		t.Fatal("the host must be committed for user A before the response")
	}

	// B cannot see, pause or revoke A's host.
	if code, body := doJSON(t, ts, "GET", "/api/v1/hosts", jwtB, nil, nil); code != http.StatusOK || containsID(body, reg.Host.ID) {
		t.Fatalf("B's host list must not show A's host: %d %s", code, body)
	}
	if code, _ := doJSON(t, ts, "PATCH", "/api/v1/hosts/"+reg.Host.ID.String(), jwtB, nil, map[string]any{"paused": true}); code != http.StatusNotFound {
		t.Fatalf("B pausing A's host: %d, want 404", code)
	}
	if code, _ := doJSON(t, ts, "DELETE", "/api/v1/hosts/"+reg.Host.ID.String(), jwtB, nil, nil); code != http.StatusNotFound {
		t.Fatalf("B revoking A's host: %d, want 404", code)
	}
	// B cannot register a host owned by A (the policy's WITH CHECK).
	if _, err := e.UserTx.Pool.Exec(ctx, `INSERT INTO hosts (user_id, name, kind, token_hash) VALUES ($1,'x','laptop','h')`, a); err == nil {
		t.Fatal("griffin_app without a user transaction must not be able to insert a host at all")
	}

	// The host authenticates through the explicit system path and is given only its owner's work.
	bearer := map[string]string{"Authorization": "Bearer " + reg.Token}
	if code, body := doJSON(t, ts, "POST", "/api/v1/host/heartbeat", "", bearer, map[string]any{"capabilities": []string{"agent"}}); code != http.StatusOK {
		t.Fatalf("host heartbeat through the system path: %d %s", code, body)
	}
	if _, err := e.Engine.CreateTask(ctx, &domain.Task{UserID: b, Title: "B's task", Goal: "g", Priority: domain.TaskPriorityNormalP2}, nil); err != nil {
		t.Fatal(err)
	}
	if code, body := doJSON(t, ts, "POST", "/api/v1/host/claim", "", bearer, nil); code != http.StatusNoContent {
		t.Fatalf("A's host must not be handed B's task: %d %s", code, body)
	}
	if _, err := e.Engine.CreateTask(ctx, &domain.Task{UserID: a, Title: "A's task", Goal: "g", Priority: domain.TaskPriorityNormalP2}, nil); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, ts, "POST", "/api/v1/host/claim", "", bearer, nil)
	if code != http.StatusOK || !containsStr(body, "A's task") {
		t.Fatalf("A's host takes A's task: %d %s", code, body)
	}

	// Host routes need a host token; the user JWT and the worker key do not work there.
	if code, _ := doJSON(t, ts, "POST", "/api/v1/host/claim", jwtA, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("user JWT on a host route: %d", code)
	}
	if code, _ := doJSON(t, ts, "POST", "/api/v1/host/claim", "", map[string]string{"X-Worker-Key": TestWorkerKey}, nil); code != http.StatusUnauthorized {
		t.Fatalf("worker key on a host route: %d", code)
	}
}

func containsID(b []byte, id uuid.UUID) bool { return containsStr(b, id.String()) }

func containsStr(b []byte, s string) bool { return strings.Contains(string(b), s) }
