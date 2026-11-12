package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// These tests run the REAL infra/ops/create-runtime-roles.sql against Postgres and connect as the
// resulting griffin_app (RLS enforced) and griffin_system (explicit system path) roles.

type rlsEnv struct {
	*TestEnv
	UserPool   *pgxpool.Pool
	SystemPool *pgxpool.Pool
	UserTx     *database.UserTxManager
	Scoped     *database.Scoped
}

func roleDSN(t *testing.T, role, pw, extraQuery string) string {
	t.Helper()
	base := os.Getenv("GRIFFIN_TEST_DB_URL")
	if base == "" {
		base = TestDBURL
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, pw)
	if extraQuery != "" {
		q := u.Query()
		for _, kv := range strings.Split(extraQuery, "&") {
			p := strings.SplitN(kv, "=", 2)
			q.Set(p[0], p[1])
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func setupRLS(t *testing.T, userMaxConns int32, userQuery string) *rlsEnv {
	t.Helper()
	env := SetupTestEnv(t)
	t.Cleanup(env.Teardown)
	ctx := context.Background()

	// Idempotent cleanup of roles from a previous run, then apply the production role SQL verbatim.
	if _, err := env.DB.Pool.Exec(ctx, `DO $$ DECLARE r text; BEGIN
		FOREACH r IN ARRAY ARRAY['griffin_app','griffin_system'] LOOP
			IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
				EXECUTE format('DROP OWNED BY %I', r); EXECUTE format('DROP ROLE %I', r);
			END IF;
		END LOOP; END $$;`); err != nil {
		t.Fatalf("role cleanup: %v", err)
	}
	sqlBytes, err := os.ReadFile("../infra/ops/create-runtime-roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.NewReplacer(":'app_pw'", "'app-pw-test'", ":'system_pw'", "'system-pw-test'").Replace(string(sqlBytes))
	if _, err := env.DB.Pool.Exec(ctx, sqlText); err != nil {
		t.Fatalf("apply create-runtime-roles.sql: %v", err)
	}

	userDB, err := database.ConnectWithOptions(ctx, roleDSN(t, "griffin_app", "app-pw-test", userQuery), database.PoolOptions{MaxConns: userMaxConns, MinConns: 0})
	if err != nil {
		t.Fatalf("connect as griffin_app: %v", err)
	}
	t.Cleanup(userDB.Close)
	sysDB, err := database.ConnectWithOptions(ctx, roleDSN(t, "griffin_system", "system-pw-test", ""), database.PoolOptions{MaxConns: 4, MinConns: 0})
	if err != nil {
		t.Fatalf("connect as griffin_system: %v", err)
	}
	t.Cleanup(sysDB.Close)

	return &rlsEnv{
		TestEnv: env, UserPool: userDB.Pool, SystemPool: sysDB.Pool,
		UserTx: &database.UserTxManager{Pool: userDB.Pool}, Scoped: database.NewScoped(sysDB.Pool),
	}
}

func (e *rlsEnv) server(t *testing.T) *httptest.Server {
	t.Helper()
	taskRepo := repository.NewPostgresTaskRepository(e.Scoped)
	approvalRepo := repository.NewPostgresApprovalRepository(e.Scoped)
	agentRepo := repository.NewPostgresAgentRepository(e.Scoped)
	artifactRepo := repository.NewPostgresArtifactRepository(e.Scoped)
	workerDefRepo := repository.NewPostgresWorkerDefinitionRepository(e.Scoped)
	orchRepo := repository.NewPostgresOrchestrationRepository(e.Scoped)
	engine := service.NewTaskEngine(taskRepo, approvalRepo, nil)
	factory := service.NewAgentFactoryService(workerDefRepo, e.ManagerReg, nil)
	orch := service.NewJJOrchestrator(engine, orchRepo, taskRepo, e.ManagerReg, factory, nil, nil)
	storageSvc := storage.NewStorageService(storage.NewMockStorageProvider(), artifactRepo, "griffin-test-artifacts", nil)
	cfg := *e.Config
	cfg.MaxUploadBytes = 1 << 20
	srv := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: e.DB, Engine: engine, TaskRepo: taskRepo, ApprovalRepo: approvalRepo, AgentRepo: agentRepo,
		ArtifactRepo: artifactRepo, StorageService: storageSvc, WorkerDefRepo: workerDefRepo, OrchRepo: orchRepo,
		Orchestrator: orch, Factory: factory, ManagerRegistry: e.ManagerReg, UserTx: e.UserTx,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func countRows(t *testing.T, ctx context.Context, q database.Querier, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// ---- 1. role attributes and grants ----

func TestRuntimeRolesAreLeastPrivilege(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()

	type attrs struct{ super, bypass bool }
	got := map[string]attrs{}
	rows, err := e.DB.Pool.Query(ctx, `SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname IN ('griffin_app','griffin_system')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		var a attrs
		if err := rows.Scan(&n, &a.super, &a.bypass); err != nil {
			t.Fatal(err)
		}
		got[n] = a
	}
	rows.Close()
	if a := got["griffin_app"]; a.super || a.bypass {
		t.Fatalf("griffin_app must be NOSUPERUSER NOBYPASSRLS, got %+v", a)
	}
	if a := got["griffin_system"]; a.super || !a.bypass {
		t.Fatalf("griffin_system must be NOSUPERUSER BYPASSRLS, got %+v", a)
	}

	// Startup guard: accepts the RLS role, refuses the system role and the admin/superuser role.
	if err := database.AssertRoleIsolation(ctx, e.UserPool); err != nil {
		t.Fatalf("griffin_app should pass the guard: %v", err)
	}
	if err := database.AssertRoleIsolation(ctx, e.SystemPool); err == nil {
		t.Fatal("guard must refuse a BYPASSRLS role as the user pool")
	}
	if err := database.AssertRoleIsolation(ctx, e.DB.Pool); err == nil {
		t.Fatal("guard must refuse a superuser/admin role as the user pool")
	}

	// Least-privilege grants: everything below must be denied for BOTH runtime roles.
	for name, pool := range map[string]*pgxpool.Pool{"griffin_app": e.UserPool, "griffin_system": e.SystemPool} {
		denied := []string{
			`CREATE TABLE public.evil (id int)`,
			`DROP TABLE public.tasks`,
			`ALTER TABLE public.tasks DISABLE ROW LEVEL SECURITY`,
			`TRUNCATE public.tasks`,
			`SELECT count(*) FROM auth.users`,
			`INSERT INTO auth.users (id, email) VALUES (gen_random_uuid(), 'x@example.com')`,
			`INSERT INTO public.schema_migrations (version, checksum) VALUES ('999_evil', 'x')`,
			`UPDATE public.schema_migrations SET checksum = 'x'`,
			`DELETE FROM public.schema_migrations`,
			`CREATE ROLE evil_role`,
			`SET ROLE postgres`,
			`CREATE SCHEMA evil`,
		}
		for _, stmt := range denied {
			if _, err := pool.Exec(ctx, stmt); err == nil {
				t.Errorf("%s: statement should be denied: %s", name, stmt)
			}
		}
	}
	if _, err := e.SystemPool.Exec(ctx, `SELECT count(*) FROM public.tasks`); err != nil {
		t.Fatalf("griffin_system DML: %v", err)
	}
}

// ---- 2. database-level isolation ----

func seedTwoUsers(t *testing.T, e *rlsEnv) (a, b uuid.UUID, taskA, taskB *domain.Task) {
	t.Helper()
	ctx := context.Background()
	a = CreateTestUser(t, e.DB, "rls_a@example.com")
	b = CreateTestUser(t, e.DB, "rls_b@example.com")
	var err error
	if taskA, err = e.Engine.CreateTask(ctx, &domain.Task{UserID: a, Title: "A secret", Goal: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	if taskB, err = e.Engine.CreateTask(ctx, &domain.Task{UserID: b, Title: "B secret", Goal: "b"}, nil); err != nil {
		t.Fatal(err)
	}
	return
}

func TestUserATransactionCannotTouchUserBData(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	a, b, taskA, taskB := seedTwoUsers(t, e)

	tx, err := e.UserTx.Begin(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var uid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT auth.uid()`).Scan(&uid); err != nil || uid != a {
		t.Fatalf("auth.uid() = %v (err %v), want %v", uid, err, a)
	}
	if n := countRows(t, ctx, tx, `SELECT count(*) FROM tasks`); n != 1 {
		t.Fatalf("user A sees %d tasks, want exactly 1", n)
	}
	if n := countRows(t, ctx, tx, `SELECT count(*) FROM tasks WHERE id = $1`, taskB.ID); n != 0 {
		t.Fatal("user A can read user B's task")
	}
	if n := countRows(t, ctx, tx, `SELECT count(*) FROM tasks WHERE id = $1`, taskA.ID); n != 1 {
		t.Fatal("user A cannot read own task")
	}
	ct, err := tx.Exec(ctx, `UPDATE tasks SET title = 'pwned' WHERE id = $1`, taskB.ID)
	if err != nil || ct.RowsAffected() != 0 {
		t.Fatalf("user A updated B's task: rows=%d err=%v", ct.RowsAffected(), err)
	}
	ct, err = tx.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskB.ID)
	if err != nil || ct.RowsAffected() != 0 {
		t.Fatalf("user A deleted B's task: rows=%d err=%v", ct.RowsAffected(), err)
	}
	// Re-homing own row to another tenant must be rejected by WITH CHECK.
	if _, err := tx.Exec(ctx, `UPDATE tasks SET user_id = $1 WHERE id = $2`, b, taskA.ID); err == nil {
		t.Fatal("user A moved own task into user B's tenant")
	}
}

func TestUserCannotInsertForAnotherTenant(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	a, b, _, _ := seedTwoUsers(t, e)
	tx, err := e.UserTx.Begin(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO tasks (id, user_id, title, goal, status, manager_domain, priority) VALUES (gen_random_uuid(), $1, 't', 'g', 'QUEUED', 'JJ', 'NORMAL_P2')`, b); err == nil {
		t.Fatal("user A inserted a task owned by user B")
	}
}

// Every tenant table must hide other tenants' rows from an RLS-enforced session.
func TestEveryTenantTableIsRLSProtected(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	seedTwoUsers(t, e)

	rows, err := e.DB.Pool.Query(ctx, `SELECT relname, relrowsecurity, relforcerowsecurity FROM pg_class
		WHERE relnamespace = 'public'::regnamespace AND relkind = 'r' AND relname <> 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		var rls, force bool
		if err := rows.Scan(&n, &rls, &force); err != nil {
			t.Fatal(err)
		}
		if !rls || !force {
			t.Errorf("table %s: relrowsecurity=%t relforcerowsecurity=%t", n, rls, force)
		}
		tables = append(tables, n)
	}
	rows.Close()
	if len(tables) < 15 {
		t.Fatalf("expected the full tenant schema, found %d tables", len(tables))
	}
	// A fresh user owns no data, so every table is empty for them (profiles: only their own row).
	c := CreateTestUser(t, e.DB, "rls_c@example.com")
	tx, err := e.UserTx.Begin(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, tbl := range tables {
		var n int
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM public.%s`, tbl)).Scan(&n); err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
		if tbl == "profiles" && n == 1 {
			continue
		}
		if n != 0 {
			t.Errorf("table %s leaks %d row(s) to an unrelated user", tbl, n)
		}
	}
}

func TestNoUserContextSeesNothing(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	seedTwoUsers(t, e)
	// Plain griffin_app connection with no claims: auth.uid() is NULL, so RLS hides every row.
	if n := countRows(t, ctx, e.UserPool, `SELECT count(*) FROM tasks`); n != 0 {
		t.Fatalf("no-claims session sees %d tasks", n)
	}
	var uid *uuid.UUID
	if err := e.UserPool.QueryRow(ctx, `SELECT auth.uid()`).Scan(&uid); err != nil || uid != nil {
		t.Fatalf("auth.uid() without claims = %v, err %v", uid, err)
	}
	if _, err := e.UserTx.Begin(ctx, uuid.Nil); err == nil {
		t.Fatal("must refuse a user transaction for the nil user id")
	}
}

// ---- 3. no leakage across pooled connections / transactions ----

func assertNoLeakedClaims(t *testing.T, e *rlsEnv) {
	t.Helper()
	var sub, claims *string
	if err := e.UserPool.QueryRow(context.Background(),
		`SELECT NULLIF(current_setting('request.jwt.claim.sub', true), ''), NULLIF(current_setting('request.jwt.claims', true), '')`).Scan(&sub, &claims); err != nil {
		t.Fatal(err)
	}
	if sub != nil || claims != nil {
		t.Fatalf("identity leaked onto the pooled connection after the transaction ended: sub=%v claims=%v", sub, claims)
	}
}

func runLeakChecks(t *testing.T, e *rlsEnv) {
	ctx := context.Background()
	a, b, _, _ := seedTwoUsers(t, e)
	for i := 0; i < 20; i++ {
		for _, u := range []uuid.UUID{a, b} {
			tx, err := e.UserTx.Begin(ctx, u)
			if err != nil {
				t.Fatal(err)
			}
			var uid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT auth.uid()`).Scan(&uid); err != nil || uid != u {
				t.Fatalf("iteration %d: uid=%v want %v err=%v", i, uid, u, err)
			}
			if n := countRows(t, ctx, tx, `SELECT count(*) FROM tasks`); n != 1 {
				t.Fatalf("iteration %d: %v sees %d tasks", i, u, n)
			}
			if i%2 == 0 {
				_ = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			assertNoLeakedClaims(t, e)
		}
	}
	// Concurrent alternating users on a tiny pool: every goroutine only ever sees its own identity.
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				u := a
				if (g+i)%2 == 1 {
					u = b
				}
				tx, err := e.UserTx.Begin(ctx, u)
				if err != nil {
					errs <- err
					return
				}
				var uid uuid.UUID
				var n int
				if err := tx.QueryRow(ctx, `SELECT auth.uid(), (SELECT count(*) FROM tasks WHERE user_id <> auth.uid())`).Scan(&uid, &n); err != nil || uid != u || n != 0 {
					errs <- fmt.Errorf("goroutine %d: uid=%v want %v foreign=%d err=%v", g, uid, u, n, err)
					_ = tx.Rollback(ctx)
					return
				}
				_ = tx.Commit(ctx)
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	assertNoLeakedClaims(t, e)
}

func TestClaimsDoNotLeakAcrossPooledConnections_SingleConnection(t *testing.T) {
	// MaxConns=1 forces every transaction onto the very same physical connection.
	runLeakChecks(t, setupRLS(t, 1, ""))
}

func TestClaimsDoNotLeakAcrossPooledConnections_SmallPool(t *testing.T) {
	runLeakChecks(t, setupRLS(t, 2, ""))
}

// Supabase's transaction pooler (:6543) requires the simple protocol; isolation must hold there too.
func TestClaimsDoNotLeak_SimpleProtocol(t *testing.T) {
	runLeakChecks(t, setupRLS(t, 1, "default_query_exec_mode=simple_protocol&statement_cache_capacity=0"))
}

// ---- 4. system path is explicit ----

func TestScopedRepositoriesFailClosedAndSystemIsExplicit(t *testing.T) {
	e := setupRLS(t, 2, "")
	a, _, taskA, _ := seedTwoUsers(t, e)
	repo := repository.NewPostgresTaskRepository(e.Scoped)

	// No scope: denied, never a silent privileged default.
	if _, err := repo.GetTaskByID(context.Background(), taskA.ID); err == nil || !strings.Contains(err.Error(), "no authorized scope") {
		t.Fatalf("unscoped repository call must be denied, got %v", err)
	}
	if _, err := repo.ListTasks(context.Background(), repository.TaskFilter{}); err == nil {
		t.Fatal("unscoped list must be denied")
	}

	// Explicit system scope sees every tenant (this is what claim/reaper/tick need).
	sysTasks, err := repo.ListTasks(database.WithSystem(context.Background()), repository.TaskFilter{})
	if err != nil || len(sysTasks) != 2 {
		t.Fatalf("system scope: %d tasks err %v, want 2", len(sysTasks), err)
	}

	// A user transaction always wins, even if the context is also marked system.
	tx, err := e.UserTx.Begin(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	ctx := database.WithSystem(database.WithUserTx(context.Background(), tx))
	tasks, err := repo.ListTasks(ctx, repository.TaskFilter{})
	if err != nil || len(tasks) != 1 || tasks[0].UserID != a {
		t.Fatalf("user tx must restrict even a system-marked context: %d tasks err %v", len(tasks), err)
	}
}

// ---- 5. end to end over HTTP with RLS enforced ----

func doJSON(t *testing.T, ts *httptest.Server, method, path, auth string, hdr map[string]string, body any) (int, []byte) {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(payload))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestHTTPEndToEndWithRLSEnforced(t *testing.T) {
	e := setupRLS(t, 2, "")
	ts := e.server(t)
	a := CreateTestUser(t, e.DB, "http_a@example.com")
	b := CreateTestUser(t, e.DB, "http_b@example.com")
	jwtA, jwtB := GenerateTestJWT(a, "http_a@example.com"), GenerateTestJWT(b, "http_b@example.com")
	ctx := context.Background()

	code, body := doJSON(t, ts, "POST", "/api/v1/tasks", jwtA, nil, map[string]any{"title": "A via API", "goal": "g", "manager_domain": "JJ", "priority": "HIGH_P1"})
	if code != http.StatusCreated && code != http.StatusAccepted {
		t.Fatalf("create task as A: %d %s", code, body)
	}
	var env struct {
		Data struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Data.ID == uuid.Nil {
		t.Fatalf("no task id in response: %s", body)
	}
	if n := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE id=$1 AND user_id=$2`, env.Data.ID, a); n != 1 {
		t.Fatal("task not durably persisted for user A (must be committed before the response)")
	}

	if code, _ := doJSON(t, ts, "GET", "/api/v1/tasks/"+env.Data.ID.String(), jwtA, nil, nil); code != http.StatusOK {
		t.Fatalf("A reads own task: %d", code)
	}
	if code, _ := doJSON(t, ts, "GET", "/api/v1/tasks/"+env.Data.ID.String(), jwtB, nil, nil); code != http.StatusNotFound && code != http.StatusForbidden {
		t.Fatalf("B reading A's task: %d, want 404/403", code)
	}
	if code, _ := doJSON(t, ts, "POST", "/api/v1/tasks/"+env.Data.ID.String()+"/cancel", jwtB, nil, nil); code == http.StatusOK {
		t.Fatal("B cancelled A's task")
	}
	if code, body := doJSON(t, ts, "GET", "/api/v1/tasks", jwtB, nil, nil); code != http.StatusOK || bytes.Contains(body, []byte(env.Data.ID.String())) {
		t.Fatalf("B's list leaks A's task: %d %s", code, body)
	}
	// A 4xx must roll back (nothing persisted for a rejected request).
	before := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks`)
	if code, _ := doJSON(t, ts, "POST", "/api/v1/tasks", jwtA, nil, map[string]any{"goal": ""}); code < 400 {
		t.Fatalf("invalid task accepted: %d", code)
	}
	if after := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks`); after != before {
		t.Fatalf("rejected request left %d new row(s)", after-before)
	}

	// Worker-key routes are the explicit system path (cross-tenant claim); user JWTs cannot reach them.
	if code, _ := doJSON(t, ts, "POST", "/api/v1/workers/claim", jwtA, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("user JWT on worker route: %d", code)
	}
	if code, _ := doJSON(t, ts, "POST", "/api/v1/workers/claim", "", map[string]string{"X-Worker-Key": TestWorkerKey, "X-Worker-ID": "w1"}, nil); code != http.StatusOK {
		t.Fatalf("worker-key claim through system scope: %d", code)
	}
	if code, _ := doJSON(t, ts, "POST", "/api/v1/scheduler/tick", "", map[string]string{"X-Worker-Key": TestWorkerKey}, nil); code != http.StatusOK {
		t.Fatalf("scheduler tick through system scope: %d", code)
	}
}

// Production must refuse to serve user routes when no RLS pool is wired.
func TestProductionWithoutUserScopeRefusesUserRoutes(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ts := secServer(t, env, func(c *config.Config) { c.Environment = "production"; c.EnableDevLogin = false })
	u := CreateTestUser(t, env.DB, "prod_noscope@example.com")
	code, _, _ := call(t, "GET", ts.URL+"/api/v1/tasks", GenerateTestJWT(u, "prod_noscope@example.com"), nil, nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("production without UserTx must fail closed (503), got %d", code)
	}
}

// The approval repository opens its own transaction; inside a request transaction that becomes a
// savepoint. It must work for the owner under RLS and be impossible for another tenant.
func TestApprovalResponseUnderRLS_NestedTransaction(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	a := CreateTestUser(t, e.DB, "appr_a@example.com")
	b := CreateTestUser(t, e.DB, "appr_b@example.com")

	task, err := e.Engine.CreateTask(ctx, &domain.Task{UserID: a, Title: "Send email", Goal: "g"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Engine.ClaimTask(ctx, "w-rls", 60*1e9); err != nil {
		t.Fatal(err)
	}
	approval := &domain.Approval{
		ID: uuid.New(), UserID: a, TaskID: task.ID, ActionType: "SEND_EMAIL", RiskLevel: domain.ApprovalRiskHigh,
		Summary: "send", ActionPayload: json.RawMessage(`{}`),
	}
	if err := e.Engine.RequestApproval(ctx, task.ID, approval); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPostgresApprovalRepository(e.Scoped)
	status := func() string {
		var s string
		if err := e.DB.Pool.QueryRow(ctx, `SELECT status FROM approvals WHERE id=$1`, approval.ID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := status()

	// User B cannot respond to A's approval.
	txB, err := e.UserTx.Begin(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RespondToApproval(database.WithUserTx(ctx, txB), approval.ID, b, true, "pwn"); err == nil {
		t.Fatal("user B responded to user A's approval")
	}
	_ = txB.Rollback(ctx)
	if status() != before {
		t.Fatalf("approval status changed by another tenant: %s -> %s", before, status())
	}

	// Owner can, through the nested transaction, and the change commits with the request transaction.
	txA, err := e.UserTx.Begin(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.RespondToApproval(database.WithUserTx(ctx, txA), approval.ID, a, true, "ok")
	if err != nil || got == nil {
		t.Fatalf("owner approval failed: %v", err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if s := status(); s == before {
		t.Fatalf("approval not persisted after commit (still %s)", s)
	}
}

// JJ orchestration submit (plan + delegated tasks) runs in one request transaction under RLS; the
// plan and its child tasks belong to the caller and are invisible to another tenant.
func TestJJOrchestrationSubmitUnderRLS(t *testing.T) {
	e := setupRLS(t, 2, "")
	ts := e.server(t)
	a := CreateTestUser(t, e.DB, "jj_a@example.com")
	b := CreateTestUser(t, e.DB, "jj_b@example.com")
	jwtA, jwtB := GenerateTestJWT(a, "jj_a@example.com"), GenerateTestJWT(b, "jj_b@example.com")
	ctx := context.Background()

	code, body := doJSON(t, ts, "POST", "/api/v1/orchestrator/requests", jwtA, nil,
		map[string]any{"request_text": "Research distributed consensus protocols in Go", "priority": "HIGH_P1"})
	if code != http.StatusCreated && code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("JJ submit as A: %d %s", code, body)
	}
	var resp struct {
		Data struct {
			Orchestration domain.OrchestrationRecord `json:"orchestration"`
			Plan          domain.OrchestrationPlan   `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Data.Orchestration.ID == uuid.Nil || resp.Data.Plan.ID == uuid.Nil {
		t.Fatalf("no orchestration/plan in response: %s", body)
	}
	orchID, planID := resp.Data.Orchestration.ID, resp.Data.Plan.ID

	if n := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM orchestrations WHERE id=$1 AND user_id=$2`, orchID, a); n != 1 {
		t.Fatal("orchestration not durably persisted for A")
	}
	if n := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1`, a); n < 1 {
		t.Fatal("JJ created no delegated tasks for A")
	}
	if n := countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1`, b); n != 0 {
		t.Fatal("JJ created tasks under the wrong tenant")
	}
	if code, _ := doJSON(t, ts, "GET", "/api/v1/orchestrator/requests/"+orchID.String(), jwtA, nil, nil); code != http.StatusOK {
		t.Fatalf("A reads own orchestration: %d", code)
	}
	for _, path := range []string{"/api/v1/orchestrator/requests/" + orchID.String(), "/api/v1/orchestrator/plans/" + planID.String()} {
		if code, _ := doJSON(t, ts, "GET", path, jwtB, nil, nil); code != http.StatusNotFound && code != http.StatusForbidden {
			t.Fatalf("B reading A's %s: %d, want 404/403", path, code)
		}
	}
}

// Editing a pending message must rewrite the task's action and the approval together, so the digest the owner
// approves is the digest of the edited text. Another tenant can neither edit nor see it.
func TestEditPendingApprovalRebindsTheDigest(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	a := CreateTestUser(t, e.DB, "edit_a@example.com")
	b := CreateTestUser(t, e.DB, "edit_b@example.com")

	payload := json.RawMessage(`{"requires":["messaging"],"messaging":{"channel":"whatsapp","op":"send","contact":"Bro","text":"hi"}}`)
	task, err := e.Engine.CreateTask(ctx, &domain.Task{UserID: a, Title: "Message Bro", Goal: "g", InputPayload: payload}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Engine.ClaimTask(ctx, "w-edit", 60*1e9); err != nil {
		t.Fatal(err)
	}
	old, _ := domain.ParseMessagingAction(payload)
	approval := &domain.Approval{
		ID: uuid.New(), UserID: a, TaskID: task.ID, ActionType: old.ApprovalActionType(), RiskLevel: domain.ApprovalRiskHigh,
		Summary: old.Summary(), ActionPayload: json.RawMessage(`{"digest":"` + old.Digest() + `","text":"hi"}`),
	}
	if err := e.Engine.RequestApproval(ctx, task.ID, approval); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPostgresApprovalRepository(e.Scoped)

	txB, _ := e.UserTx.Begin(ctx, b)
	if _, err := repo.EditPendingApproval(database.WithUserTx(ctx, txB), approval.ID, b, "pwn"); err == nil {
		t.Fatal("another tenant edited the approval")
	}
	_ = txB.Rollback(ctx)

	txA, _ := e.UserTx.Begin(ctx, a)
	got, err := repo.EditPendingApproval(database.WithUserTx(ctx, txA), approval.ID, a, "see you at 8?")
	if err != nil {
		t.Fatal(err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var stored json.RawMessage
	if err := e.DB.Pool.QueryRow(ctx, `SELECT input_payload FROM tasks WHERE id=$1`, task.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	act, _ := domain.ParseMessagingAction(stored)
	if act.Text != "see you at 8?" || got.ActionType != act.ApprovalActionType() || got.ActionType == old.ApprovalActionType() {
		t.Fatalf("approval and task must agree on the edited action: %q %s", act.Text, got.ActionType)
	}
}
