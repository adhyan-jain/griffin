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
	"github.com/griffin/core/internal/service"
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
	fmt.Fprintln(tw, "#\texpected\tchosen\tpass\tgrounding(first/final)\trequest\tsequence")
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
		seq := strings.Join(dry.calls, ">")
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", n, exp, chosen, pass, ground, shorten(req, 50), seq)
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
		Listings:  newFakeListings(user),
		Trackers:  newFakeTrackers(),
		Vault:     fakeVault{},
		CheckLink: func(context.Context, string) service.PostingState { return service.PostingOpen },
	}
	return d
}

// registry builds the real tools over the fakes and wraps each to record that it was called.
func (d *dry) registry() *agent.Registry {
	tools := agent.NewTools(d.deps)
	for _, t := range tools {
		name, run, prepare := t.Name, t.Run, t.Prepare
		if run != nil {
			t.Run = func(ctx context.Context, c *agent.Call, args json.RawMessage) (agent.Output, error) {
				d.calls = append(d.calls, name)
				if os.Getenv("EVAL_ARGS") != "" {
					fmt.Fprintf(os.Stderr, "  call %s %s\n", name, args)
				}
				return run(ctx, c, args)
			}
		}
		if prepare != nil { // an external tool: record it, and never execute it (the dry run changes nothing)
			t.Prepare = func(args json.RawMessage) (json.RawMessage, string, error) {
				d.calls = append(d.calls, name)
				return prepare(args)
			}
			t.Exec = func(context.Context, uuid.UUID, json.RawMessage) (string, error) {
				return "Done (dry run: nothing was sent).", nil
			}
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
	all  []*domain.Schedule
}

func (f *fakeRoutines) list() []*domain.Schedule {
	if f.all == nil {
		mk := func(name, goal string) *domain.Schedule {
			return &domain.Schedule{ID: uuid.New(), UserID: f.user, Name: name, CronExpression: "daily 09:00 Asia/Kolkata", NextRunAt: f.now.Add(6 * time.Hour),
				TaskSpecTemplate: []byte(`{"title":"` + name + `","goal":"` + goal + `"}`)}
		}
		f.all = []*domain.Schedule{
			mk("Internship search", "Search interndock.com/companies for software and AI internships and add verified roles to the Internships tracker"),
			mk("Repo README update", "Update the READMEs of my GitHub repos so each explains what it does and how to run it"),
			mk("HACKATHON TRACKER", "Find open hackathons on Devpost, Unstop and Devfolio that fit me and keep the Hackathons tracker current"),
			mk("Community scan", "Scan the communities I follow and draft replies I can send on relevant posts"),
		}
	}
	return f.all
}
func (f *fakeRoutines) ListSchedules(context.Context, uuid.UUID) ([]*domain.Schedule, error) {
	return f.list(), nil
}
func (f *fakeRoutines) GetScheduleByID(_ context.Context, id uuid.UUID) (*domain.Schedule, error) {
	for _, s := range f.list() {
		if s.ID == id {
			cp := *s
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
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

type fakeListings struct {
	user uuid.UUID
	all  []*domain.Listing
}

func newFakeListings(user uuid.UUID) *fakeListings {
	mk := func(company, role, pay, stage string) *domain.Listing {
		return &domain.Listing{ID: uuid.New(), UserID: user, Company: company, Role: role, Pay: pay, Tier: domain.TierForPay(pay), Location: "Remote", Stage: stage,
			URL: "https://www.interndock.com/internships/" + strings.ToLower(strings.ReplaceAll(company, " ", "-"))}
	}
	return &fakeListings{user: user, all: []*domain.Listing{
		mk("Acme", "Backend Intern", "₹ 50,000 /month", domain.StageVerified),
		mk("Globex", "ML Intern", "₹ 45,000 /month", domain.StageVerified),
		mk("Initech", "SDE Intern", "₹ 20,000 /month", domain.StageVerified),
		mk("Umbrella", "Full Stack Intern", "Unpaid", domain.StageVerified),
	}}
}

func (f *fakeListings) ListAssessed(_ context.Context, _ uuid.UUID, fl domain.AssessedFilter) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, l := range f.all {
		if len(fl.Tiers) > 0 && l.Tier != fl.Tiers[0] {
			continue
		}
		if len(fl.Stages) > 0 && l.Stage != fl.Stages[0] {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}
func (f *fakeListings) Get(_ context.Context, _, id uuid.UUID) (*domain.Listing, error) {
	for _, l := range f.all {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeListings) Move(_ context.Context, _, id uuid.UUID, _, to, _ string, _ *uuid.UUID) error {
	for _, l := range f.all {
		if l.ID == id {
			l.Stage = to
		}
	}
	return nil
}

type fakeTrackers struct{ lists map[string]string }

func newFakeTrackers() *fakeTrackers {
	return &fakeTrackers{lists: map[string]string{
		"hackathons":  "| Name | Mode | Deadline | Link | Status |\n|---|---|---|---|---|\n| AI Agents Hack | Online | 2026-12-10 | https://devpost.com/a | open |\n| BuildFest | Mumbai | 2026-12-05 | https://unstop.com/b | open |\n| Devfolio Zero | Online | 2026-12-20 | https://devfolio.co/c | open |",
		"internships": "| Tier | Company | Role | Link | Status |\n|---|---|---|---|---|\n| Tier A | Acme | Backend Intern | https://www.interndock.com/internships/acme | open |",
		"applied":     "| Company | Role | Status |\n|---|---|---|\n| Initech | SDE Intern | applied |",
	}}
}
func (f *fakeTrackers) ReadList(_ context.Context, _ uuid.UUID, t string) (string, bool, error) {
	s, ok := f.lists[t]
	return s, ok, nil
}
func (f *fakeTrackers) WriteList(_ context.Context, _ uuid.UUID, t, text string) error {
	f.lists[t] = text
	return nil
}

type fakeVault struct{}

func (fakeVault) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	meta := func(label string, c domain.VaultCategory) *domain.Artifact {
		b, _ := json.Marshal(domain.VaultMeta{Kind: domain.VaultKind, Label: label, Category: c})
		return &domain.Artifact{ID: uuid.New(), Metadata: b}
	}
	return []*domain.Artifact{meta("PAN card", domain.VaultID), meta("Backend resume", domain.VaultResume), meta("Semester 6 marksheet", domain.VaultCertificate)}, nil
}
