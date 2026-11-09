package handover

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/griffin/core/internal/hostagent"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Owner", "GIT_AUTHOR_EMAIL=o@x", "GIT_COMMITTER_NAME=Owner", "GIT_COMMITTER_EMAIL=o@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo makes a repo on branch main with one commit, optionally with a bare remote called origin.
func newRepo(t *testing.T, withRemote bool) (repo, remote string) {
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "app.go", "package main\n")
	write(t, repo, ".gitignore", "node_modules/\n*.log\n")
	write(t, repo, "settings.key", "HEAD-VERSION-OF-A-TRACKED-KEY-FILE\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "first")
	if withRemote {
		remote = t.TempDir()
		git(t, remote, "init", "-q", "--bare", "-b", "main")
		git(t, repo, "remote", "add", "origin", remote)
		git(t, repo, "push", "-q", "origin", "main")
	}
	return repo, remote
}

func opts(push bool) SnapshotOptions {
	return SnapshotOptions{Branch: "griffin/handover-test", Push: push, Doc: func(f SnapshotFacts) string {
		return BuildDoc(DocContext{Host: "laptop", Battery: 5, Level: LevelPush, At: time.Now(), Tasks: []string{"Research X"}}, f)
	}}
}

func TestSnapshotSavesWorkAndNeverTouchesTheOwnersBranchIndexOrFiles(t *testing.T) {
	repo, _ := newRepo(t, false)
	write(t, repo, "app.go", "package main\n// edited, unstaged\n")
	write(t, repo, "new_feature.go", "package main\n// brand new\n")
	write(t, repo, "staged.go", "package main\n// staged by the owner\n")
	git(t, repo, "add", "staged.go")
	write(t, repo, "debug.log", "ignored\n")
	write(t, repo, ".env", "API_TOKEN=super-secret-token\n")
	write(t, repo, "id_rsa", "-----BEGIN PRIVATE KEY-----\n")
	write(t, repo, "settings.key", "MODIFIED-SECRET-IN-A-TRACKED-FILE\n")
	big := make([]byte, 6<<20)
	if err := os.WriteFile(filepath.Join(repo, "huge.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	beforeStatus := git(t, repo, "status", "--porcelain=v1")
	beforeBranch := git(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "diff", "--cached")

	res, err := Snapshot(context.Background(), repo, opts(false))
	if err != nil {
		t.Fatal(err)
	}

	// 1. the owner's world is exactly as it was
	if got := git(t, repo, "status", "--porcelain=v1"); got != beforeStatus {
		t.Errorf("working tree/index changed:\nbefore:\n%s\nafter:\n%s", beforeStatus, got)
	}
	if git(t, repo, "rev-parse", "--abbrev-ref", "HEAD") != beforeBranch || git(t, repo, "rev-parse", "HEAD") != beforeHead {
		t.Error("the owner's branch and HEAD must not move")
	}
	if git(t, repo, "diff", "--cached") != beforeIndex {
		t.Error("the owner's staging area must not change")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.go")); !strings.Contains(string(b), "edited, unstaged") {
		t.Error("working files must not be touched")
	}

	// 2. the handover branch has the work, and only the safe work
	files := git(t, repo, "ls-tree", "-r", "--name-only", "griffin/handover-test")
	for _, want := range []string{"app.go", "new_feature.go", "staged.go", "HANDOVER.md"} {
		if !strings.Contains("\n"+files+"\n", "\n"+want+"\n") {
			t.Errorf("handover branch is missing %s; has:\n%s", want, files)
		}
	}
	for _, never := range []string{".env", "id_rsa", "debug.log", "huge.bin"} {
		if strings.Contains("\n"+files+"\n", "\n"+never+"\n") {
			t.Errorf("%s must never be saved to the branch", never)
		}
	}
	if got := git(t, repo, "show", "griffin/handover-test:app.go"); !strings.Contains(got, "edited, unstaged") {
		t.Errorf("unstaged edits must be saved, got %q", got)
	}
	// a tracked secret-like file keeps the version already in git, not the modified one
	if got := git(t, repo, "show", "griffin/handover-test:settings.key"); strings.Contains(got, "MODIFIED") || !strings.Contains(got, "HEAD-VERSION") {
		t.Errorf("a modified secret-like file must keep its committed version, got %q", got)
	}
	// 3. the note names things but contains no contents
	doc := git(t, repo, "show", "griffin/handover-test:HANDOVER.md")
	for _, leak := range []string{"super-secret-token", "PRIVATE KEY", "MODIFIED-SECRET"} {
		if strings.Contains(doc, leak) {
			t.Errorf("the note must never contain file contents (%q)", leak)
		}
	}
	for _, want := range []string{"Research X", "new_feature.go", "main", "Do not push to the owner's main branch"} {
		if !strings.Contains(doc, want) {
			t.Errorf("note should mention %q:\n%s", want, doc)
		}
	}
	// 4. it starts from HEAD, so it reads as a continuation of the owner's work
	if parent := git(t, repo, "rev-parse", "griffin/handover-test~1"); parent != beforeHead {
		t.Errorf("the first snapshot must start from HEAD (%s), parent is %s", beforeHead, parent)
	}
	if res.Files != 3 || len(res.Skipped) < 3 {
		t.Errorf("want 3 files saved and the secret/large ones reported as skipped, got %d saved, skipped %v", res.Files, res.Skipped)
	}
}

func TestSnapshotOfACleanRepoMakesNoBranch(t *testing.T) {
	repo, _ := newRepo(t, false)
	res, err := Snapshot(context.Background(), repo, opts(false))
	if err != nil || !res.Clean {
		t.Fatalf("want clean, got %+v, %v", res, err)
	}
	if out := git(t, repo, "branch", "--list", "griffin/*"); out != "" {
		t.Errorf("no branch for nothing to save, got %q", out)
	}
	// only a secret changed: still nothing to save
	write(t, repo, ".env", "X=1\n")
	res, _ = Snapshot(context.Background(), repo, opts(false))
	if !res.Clean || len(res.Skipped) != 1 {
		t.Errorf("a repo where only a secret changed has nothing safe to save: %+v", res)
	}
}

func TestSnapshotPushesAFastForwardBranchAndNothingElse(t *testing.T) {
	repo, remote := newRepo(t, true)
	write(t, repo, "work1.go", "one\n")
	if res, err := Snapshot(context.Background(), repo, opts(true)); err != nil || !res.Pushed {
		t.Fatalf("first push: %+v, %v", res, err)
	}
	first := git(t, remote, "rev-parse", "griffin/handover-test")

	write(t, repo, "work2.go", "two\n")
	res, err := Snapshot(context.Background(), repo, opts(true))
	if err != nil || !res.Pushed || res.PushErr != "" {
		t.Fatalf("second snapshot must fast-forward the same branch: %+v, %v", res, err)
	}
	second := git(t, remote, "rev-parse", "griffin/handover-test")
	if first == second {
		t.Fatal("the remote branch should have moved")
	}
	git(t, remote, "merge-base", "--is-ancestor", first, second) // fails the test unless the push fast-forwarded
	if out := git(t, remote, "branch", "--list"); strings.Count(out, "\n") != 1 || !strings.Contains(out, "main") {
		t.Errorf("only main and the handover branch may exist remotely, got:\n%s", out)
	}
	if git(t, remote, "rev-parse", "main") != git(t, repo, "rev-parse", "main") {
		t.Error("the remote's main must not be touched")
	}
	files := git(t, remote, "ls-tree", "-r", "--name-only", "griffin/handover-test")
	if !strings.Contains(files, "work1.go") || !strings.Contains(files, "work2.go") {
		t.Errorf("the pushed branch should hold all the work:\n%s", files)
	}
}

func TestSnapshotReportsPushFailureButKeepsTheLocalBranch(t *testing.T) {
	repo, remote := newRepo(t, true)
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "work.go", "x\n")
	res, err := Snapshot(context.Background(), repo, SnapshotOptions{Branch: "griffin/handover-test", Push: true, PushTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("a failed push is reported, not fatal: %v", err)
	}
	if res.Pushed || res.PushErr == "" {
		t.Errorf("failure must be visible: %+v", res)
	}
	if git(t, repo, "branch", "--list", "griffin/handover-test") == "" {
		t.Error("the local branch must still exist")
	}
}

func TestSnapshotWithoutARemoteSaysSo(t *testing.T) {
	repo, _ := newRepo(t, false)
	write(t, repo, "w.go", "x\n")
	res, err := Snapshot(context.Background(), repo, opts(true))
	if err != nil || res.Pushed || len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "no git remote") {
		t.Fatalf("want a clear note, got %+v, %v", res, err)
	}
}

func TestSnapshotRefusesBadInput(t *testing.T) {
	if _, err := Snapshot(context.Background(), t.TempDir(), opts(false)); err == nil {
		t.Error("a folder that is not a repository must be an error")
	}
	empty := t.TempDir()
	git(t, empty, "init", "-q")
	if _, err := Snapshot(context.Background(), empty, opts(false)); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("a repo with no commits must say so, got %v", err)
	}
	repo, _ := newRepo(t, false)
	for _, bad := range []string{"", "-rf", "a b", "x:y", "a~1"} {
		if _, err := Snapshot(context.Background(), repo, SnapshotOptions{Branch: bad}); err == nil {
			t.Errorf("branch name %q must be refused", bad)
		}
	}
}

