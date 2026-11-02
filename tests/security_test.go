package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/storage"
)

func secServer(t *testing.T, env *TestEnv, mutate func(*config.Config)) *httptest.Server {
	t.Helper()
	return secServerWithRunner(t, env, mutate, nil)
}

func secServerWithRunner(t *testing.T, env *TestEnv, mutate func(*config.Config), mgr runner.InstanceManager) *httptest.Server {
	t.Helper()
	cfg := *env.Config
	cfg.MaxUploadBytes = 1 << 20
	if mutate != nil {
		mutate(&cfg)
	}
	server := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo,
		AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService,
		WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo, Orchestrator: env.Orchestrator,
		Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg, InstanceMgr: mgr,
	})
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, method, url, auth string, body []byte, hdr map[string]string) (int, []byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

// ---------- G1: development login can never exist in production ----------

func TestDevLoginRouteGating(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	login := func(ts *httptest.Server) int {
		code, _, _ := call(t, "POST", ts.URL+"/api/v1/auth/login", "", []byte(`{"email":"victim@example.com"}`), map[string]string{"Content-Type": "application/json"})
		return code
	}
	usersBefore := countUsers(t, env)

	// default (dev login not enabled): route does not exist
	if code := login(secServer(t, env, nil)); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("dev login must not exist by default, got %d", code)
	}
	// enabled flag but production environment: still not registered
	prod := secServer(t, env, func(c *config.Config) { c.Environment = "production"; c.EnableDevLogin = true })
	if code := login(prod); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("dev login must not exist in production even if flagged, got %d", code)
	}
	if got := countUsers(t, env); got != usersBefore {
		t.Fatalf("a refused login created %d auth.users rows", got-usersBefore)
	}

	// positive: explicit dev opt-in outside production works and the token is accepted
	dev := secServer(t, env, func(c *config.Config) { c.Environment = "development"; c.EnableDevLogin = true })
	code, body, _ := call(t, "POST", dev.URL+"/api/v1/auth/login", "", []byte(`{"email":"dev-only@example.com"}`), map[string]string{"Content-Type": "application/json"})
	if code != http.StatusOK {
		t.Fatalf("dev login with explicit opt-in: %d %s", code, body)
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &out)
	if out.Data.Token == "" {
		t.Fatalf("no token in %s", body)
	}
	if code, _, _ := call(t, "GET", dev.URL+"/api/v1/tasks", "Bearer "+out.Data.Token, nil, nil); code != http.StatusOK {
		t.Fatalf("dev token rejected: %d", code)
	}
}

func countUsers(t *testing.T, env *TestEnv) int {
	t.Helper()
	var n int
	if err := env.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM auth.users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRunnerStatusRequiresAuth(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ts := secServer(t, env, nil)
	if code, _, _ := call(t, "GET", ts.URL+"/api/v1/runner/status", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("runner status without token: %d", code)
	}
	u := CreateTestUser(t, env.DB, "status@example.com")
	if code, _, _ := call(t, "GET", ts.URL+"/api/v1/runner/status", GenerateTestJWT(u, "status@example.com"), nil, nil); code != http.StatusOK {
		t.Fatalf("runner status with token: %d", code)
	}
}

// ---------- G2: CORS end to end ----------

func TestServerCORSAllowlist(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ts := secServer(t, env, func(c *config.Config) { c.CORSAllowedOrigins = []string{"https://app.example.com"} })

	code, _, h := call(t, "OPTIONS", ts.URL+"/api/v1/tasks", "", nil, map[string]string{"Origin": "https://app.example.com"})
	if code != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != "https://app.example.com" || h.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("allowed origin preflight: %d %v", code, h)
	}
	code, _, h = call(t, "OPTIONS", ts.URL+"/api/v1/tasks", "", nil, map[string]string{"Origin": "https://evil.example"})
	if code != http.StatusForbidden || h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("evil preflight: %d %v", code, h)
	}
	_, _, h = call(t, "GET", ts.URL+"/healthz", "", nil, map[string]string{"Origin": "https://evil.example"})
	if h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("evil origin received ACAO on a simple request")
	}
	// no allowlist configured => never any CORS headers
	none := secServer(t, env, nil)
	_, _, h = call(t, "GET", none.URL+"/healthz", "", nil, map[string]string{"Origin": "https://app.example.com"})
	if h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("empty allowlist must not emit ACAO")
	}
}

// ---------- G7: cross-tenant artifact access ----------

type artResp struct {
	Data domain.Artifact `json:"data"`
}

func uploadAs(t *testing.T, ts *httptest.Server, auth, filename string, taskID *uuid.UUID, content string) (int, domain.Artifact) {
	t.Helper()
	h := map[string]string{"X-Filename": filename, "Content-Type": "text/plain"}
	if taskID != nil {
		h["X-Task-ID"] = taskID.String()
	}
	code, body, _ := call(t, "POST", ts.URL+"/api/v1/artifacts/upload", auth, []byte(content), h)
	var r artResp
	_ = json.Unmarshal(body, &r)
	return code, r.Data
}

