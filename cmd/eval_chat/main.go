// Command eval_chat runs the JJ chat agent with the real Gemini model against DRY-RUN tools: the tools have the
// real names and schemas but run on fake data and only record that they were called. It prints, per request, the
// tool the model chose first, whether that was acceptable, and how many unsupported statements the reply held.
//
//	set -a; . ./.env; set +a
//	go run ./cmd/eval_chat [requests.txt [expectations.tsv]]
//
// The requests file has one request per line. The expectations file has lines "N<TAB>tool1,tool2": request number N
// passes when its first tool call is one of them. The API key is read from GEMINI_API_KEY or GEMINI_KEY and is
// never printed.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/web"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
)

func main() {
	key := firstEnv("GEMINI_API_KEY", "GEMINI_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "set GEMINI_API_KEY (or GEMINI_KEY), for example: set -a; . ./.env; set +a")
		os.Exit(2)
	}
	home, _ := os.UserHomeDir()
	reqPath := filepath.Join(home, "Documents", "Griffin", "eval", "past-requests.txt")
	if len(os.Args) > 1 {
		reqPath = os.Args[1]
	}
	requests := readLines(reqPath)
	expected := map[int][]string{}
	if len(os.Args) > 2 {
		for _, l := range readLines(os.Args[2]) {
			n, rest, ok := strings.Cut(l, "\t")
			if i, err := strconv.Atoi(strings.TrimSpace(n)); ok && err == nil {
				expected[i] = strings.Split(strings.TrimSpace(rest), ",")
			}
		}
	}
	model := firstEnv("GEMINI_AGENT_MODEL")
	if model == "" {
		model = "gemini-flash-latest"
	}
	client := &gemini.Client{APIKey: key, Model: model, MaxOutputTokens: 1500, Fallback: gemini.DefaultFallbackModel}

	user := uuid.New()
	now := time.Now()
	passed, withExpect := 0, 0
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\texpected\tchosen\tpass\tgrounding(first/final)\trequest")
	for i, req := range requests {
		n := i + 1
		dry := newDry(user, now)
		ag := &agent.Agent{Model: agent.GeminiModel{Client: client}, Store: agent.NewMemStore(), Registry: dry.registry(), Approvals: nil,
			Now: func() time.Time { return now }, Deadline: 40 * time.Second}
		res, err := ag.Run(context.Background(), agent.Input{UserID: user, ConversationID: uuid.New(), Message: req, Origin: agent.OriginOwnerChat})
		chosen, ground := "-", "-"
		switch {
		case err != nil:
			chosen = "ERROR: " + shorten(strings.ReplaceAll(err.Error(), key, "[key]"), 40)
		default:
			chosen = "(no tool)"
			if len(dry.calls) > 0 {
				chosen = dry.calls[0]
			}
			if res.Handoff != nil {
				chosen = "start_job"
			}
			ground = fmt.Sprintf("%d/%d", res.GroundingFirst, res.GroundingFinal)
		}
		exp, pass := "-", "-"
		if want, ok := expected[n]; ok {
			exp = strings.Join(want, "|")
			withExpect++
			pass = "FAIL"
			for _, w := range want {
				if w == chosen {
					pass = "ok"
					passed++
				}
			}
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", n, exp, chosen, pass, ground, shorten(req, 50))
		time.Sleep(700 * time.Millisecond)
	}
	tw.Flush()
	if withExpect > 0 {
		fmt.Printf("\n%d/%d first tools acceptable\n", passed, withExpect)
	}
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), "\r\n"); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// dry is one request's set of fake services; every tool call is recorded in calls.
type dry struct {
	user  uuid.UUID
	now   time.Time
	calls []string
	deps  agent.Deps
}

func newDry(user uuid.UUID, now time.Time) *dry {
	d := &dry{user: user, now: now}
	d.deps = agent.Deps{
		Tasks:     &fakeTasks{user: user},
		Routines:  &fakeRoutines{user: user, now: now},
		Reminders: fakeReminders{},
		Approvals: fakeApprovals{user: user},
		Search:    fakeSearch{},
		Fetch:     fakeFetch{},
		Store:     agent.NewMemStore(),
	}
	return d
}

