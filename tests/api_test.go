package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
)

func TestAPIEndpoints(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	server := api.NewServer(api.ServerDependencies{
		Config:       env.Config,
		Logger:       nil,
		DB:           env.DB,
		Engine:       env.Engine,
		TaskRepo:     env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo,
		AgentRepo:    env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo,
	})

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := ts.Client()

	// 1. Health & Readiness (unauthenticated)
	t.Run("Healthz and Readyz", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on /healthz, got %v, code %d", err, resp.StatusCode)
		}

		resp, err = client.Get(ts.URL + "/readyz")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on /readyz, got %v, code %d", err, resp.StatusCode)
		}
	})

	// 2. Authentication Enforcement
	t.Run("Auth Enforcement", func(t *testing.T) {
		// Missing header
		resp, err := client.Get(ts.URL + "/api/v1/tasks")
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 on missing auth, got %d", resp.StatusCode)
		}

		// Malformed header
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/tasks", nil)
		req.Header.Set("Authorization", "InvalidTokenHeader")
		resp, err = client.Do(req)
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 on malformed auth, got %d", resp.StatusCode)
		}

		// Bad token
		req, _ = http.NewRequest("GET", ts.URL+"/api/v1/tasks", nil)
		req.Header.Set("Authorization", "Bearer invalid.jwt.signature")
		resp, err = client.Do(req)
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 on bad token, got %d", resp.StatusCode)
		}
	})

	// 3. User Task Lifecycle via REST API
	userA := CreateTestUser(t, env.DB, "api_user_a@example.com")
	jwtUserA := GenerateTestJWT(userA, "api_user_a@example.com")

	var createdTaskID uuid.UUID

	t.Run("Create Task via API", func(t *testing.T) {
		body := map[string]any{
			"title":          "API Task 1",
			"goal":           "Test REST API creation",
			"manager_domain": "JJ",
			"priority":       "HIGH_P1",
			"input_payload":  map[string]string{"foo": "bar"},
		}
		jsonBytes, _ := json.Marshal(body)

		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/tasks", bytes.NewReader(jsonBytes))
		req.Header.Set("Authorization", jwtUserA)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /api/v1/tasks failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyStr, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201 Created, got %d: %s", resp.StatusCode, string(bodyStr))
		}

		var apiResp struct {
			Success bool        `json:"success"`
			Data    domain.Task `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			t.Fatalf("decode response error: %v", err)
		}

		createdTaskID = apiResp.Data.ID
		if createdTaskID == uuid.Nil {
			t.Fatal("expected non-nil task ID in API response")
		}
		if apiResp.Data.Status != domain.TaskStatusQueued {
			t.Errorf("expected status QUEUED, got %s", apiResp.Data.Status)
		}
	})

	t.Run("Get Task via API", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/tasks/%s", ts.URL, createdTaskID), nil)
		req.Header.Set("Authorization", jwtUserA)

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/tasks/{id} failed with code %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var apiResp struct {
			Success bool        `json:"success"`
			Data    domain.Task `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiResp)
		if apiResp.Data.Title != "API Task 1" {
			t.Errorf("expected title 'API Task 1', got %s", apiResp.Data.Title)
		}
	})

	t.Run("List Tasks via API", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/tasks?status=QUEUED", nil)
		req.Header.Set("Authorization", jwtUserA)

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/tasks failed with code %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var apiResp struct {
			Success bool          `json:"success"`
			Data    []domain.Task `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiResp)
		if len(apiResp.Data) == 0 {
			t.Fatal("expected at least 1 task listed")
		}
	})

	// 4. Worker Endpoints via REST API
	t.Run("Worker API Security and Workflow", func(t *testing.T) {
		// Worker claim without key -> 401
		claimReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workers/claim", nil)
		resp, err := client.Do(claimReq)
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated worker, got %d", resp.StatusCode)
		}

		// Worker claim with valid key
		claimReq, _ = http.NewRequest("POST", ts.URL+"/api/v1/workers/claim", nil)
		claimReq.Header.Set("X-Worker-Key", TestWorkerKey)
		claimReq.Header.Set("X-Worker-ID", "api-worker-1")

		resp, err = client.Do(claimReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for worker claim, got %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var claimResp struct {
			Success bool        `json:"success"`
			Data    domain.Task `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&claimResp)
		if claimResp.Data.ID != createdTaskID {
			t.Fatalf("expected claimed task %s, got %s", createdTaskID, claimResp.Data.ID)
		}

		// Worker heartbeat
		hbURL := fmt.Sprintf("%s/api/v1/workers/%s/heartbeat", ts.URL, createdTaskID)
		hbReq, _ := http.NewRequest("POST", hbURL, nil)
		hbReq.Header.Set("X-Worker-Key", TestWorkerKey)
		hbReq.Header.Set("X-Worker-ID", "api-worker-1")

		resp, err = client.Do(hbReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for worker heartbeat, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		// Worker complete
		compURL := fmt.Sprintf("%s/api/v1/workers/%s/complete", ts.URL, createdTaskID)
		compBody := map[string]any{"output": map[string]string{"result": "api success"}}
		compBytes, _ := json.Marshal(compBody)

		compReq, _ := http.NewRequest("POST", compURL, bytes.NewReader(compBytes))
		compReq.Header.Set("X-Worker-Key", TestWorkerKey)
		compReq.Header.Set("X-Worker-ID", "api-worker-1")
		compReq.Header.Set("Content-Type", "application/json")

		resp, err = client.Do(compReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for worker complete, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		// Verify task is now completed
		getReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/tasks/%s", ts.URL, createdTaskID), nil)
		getReq.Header.Set("Authorization", jwtUserA)
		resp, _ = client.Do(getReq)
		var checkResp struct {
			Data domain.Task `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&checkResp)
		resp.Body.Close()

		if checkResp.Data.Status != domain.TaskStatusCompleted {
			t.Fatalf("expected task status COMPLETED, got %s", checkResp.Data.Status)
		}
	})

	// 5. Agent Registration and Retrieval
	t.Run("Agent Registry API", func(t *testing.T) {
		agentBody := map[string]any{
			"name":           "Career Agent",
			"role_type":      "MANAGER",
			"manager_domain": "CAREER",
			"description":    "Job search manager",
			"system_prompt":  "Manage career opportunities",
		}
		agentBytes, _ := json.Marshal(agentBody)

		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents", bytes.NewReader(agentBytes))
		req.Header.Set("Authorization", jwtUserA)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST /api/v1/agents failed with code %d", resp.StatusCode)
		}
		resp.Body.Close()

		// List agents
		listReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents", nil)
		listReq.Header.Set("Authorization", jwtUserA)
		resp, err = client.Do(listReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/agents failed with code %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var agentResp struct {
			Data []domain.Agent `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&agentResp)
		if len(agentResp.Data) == 0 {
			t.Fatal("expected at least 1 agent returned")
		}
	})

	// 6. Artifact API
	t.Run("Artifacts API", func(t *testing.T) {
		artBody := map[string]any{
			"title":         "Research Summary",
			"artifact_type": "MARKDOWN",
			"storage_uri":   "s3://griffin-artifacts/report.md",
		}
		artBytes, _ := json.Marshal(artBody)

		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/artifacts", bytes.NewReader(artBytes))
		req.Header.Set("Authorization", jwtUserA)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST /api/v1/artifacts failed with code %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var artResp struct {
			Data domain.Artifact `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&artResp)

		// Get artifact by ID
		getReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/artifacts/%s", ts.URL, artResp.Data.ID), nil)
		getReq.Header.Set("Authorization", jwtUserA)

		resp, err = client.Do(getReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/artifacts/{id} failed with code %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}
