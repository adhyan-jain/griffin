package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// finish marks a task finished the given time ago, with an answer.
func (e *hostEnv) finish(t *testing.T, id uuid.UUID, status string, ago time.Duration, answer string) {
	t.Helper()
	at := time.Now().Add(-ago)
	out, _ := json.Marshal(map[string]string{"response": answer})
	if _, err := e.DB.Pool.Exec(context.Background(),
		`UPDATE tasks SET status = $2::task_status, completed_at = $3, updated_at = $3, output_payload = $4::jsonb WHERE id = $1`,
		id, status, at, string(out)); err != nil {
		t.Fatalf("finish %s: %v", id, err)
	}
}

func (e *hostEnv) listIDs(t *testing.T, query string) map[string]bool {
	t.Helper()
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/tasks?limit=200"+query, nil)
	req.Header.Set("Authorization", e.auth)
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range body.Data {
		ids[d.ID] = true
	}
	return ids
}

func TestOwnerArchivesRestoresAndDeletesTasks(t *testing.T) {
	e := setupHostEnv(t)
	done := e.newTask(t, "Summarise a paper")
	e.finish(t, done.ID, "COMPLETED", time.Hour, "A real answer.")
	active := e.newTask(t, "Still queued")

	if code, out := e.do(t, "POST", "/api/v1/tasks/"+active.ID.String()+"/archive", e.auth, nil); code != http.StatusConflict {
		t.Fatalf("a task still in flight cannot be archived: %d %v", code, out)
	}
	if code, out := e.do(t, "POST", "/api/v1/tasks/"+done.ID.String()+"/archive", e.auth, nil); code != http.StatusOK {
		t.Fatalf("archive: %d %v", code, out)
	}
	if ids := e.listIDs(t, ""); ids[done.ID.String()] || !ids[active.ID.String()] {
		t.Fatalf("the normal list must hide the archived task: %v", ids)
	}
	if ids := e.listIDs(t, "&archived=true"); !ids[done.ID.String()] || ids[active.ID.String()] {
		t.Fatalf("the archived list holds only archived tasks: %v", ids)
	}
	if code, _ := e.do(t, "GET", "/api/v1/tasks/"+done.ID.String(), e.auth, nil); code != http.StatusOK {
		t.Fatalf("an archived task can still be opened: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/v1/tasks/"+done.ID.String()+"/archive", e.auth, nil); code != http.StatusConflict {
		t.Fatalf("archiving twice is a conflict, not a silent success: %d", code)
	}

	if code, out := e.do(t, "POST", "/api/v1/tasks/"+done.ID.String()+"/unarchive", e.auth, nil); code != http.StatusOK {
		t.Fatalf("unarchive: %d %v", code, out)
	}
	if ids := e.listIDs(t, ""); !ids[done.ID.String()] {
		t.Fatalf("an unarchived task is back in the normal list: %v", ids)
	}
	if code, _ := e.do(t, "POST", "/api/v1/tasks/"+done.ID.String()+"/unarchive", e.auth, nil); code != http.StatusConflict {
		t.Fatalf("unarchiving a task that is not archived is a conflict: %d", code)
	}

	// Delete all archived: only archived tasks, only the caller's.
	e.do(t, "POST", "/api/v1/tasks/"+done.ID.String()+"/archive", e.auth, nil)
	otherEmail := "archive_other@example.com"
	other := CreateTestUser(t, e.DB, otherEmail)
	foreign, err := e.Engine.CreateTask(context.Background(), &domain.Task{UserID: other, Title: "Theirs", Goal: "g", Priority: domain.TaskPriorityNormalP2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.finish(t, foreign.ID, "COMPLETED", time.Hour, "theirs")
	if _, err := e.DB.Pool.Exec(context.Background(), `UPDATE tasks SET archived_at = now() WHERE id = $1`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "POST", "/api/v1/tasks/"+foreign.ID.String()+"/archive", e.auth, nil); code != http.StatusNotFound {
		t.Fatalf("another owner's task is not found: %d", code)
	}
	code, out := e.do(t, "POST", "/api/v1/tasks/archived/delete", e.auth, nil)
	if code != http.StatusOK || unwrap(out)["deleted"] != float64(1) {
		t.Fatalf("delete archived must remove exactly the caller's one archived task: %d %v", code, out)
	}
	if _, err := e.TaskRepo.GetTaskByID(context.Background(), done.ID); err == nil {
		t.Fatal("the archived task must be gone")
	}
	if _, err := e.TaskRepo.GetTaskByID(context.Background(), active.ID); err != nil {
		t.Fatalf("an unarchived task must stay: %v", err)
	}
	if _, err := e.TaskRepo.GetTaskByID(context.Background(), foreign.ID); err != nil {
		t.Fatalf("another owner's archived task must stay: %v", err)
	}
}

func TestArchivingAPlanArchivesItsSubtasks(t *testing.T) {
	e := setupHostEnv(t)
	root := e.newTask(t, "A plan")
	child, err := e.Engine.CreateTask(context.Background(), &domain.Task{UserID: e.userID, ParentID: &root.ID, Title: "step", Goal: "g", Priority: domain.TaskPriorityNormalP2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.finish(t, root.ID, "COMPLETED", time.Hour, "done")
	if code, _ := e.do(t, "POST", "/api/v1/tasks/"+root.ID.String()+"/archive", e.auth, nil); code != http.StatusConflict {
		t.Fatalf("a plan with work still in flight stays: %d", code)
	}
	e.finish(t, child.ID, "COMPLETED", time.Hour, "done")
	if code, out := e.do(t, "POST", "/api/v1/tasks/"+root.ID.String()+"/archive", e.auth, nil); code != http.StatusOK || unwrap(out)["archived"] != float64(2) {
		t.Fatalf("the plan and its subtask are archived together: %d %v", code, out)
	}
	if ids := e.listIDs(t, "&parent_id="+root.ID.String()); !ids[child.ID.String()] {
		t.Fatalf("an archived plan still shows its subtasks: %v", ids)
	}
	if code, _ := e.do(t, "POST", "/api/v1/tasks/"+root.ID.String()+"/unarchive", e.auth, nil); code != http.StatusOK {
		t.Fatal("unarchive the plan")
	}
	var n int
	if err := e.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE archived_at IS NOT NULL`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unarchiving the plan restores its subtasks: %d %v", n, err)
	}
}

// A host claiming work tidies the owner's old tasks, once an hour, and never the latest tracker run.
func TestHostClaimAutoArchivesOldMinorTasks(t *testing.T) {
	e := setupHostEnv(t)
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")

	old := e.newTask(t, "Summarise a paper")
	e.finish(t, old.ID, "COMPLETED", 5*24*time.Hour, "A long enough answer. A long enough answer. A long enough answer. A long enough answer. A long enough answer. A long enough answer. A long enough answer. A long enough answer.")
	recent := e.newTask(t, "Summarise another paper")
	e.finish(t, recent.ID, "COMPLETED", 24*time.Hour, "answer")
	junk := e.newTask(t, "Look up a thing")
	e.finish(t, junk.ID, "COMPLETED", time.Minute, "I couldn't find anything.")
	tracker := e.newTask(t, "Find hackathons")
	e.finish(t, tracker.ID, "COMPLETED", 9*24*time.Hour, "I couldn't find anything.")
	routine := e.newTask(t, "LISTINGS SYNC")
	e.finish(t, routine.ID, "COMPLETED", 9*24*time.Hour, "synced")

	if code, out := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusNoContent {
		t.Fatalf("claim with nothing queued: %d %v", code, out)
	}
	archived := e.listIDs(t, "&archived=true")
	if !archived[old.ID.String()] || !archived[junk.ID.String()] {
		t.Fatalf("old and junk tasks are archived: %v", archived)
	}
	if archived[recent.ID.String()] || archived[tracker.ID.String()] || archived[routine.ID.String()] {
		t.Fatalf("recent tasks, the latest tracker run and routines stay: %v", archived)
	}

	// The hour guard: a task that becomes old now waits for the next hour.
	e.finish(t, recent.ID, "COMPLETED", 5*24*time.Hour, "answer")
	e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
	if e.listIDs(t, "&archived=true")[recent.ID.String()] {
		t.Fatal("auto-archive runs at most once an hour")
	}
}

// Production reaches Postgres through a transaction pooler (simple protocol): the archive queries pass uuid arrays.
func TestArchiveQueriesWorkThroughThePooler(t *testing.T) {
	e := setupHostEnv(t)
	ctx := context.Background()
	url := os.Getenv("GRIFFIN_TEST_DB_URL")
	if url == "" {
		url = TestDBURL
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := repository.NewPostgresTaskRepository(pool)

	task := e.newTask(t, "Summarise a paper")
	e.finish(t, task.ID, "COMPLETED", 5*24*time.Hour, "answer")
	cands, err := repo.ArchiveCandidates(ctx, e.userID)
	if err != nil || len(cands) != 1 {
		t.Fatalf("candidates through the pooler: %d %v", len(cands), err)
	}
	ids := domain.AutoArchiveIDs(cands, time.Now())
	if n, err := repo.ArchiveTasks(ctx, e.userID, ids, time.Now()); err != nil || n != 1 {
		t.Fatalf("archive through the pooler: %d %v", n, err)
	}
	if n, err := repo.ArchiveTasks(ctx, e.userID, ids, time.Now()); err != nil || n != 0 {
		t.Fatalf("archiving again is a no-op: %d %v", n, err)
	}
	if n, err := repo.UnarchiveTask(ctx, e.userID, task.ID); err != nil || n != 1 {
		t.Fatalf("unarchive through the pooler: %d %v", n, err)
	}
	if _, err := repo.ArchiveTasks(ctx, e.userID, ids, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.DeleteArchived(ctx, e.userID); err != nil || n != 1 {
		t.Fatalf("delete archived through the pooler: %d %v", n, err)
	}
}