// registry builds the real tools over the fakes and wraps each to record that it was called.
func (d *dry) registry() *agent.Registry {
	tools := agent.NewTools(d.deps)
	for _, t := range tools {
		name, run := t.Name, t.Run
		t.Run = func(ctx context.Context, c *agent.Call, args json.RawMessage) (agent.Output, error) {
			d.calls = append(d.calls, name)
			return run(ctx, c, args)
		}
	}
	reg, err := agent.NewRegistry(tools...)
	if err != nil {
		panic(err)
	}
	return reg
}

type fakeTasks struct{ user uuid.UUID }

func (f *fakeTasks) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return []*domain.Task{
		{ID: uuid.New(), UserID: f.user, Title: "RESEARCH: interndock sweep", Status: domain.TaskStatusRunning},
		{ID: uuid.New(), UserID: f.user, Title: "APPLY: Acme backend intern", Status: domain.TaskStatusQueued},
		{ID: uuid.New(), UserID: f.user, Title: "Chat: hello", Status: domain.TaskStatusCompleted},
	}, nil
}
func (f *fakeTasks) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (f *fakeTasks) ResumeTask(context.Context, uuid.UUID) error         { return nil }
func (f *fakeTasks) CreateTask(_ context.Context, t *domain.Task, _ []uuid.UUID) (*domain.Task, error) {
	t.ID = uuid.New()
	return t, nil
}

type fakeRoutines struct {
	user uuid.UUID
	now  time.Time
}

func (f *fakeRoutines) list() []*domain.Schedule {
	mk := func(name, goal string) *domain.Schedule {
		return &domain.Schedule{ID: uuid.New(), UserID: f.user, Name: name, CronExpression: "daily 09:00 Asia/Kolkata", NextRunAt: f.now.Add(6 * time.Hour),
			TaskSpecTemplate: []byte(`{"title":"` + name + `","goal":"` + goal + `"}`)}
	}
	return []*domain.Schedule{mk("Internship search", "Search interndock.com for internships"), mk("Repo README update", "Update READMEs of my repos")}
}
func (f *fakeRoutines) ListSchedules(context.Context, uuid.UUID) ([]*domain.Schedule, error) {
	return f.list(), nil
}
func (f *fakeRoutines) GetScheduleByID(_ context.Context, id uuid.UUID) (*domain.Schedule, error) {
	s := f.list()[0]
	s.ID = id
	return s, nil
}
func (f *fakeRoutines) UpdateSchedule(context.Context, *domain.Schedule) error { return nil }

type fakeReminders struct{}

func (fakeReminders) Create(_ context.Context, u uuid.UUID, _ string, _ time.Time) (*domain.Task, error) {
	return &domain.Task{ID: uuid.New(), UserID: u}, nil
}

type fakeApprovals struct{ user uuid.UUID }

func (f fakeApprovals) ListApprovals(context.Context, uuid.UUID, *domain.ApprovalStatus) ([]*domain.Approval, error) {
	return []*domain.Approval{{ID: uuid.New(), UserID: f.user, ActionType: "application_package", Summary: "Application package: Acme - Backend Intern"}}, nil
}
func (f fakeApprovals) RespondApproval(_ context.Context, id, _ uuid.UUID, _ bool, _ string) (*domain.Approval, error) {
	return &domain.Approval{ID: id}, nil
}

type fakeSearch struct{}

func (fakeSearch) Search(context.Context, string, int) ([]web.Result, string, error) {
	return []web.Result{{Title: "Example result", URL: "https://example.com/a", Snippet: "A sample snippet."}}, "dry-run", nil
}

type fakeFetch struct{}

func (fakeFetch) Fetch(_ context.Context, u string) (*web.Page, error) {
	return &web.Page{URL: u, Title: "Example page", Text: "Sample page text."}, nil
}