func TestArtifactTenantIsolation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ts := secServer(t, env, nil)
	ctx := context.Background()

	a := CreateTestUser(t, env.DB, "tenant-a@example.com")
	b := CreateTestUser(t, env.DB, "tenant-b@example.com")
	authA, authB := GenerateTestJWT(a, "tenant-a@example.com"), GenerateTestJWT(b, "tenant-b@example.com")

	taskA := createTaskFor(t, env, a)
	taskB := createTaskFor(t, env, b)

	// Positive: A uploads under their own prefix and can read it back.
	code, artA := uploadAs(t, ts, authA, "secret.txt", &taskA, "tenant A secret")
	if code != http.StatusCreated || artA.ObjectKey == nil {
		t.Fatalf("A upload: %d", code)
	}
	if !strings.HasPrefix(*artA.ObjectKey, storage.UserKeyPrefix(a)) {
		t.Fatalf("object key %q is not under A's prefix", *artA.ObjectKey)
	}
	code, body, _ := call(t, "GET", fmt.Sprintf("%s/api/v1/artifacts/%s/download", ts.URL, artA.ID), authA, nil, nil)
	if code != 200 || string(body) != "tenant A secret" {
		t.Fatalf("A download: %d %q", code, body)
	}

	// Negative: B cannot read, presign, or delete A's artifact by id.
	for name, c := range map[string][2]string{
		"get":      {"GET", ""},
		"download": {"GET", "/download"},
		"presign":  {"GET", "/presign"},
		"delete":   {"DELETE", ""},
	} {
		got, _, _ := call(t, c[0], fmt.Sprintf("%s/api/v1/artifacts/%s%s", ts.URL, artA.ID, c[1]), authB, nil, nil)
		if got != http.StatusNotFound {
			t.Errorf("B %s on A's artifact: want 404, got %d", name, got)
		}
	}
	if _, err := env.StorageProvider.Head(ctx, storage.HeadInput{Bucket: *artA.Bucket, Key: *artA.ObjectKey}); err != nil {
		t.Fatalf("A's object must survive B's delete attempt: %v", err)
	}

	// Negative: B cannot REGISTER A's object (the original vulnerability).
	reg := func(auth string, body map[string]any) int {
		b, _ := json.Marshal(body)
		code, _, _ := call(t, "POST", ts.URL+"/api/v1/artifacts", auth, b, map[string]string{"Content-Type": "application/json"})
		return code
	}
	foreign := map[string]any{"title": "stolen", "artifact_type": "FILE", "object_key": *artA.ObjectKey}
	if got := reg(authB, foreign); got != http.StatusForbidden {
		t.Errorf("B registering A's key: want 403, got %d", got)
	}
	foreign["bucket"] = "some-other-bucket"
	foreign["object_key"] = storage.UserKeyPrefix(b) + "x"
	if got := reg(authB, foreign); got != http.StatusForbidden {
		t.Errorf("foreign bucket: want 403, got %d", got)
	}
	trav := map[string]any{"title": "t", "artifact_type": "FILE", "object_key": storage.UserKeyPrefix(b) + "../" + a.String() + "/tasks/x"}
	if got := reg(authB, trav); got != http.StatusForbidden {
		t.Errorf("path traversal: want 403, got %d", got)
	}
	legacy := map[string]any{"title": "t", "artifact_type": "FILE", "object_key": fmt.Sprintf("tasks/%s/outputs/%s", taskA, "secret.txt")}
	if got := reg(authB, legacy); got != http.StatusForbidden {
		t.Errorf("unprefixed legacy-style key: want 403, got %d", got)
	}
	missing := map[string]any{"title": "t", "artifact_type": "FILE", "object_key": storage.UserKeyPrefix(b) + "does-not-exist"}
	if got := reg(authB, missing); got != http.StatusBadRequest && got != http.StatusUnprocessableEntity {
		t.Errorf("registering a nonexistent object: want 4xx validation, got %d", got)
	}
	// client-supplied storage_uri is ignored: the stored URI is derived server-side.
	ownKey := storage.UserKeyPrefix(b) + "mine.txt"
	if _, err := env.StorageProvider.Upload(ctx, storage.UploadInput{Bucket: env.StorageService.Bucket(), Key: ownKey, Body: strings.NewReader("mine"), ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	b2, _ := json.Marshal(map[string]any{"title": "mine", "artifact_type": "FILE", "object_key": ownKey, "storage_uri": "s3://attacker/evil"})
	code, body, _ = call(t, "POST", ts.URL+"/api/v1/artifacts", authB, b2, map[string]string{"Content-Type": "application/json"})
	if code != http.StatusCreated {
		t.Fatalf("B registering own object: %d %s", code, body)
	}
	var own artResp
	_ = json.Unmarshal(body, &own)
	if strings.Contains(own.Data.StorageURI, "attacker") || !strings.HasSuffix(own.Data.StorageURI, ownKey) {
		t.Fatalf("storage_uri not derived server-side: %q", own.Data.StorageURI)
	}

	// Negative: B cannot attach an upload to A's task; and can to their own.
	if code, _ := uploadAs(t, ts, authB, "x.txt", &taskA, "x"); code != http.StatusNotFound {
		t.Errorf("upload to A's task by B: want 404, got %d", code)
	}
	if code, _ := uploadAs(t, ts, authB, "x.txt", &taskB, "x"); code != http.StatusCreated {
		t.Errorf("upload to own task: want 201, got %d", code)
	}

	// Defence in depth: a poisoned row already in the DB (pre-fix data) cannot reach A's object.
	poison := uuid.New()
	_, err := env.DB.Pool.Exec(ctx, `INSERT INTO artifacts (id,user_id,title,artifact_type,storage_uri,storage_provider,bucket,object_key)
		VALUES ($1,$2,'poison','FILE','s3://x/y','S3',$3,$4)`, poison, b, *artA.Bucket, *artA.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/download", "/presign"} {
		if got, _, _ := call(t, "GET", fmt.Sprintf("%s/api/v1/artifacts/%s%s", ts.URL, poison, p), authB, nil, nil); got != http.StatusNotFound {
			t.Errorf("poisoned row %s: want 404, got %d", p, got)
		}
	}
	if got, _, _ := call(t, "DELETE", fmt.Sprintf("%s/api/v1/artifacts/%s", ts.URL, poison), authB, nil, nil); got != http.StatusNoContent {
		t.Errorf("poisoned row delete (row only): want 204, got %d", got)
	}
	if _, err := env.StorageProvider.Head(ctx, storage.HeadInput{Bucket: *artA.Bucket, Key: *artA.ObjectKey}); err != nil {
		t.Fatalf("deleting a poisoned row must not delete A's object: %v", err)
	}

	// Listing is scoped to the caller.
	_, listBody, _ := call(t, "GET", ts.URL+"/api/v1/artifacts", authB, nil, nil)
	if strings.Contains(string(listBody), artA.ID.String()) {
		t.Fatal("B's artifact list leaks A's artifact")
	}
}

func TestArtifactUploadSizeCap(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ts := secServer(t, env, func(c *config.Config) { c.MaxUploadBytes = 1024 })
	u := CreateTestUser(t, env.DB, "cap@example.com")
	auth := GenerateTestJWT(u, "cap@example.com")

	if code, _ := uploadAs(t, ts, auth, "ok.txt", nil, strings.Repeat("a", 512)); code != http.StatusCreated {
		t.Fatalf("under the cap: %d", code)
	}
	if code, _ := uploadAs(t, ts, auth, "big.txt", nil, strings.Repeat("a", 4096)); code < 400 {
		t.Fatalf("over the cap must be rejected, got %d", code)
	}
}

func createTaskFor(t *testing.T, env *TestEnv, user uuid.UUID) uuid.UUID {
	t.Helper()
	task, err := env.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: user, Title: "sec task", Goal: "g", ManagerDomain: "JJ",
		Status: domain.TaskStatusCreated, Priority: domain.TaskPriorityNormalP2,
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

// The shared runner serves every tenant: an authenticated ordinary user must not be able to stop it.
func TestRunnerSleepIsOperatorOnly(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	mgr := runner.NewMockInstanceManager(runner.RunnerStateRunning)
	ts := secServerWithRunner(t, env, func(c *config.Config) { c.WorkerAPIKey = "operator-key-0123456789-0123456789-abcdef" }, mgr)
	u := CreateTestUser(t, env.DB, "sleep@example.com")
	url := ts.URL + "/api/v1/runner/sleep"

	cases := []struct {
		name string
		auth string
		hdr  map[string]string
		want int
	}{
		{"no credentials", "", nil, http.StatusUnauthorized},
		{"valid user JWT", GenerateTestJWT(u, "sleep@example.com"), nil, http.StatusUnauthorized},
		{"user JWT in X-Worker-Key", "", map[string]string{"X-Worker-Key": GenerateTestJWT(u, "sleep@example.com")[len("Bearer "):]}, http.StatusUnauthorized},
		{"wrong worker key", "", map[string]string{"X-Worker-Key": "operator-key-0123456789-0123456789-abcdeX"}, http.StatusUnauthorized},
		{"prefix of worker key", "", map[string]string{"X-Worker-Key": "operator-key-0123456789"}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		if code, _, _ := call(t, "POST", url, c.auth, nil, c.hdr); code != c.want {
			t.Errorf("%s: got %d want %d", c.name, code, c.want)
		}
	}
	if mgr.StopCalls != 0 {
		t.Fatalf("runner stopped %d time(s) by unauthorized callers", mgr.StopCalls)
	}
	if code, _, _ := call(t, "POST", url, "", nil, map[string]string{"X-Worker-Key": "operator-key-0123456789-0123456789-abcdef"}); code != http.StatusOK {
		t.Fatalf("operator key: got %d want 200", code)
	}
	if mgr.StopCalls != 1 {
		t.Fatalf("operator call should stop runner once, got %d", mgr.StopCalls)
	}
	// Ordinary users can still wake the runner (cost-bounded by cooldown) and read status.
	if code, _, _ := call(t, "GET", ts.URL+"/api/v1/runner/status", GenerateTestJWT(u, "sleep@example.com"), nil, nil); code != http.StatusOK {
		t.Fatalf("status for user: %d", code)
	}
}