func TestSecretLikeNames(t *testing.T) {
	secret := []string{".env", "web/.env.local", ".env.production", "a/b/server.pem", "k.key", "id_rsa", "id_ed25519.pub", "terraform.tfstate",
		"prod.tfvars", "credentials.json", "gcp-service-account.json", ".npmrc", "my-secrets.yaml", "x/.ssh/config", ".aws/credentials", "vault.kdbx"}
	for _, n := range secret {
		if !secretLike(n) {
			t.Errorf("%s should be treated as a secret", n)
		}
	}
	fine := []string{".env.example", ".env.sample", "main.go", "README.md", "web/src/app.tsx", "environment.go", "keyboard.go", "package.json"}
	for _, n := range fine {
		if secretLike(n) {
			t.Errorf("%s should be fine", n)
		}
	}
}

func TestThresholdsAndLevels(t *testing.T) {
	d := DefaultThresholds
	cases := []struct {
		pct   int
		disch bool
		want  Level
	}{{100, true, LevelOK}, {16, true, LevelOK}, {15, true, LevelSave}, {6, true, LevelSave}, {5, true, LevelPush}, {3, true, LevelPush}, {2, true, LevelFinal}, {0, true, LevelFinal}, {2, false, LevelOK}, {15, false, LevelOK}}
	for _, c := range cases {
		if got := d.LevelFor(c.pct, c.disch); got != c.want {
			t.Errorf("LevelFor(%d, discharging=%v) = %v, want %v", c.pct, c.disch, got, c.want)
		}
	}
	if th, ok := ParseThresholds("20, 8, 2"); !ok || th != (Thresholds{20, 8, 2}) {
		t.Errorf("custom thresholds: %+v %v", th, ok)
	}
	if th, ok := ParseThresholds(""); !ok || th != DefaultThresholds {
		t.Error("empty means the defaults")
	}
	for _, bad := range []string{"2,5,15", "15,5", "a,b,c", "15,5,0", "100,5,2", "5,5,2"} {
		if _, ok := ParseThresholds(bad); ok {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSysfsBattery(t *testing.T) {
	root := t.TempDir()
	if _, ok := (SysfsBattery{Root: root}).Read(); ok {
		t.Error("a machine without a battery reports none")
	}
	bat := filepath.Join(root, "BAT0")
	os.MkdirAll(bat, 0o755)
	os.WriteFile(filepath.Join(bat, "capacity"), []byte("7\n"), 0o644)
	os.WriteFile(filepath.Join(bat, "status"), []byte("Discharging\n"), 0o644)
	if r, ok := (SysfsBattery{Root: root}).Read(); !ok || r.Percent != 7 || !r.Discharging {
		t.Errorf("got %+v %v", r, ok)
	}
	os.WriteFile(filepath.Join(bat, "status"), []byte("Charging\n"), 0o644)
	if r, _ := (SysfsBattery{Root: root}).Read(); r.Discharging {
		t.Error("Charging is not discharging")
	}
	os.WriteFile(filepath.Join(bat, "capacity"), []byte("garbage"), 0o644)
	if _, ok := (SysfsBattery{Root: root}).Read(); ok {
		t.Error("an unreadable capacity is no reading, not zero percent")
	}
}

type fakeBattery struct {
	mu sync.Mutex
	r  Reading
	ok bool
}

func (f *fakeBattery) set(p int, d bool) { f.mu.Lock(); f.r, f.ok = Reading{p, d}, true; f.mu.Unlock() }
func (f *fakeBattery) Read() (Reading, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.r, f.ok
}

func TestMonitorFiresEachLevelOnceEscalatesAndResetsOnCharge(t *testing.T) {
	bat := &fakeBattery{}
	bat.set(50, true)
	var mu sync.Mutex
	var levels []Level
	recovered := 0
	m := &Monitor{Battery: bat, Thresholds: DefaultThresholds, Interval: 5 * time.Millisecond,
		OnLevel:   func(_ context.Context, l Level, _ Reading) { mu.Lock(); levels = append(levels, l); mu.Unlock() },
		OnRecover: func(context.Context, Reading) { mu.Lock(); recovered++; mu.Unlock() }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	wait := func(cond func() bool) {
		t.Helper()
		for i := 0; i < 400; i++ {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out; levels=%v recovered=%d", levels, recovered)
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if len(levels) != 0 {
		t.Fatalf("nothing should fire at 50%%: %v", levels)
	}
	mu.Unlock()

	bat.set(14, true)
	wait(func() bool { return len(levels) == 1 })
	time.Sleep(30 * time.Millisecond) // many more readings at the same level
	bat.set(4, true)
	wait(func() bool { return len(levels) == 2 })
	bat.set(10, true) // a battery reading creeping back up must not re-fire or go backwards
	time.Sleep(30 * time.Millisecond)
	bat.set(2, true)
	wait(func() bool { return len(levels) == 3 })
	mu.Lock()
	if levels[0] != LevelSave || levels[1] != LevelPush || levels[2] != LevelFinal {
		t.Fatalf("levels must escalate once each: %v", levels)
	}
	mu.Unlock()

	bat.set(3, false) // plugged in
	wait(func() bool { return recovered == 1 })
	bat.set(14, true) // drains again: a new episode fires again
	wait(func() bool { return len(levels) == 4 && levels[3] == LevelSave })
}

type fakeAgent struct {
	mu      sync.Mutex
	drains  []string
	resumed int
	tasks   []hostagent.RunningTask
}

func (f *fakeAgent) Drain(r string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drains = append(f.drains, r)
	return len(f.tasks)
}
func (f *fakeAgent) Resume() { f.mu.Lock(); f.resumed++; f.mu.Unlock() }
func (f *fakeAgent) RunningTasks() []hostagent.RunningTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks
}

func TestResponderSavesThenPushesAndHandsTasksBackAtTheRightStages(t *testing.T) {
	repo, remote := newRepo(t, true)
	write(t, repo, "wip.go", "wip\n")
	agent := &fakeAgent{tasks: []hostagent.RunningTask{{ID: "t1", Title: "Draft the cover letter"}}}
	fixed := time.Date(2026, 11, 9, 23, 41, 0, 0, time.UTC)
	r := &Responder{Host: "laptop", Repos: func() []string { return []string{repo, repo} }, Agent: agent, Now: func() time.Time { return fixed }}

	// stage 1: local save only, tasks keep running
	res := r.Handle(context.Background(), LevelSave, Reading{Percent: 15, Discharging: true})
	if len(res) != 2 || res[0].Pushed {
		t.Fatalf("save stage must not push: %+v", res)
	}
	if len(agent.drains) != 0 {
		t.Fatal("tasks keep running at the save stage")
	}
	if out := git(t, remote, "branch", "--list", "griffin/*"); out != "" {
		t.Fatalf("nothing is pushed at the save stage, remote has %q", out)
	}
	if git(t, repo, "branch", "--list", "griffin/handover-20261109-2341") == "" {
		t.Fatal("expected the session branch named from the time")
	}

	// stage 2: tasks handed back, work pushed
	write(t, repo, "wip2.go", "more\n")
	r.Handle(context.Background(), LevelPush, Reading{Percent: 5, Discharging: true})
	if len(agent.drains) != 1 || !strings.Contains(agent.drains[0], "5%") {
		t.Fatalf("running tasks must be handed back once with the reason, got %v", agent.drains)
	}
	files := git(t, remote, "ls-tree", "-r", "--name-only", "griffin/handover-20261109-2341")
	if !strings.Contains(files, "wip.go") || !strings.Contains(files, "wip2.go") {
		t.Fatalf("the pushed branch should hold everything:\n%s", files)
	}
	doc := git(t, remote, "show", "griffin/handover-20261109-2341:HANDOVER.md")
	if !strings.Contains(doc, "Draft the cover letter") || !strings.Contains(doc, "5%") {
		t.Fatalf("the note must list the handed-back task and the battery level:\n%s", doc)
	}

	// final stage: still safe to call again; one more drain request, branch continues
	write(t, repo, "wip3.go", "last\n")
	r.Handle(context.Background(), LevelFinal, Reading{Percent: 2, Discharging: true})
	if !strings.Contains(git(t, remote, "ls-tree", "-r", "--name-only", "griffin/handover-20261109-2341"), "wip3.go") {
		t.Fatal("the final stage must push what changed since")
	}

	// charging again
	r.Recover(context.Background(), Reading{Percent: 4})
	if agent.resumed != 1 {
		t.Fatal("the agent must resume when charging")
	}
}

func TestOpenFoldersReadsVSCodeWindowState(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "Code", "User", "globalStorage")
	os.MkdirAll(dir, 0o755)
	state := map[string]any{
		"windowsState": map[string]any{
			"lastActiveWindow": map[string]any{"folder": "file:///home/me/project%20one"},
			"openedWindows":    []map[string]any{{"folder": "file:///home/me/griffin"}, {"folder": "vscode-remote://ssh-remote+box/srv/app"}, {"workspaceIdentifier": "x"}},
		},
		"backupWorkspaces": map[string]any{"folders": []map[string]any{{"folderUri": "file:///home/me/griffin"}, {"folderUri": "file:///home/me/other"}}},
	}
	raw, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "storage.json"), raw, 0o644)

	got := OpenFolders(home)
	want := []string{"/home/me/griffin", "/home/me/other", "/home/me/project one"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v (deduplicated, local folders only, URL-decoded)", got, want)
	}
	if got := OpenFolders(t.TempDir()); len(got) != 0 {
		t.Errorf("no VS Code means no folders, got %v", got)
	}
	os.WriteFile(filepath.Join(dir, "storage.json"), []byte("{not json"), 0o644)
	if got := OpenFolders(home); len(got) != 0 {
		t.Errorf("a broken file contributes nothing, got %v", got)
	}
}
