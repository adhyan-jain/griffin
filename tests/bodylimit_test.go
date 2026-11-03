package tests

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/griffin/core/internal/api"
)

func TestJSONBodyLimit(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	user := CreateTestUser(t, env.DB, "bodylimit@example.com")
	jwt := GenerateTestJWT(user, "bodylimit@example.com")

	post := func(path, body string) int {
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", jwt)
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	huge := `{"title":"t","goal":"` + strings.Repeat("a", 2<<20) + `","manager_domain":"JJ"}`
	if got := post("/api/v1/tasks", huge); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON: got %d want 413", got)
	}

	ok := `{"title":"t","goal":"small","manager_domain":"JJ","priority":"HIGH_P1"}`
	if got := post("/api/v1/tasks", ok); got != http.StatusCreated {
		t.Fatalf("normal JSON: got %d want 201", got)
	}

	// Upload route is not capped by the global limiter: it must not return 413 from it
	// (handler may still reject the malformed body with another status).
	if got := post("/api/v1/artifacts/upload", huge); got == http.StatusRequestEntityTooLarge {
		t.Fatalf("upload route hit the global JSON cap (got 413 for 2 MiB body below MaxUploadBytes)")
	}
}
